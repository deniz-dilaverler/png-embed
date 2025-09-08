// Package pngembed embeds key-value data into a png image.
package pngembed

////////////////////////////////////////////////////////////////////////////////

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"strings"

	"github.com/sabhiram/pngr"
)

////////////////////////////////////////////////////////////////////////////////

var (
	pngMagic = []byte{137, 80, 78, 71, 13, 10, 26, 10}
)

const NULL_SEPERATOR byte = 0

////////////////////////////////////////////////////////////////////////////////

// Returns nil if sub is contained in s, an error otherwise.
func errIfNotSubStr(s, sub []byte) error {
	if len(sub) > len(s) {
		return errors.New("substring larger than parent")
	}
	for i, d := range sub {
		if d != s[i] {
			return errors.New("byte mismatch with sub")
		}
	}
	return nil
}

func isValidChunkType(ct string) bool {
	for _, v := range []string{
		// Critical chunks.
		"IHDR", "PLTE", "IDAT", "IEND",

		// Ancillary chunks.
		"bKGD", "cHRM", "dSIG", "eXIf", "gAMA", "hIST", "iCCP", "iTXt", "pHYs",
		"sBIT", "sPLT", "sRGB", "sTER", "tEXt", "tIME", "tRNS", "zTXt",
	} {
		if v == ct {
			return true
		}
	}
	return false
}

// buildChunk encodes the specified chunk type and data into a png chunk.  If
// the chunk type is invalid, it is rejected.
func buildChunk(ct string, data []byte) ([]byte, error) {
	// -------------------------------------------------------------------
	// |  Length    |  Chunk Type |       ... Data ...       |    CRC    |
	// -------------------------------------------------------------------
	// |  4 bytes   |   4 bytes   |     `Length` bytes       |  4 bytes  |
	//              |-------------- CRC32'd -----------------|
	if !isValidChunkType(ct) {
		return nil, fmt.Errorf("invalid chunk type (%s)", ct)
	}

	szbs := make([]byte, 4)
	binary.BigEndian.PutUint32(szbs, uint32(len(data)))

	bb := append([]byte(ct), data...)

	crcbs := make([]byte, 4)
	binary.BigEndian.PutUint32(crcbs, crc32.ChecksumIEEE(bb))

	bb = append(bb, crcbs...)

	// Prepend the length to the payload.
	return append(szbs, bb...), nil
}

// embed verifies that the input data slice actually describes a PNG image, and
// embeds the given png chunk into the png file
func embed(data []byte, chunk []byte) ([]byte, error) {
	out := []byte{}
	buf := bytes.NewBuffer(data)

	// Magic number.
	d := buf.Next(len(pngMagic))
	out = append(out, d...)
	err := errIfNotSubStr(pngMagic, d)
	if err != nil {
		return nil, err
	}

	// Extract header length, the header type should always be the first, we
	// inject our custom text data right after this.
	d = buf.Next(4)
	out = append(out, d...)
	sz := binary.BigEndian.Uint32(d)

	// Extract the header tag, data, and CRC (for the header).
	d = buf.Next(int(sz + 8))
	out = append(out, d...)

	// Append tEXt chunk.
	out = append(out, chunk...)

	// Add the rest of the actual palette and data info.
	return append(out, buf.Bytes()...), nil
}

////////////////////////////////////////////////////////////////////////////////

// Embed processes a stream of raw PNG data, and encodes the specified key-value
// pair into a `tEXt` chunk.  The resultant PNG byte-stream is returned, or an
// error.  The interface `v` is serialized to known types and then to JSON if
// all else fails.
func EmbedTEXT(data []byte, k string, v interface{}) ([]byte, error) {
	var (
		err error
		val []byte
	)

	val, err = to_bytes(v)

	if err != nil {
		return nil, err
	}
	tEXtChunk := formatTEXTChunk(val, k)
	pngChunk, _ := buildChunk(`tEXt`, tEXtChunk)

	return embed(data, pngChunk)
}

func to_bytes(v interface{}) ([]byte, error) {
	var (
		err error
		val []byte
	)
	switch vt := v.(type) {
	case int, uint:
		val = []byte(fmt.Sprintf("%d", vt))
	case float32, float64:
		val = []byte(fmt.Sprintf("%f", vt))
	case string:
		val = []byte(vt)
	default:
		val, err = json.Marshal(v)
	}
	return val, err
}

// EmbedFile is like `Embed` but accepts the path to a PNG file.
// Embeds to a file's tEXt chunk
func EmbedTEXTInFile(fp, k string, v interface{}) ([]byte, error) {
	data, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}

	return EmbedTEXT(data, k, v)
}

////////////////////////////////////////////////////////////////////////////////

// Extract processes a stream of raw PNG data, and returns a map of `tEXt`
// records encoded by this library.
func ExtractTEXT(data []byte) (map[string][]byte, error) {
	ret := map[string][]byte{}

	r, err := pngr.NewReader(data, &pngr.ReaderOptions{
		IncludedChunkTypes: []string{`tEXt`},
	})
	if err != nil {
		return nil, err
	}

	c, err := r.Next()
	for ; err == nil; c, err = r.Next() {
		sz := len(c.Data)
		pt := strings.Index(string(c.Data), string(0))
		if pt < sz {
			ret[string(c.Data[:pt])] = c.Data[pt+1:]
		}
	}
	if err == io.EOF {
		err = nil
	}

	return ret, err
}

func readNullTerminated(r *bufio.Reader) (string, error) {
	data, err := r.ReadBytes(NULL_SEPERATOR)
	if err != nil {
		return "", err
	}
	return string(data[:len(data)-1]), nil // strip the null terminator
}

// Returns all itxt text fields and their keyword in a (keyword, text) map
func ExtractITXT(data []byte) (map[string][]byte, error) {
	ret := map[string][]byte{}

	r, err := pngr.NewReader(data, &pngr.ReaderOptions{
		IncludedChunkTypes: []string{`iTXt`},
	})
	if err != nil {
		return nil, err
	}

	c, err := r.Next()
	for ; err == nil; c, err = r.Next() {
		br := bufio.NewReader(bytes.NewReader(c.Data))
		keyword, err := readNullTerminated(br)
		if err != nil {
			return nil, err
		}

		// 2. Compression flag (1 byte)
		if _, err := br.Discard(1); err != nil {
			return nil, fmt.Errorf("discard compression flag: %w", err)
		}

		// 3. Compression method (1 byte)
		if _, err := br.Discard(1); err != nil {
			return nil, fmt.Errorf("discard compression method: %w", err)
		}

		// 4. Consume Language tag including null-sep
		_, err = br.ReadBytes(NULL_SEPERATOR)
		if err != nil {
			return nil, fmt.Errorf("read language tag: %w", err)
		}

		// 5. consume Translated keyword including Null-sep
		_, err = br.ReadBytes(NULL_SEPERATOR)
		if err != nil {
			return nil, fmt.Errorf("read translated keyword: %w", err)
		}

		// 6. Remaining bytes = Text
		textBytes, err := io.ReadAll(br)
		if err != nil {
			return nil, fmt.Errorf("read text: %w", err)
		}
		ret[keyword] = textBytes

	}
	if err == io.EOF {
		err = nil
	}

	return ret, err
}

// ExtractFile is like `Extract` but accepts the path to a PNG file.
// Extrats the tEXt from the png
func ExtractFileTEXT(fp string) (map[string][]byte, error) {
	data, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}

	return ExtractTEXT(data)
}

func EmbedITXT(data []byte, k string, v interface{}) ([]byte, error) {
	var (
		err error
		val []byte
	)

	val, err = to_bytes(v)

	if err != nil {
		return nil, err
	}
	compression_flag := 0
	compression_method := 0
	language_tag := ""
	translate_keyword := ""

	iTXtChunk := formatITXTChunk(val, k, compression_flag, compression_method, language_tag, translate_keyword)
	pngChunk, _ := buildChunk(`iTXt`, iTXtChunk)

	return embed(data, pngChunk)

}

func formatTEXTChunk(text []byte, keyword string) []byte {

	// +----------+----------------+---------+
	// | Keyword  | Null separator |  Text   |
	// +----------+----------------+---------+
	// | 1–79     | 1 byte         | n bytes |
	// | bytes    |                |         |
	// +----------+----------------+---------+

	tEXtChunk := append([]byte(keyword), NULL_SEPERATOR)
	tEXtChunk = append(tEXtChunk, text...)
	return tEXtChunk

}
func formatITXTChunk(text []byte, keyword string, compression_flag int, compression_method int, language_tag string, translated_keyword string) []byte {

	// +------------------+----------------+-----------------+-------------------+---------------+----------------+---------------------+----------------+----------------+
	// | Keyword          | Null separator | Compression flag| Compression method| Language tag  | Null separator | Translated keyword  | Null separator | Text           |
	// +------------------+----------------+-----------------+-------------------+---------------+----------------+---------------------+----------------+----------------+
	// | 1-79 bytes       | 1 byte         | 1 byte          | 1 byte            |0 or more bytes| 1 byte         | 0 or more bytes     | 1 byte         | 0 or more bytes|
	// +------------------+----------------+-----------------+-------------------+---------------+----------------+---------------------+----------------+----------------+

	// Add keyword
	iTXtChunk := append([]byte(keyword), NULL_SEPERATOR)

	//Add compression information
	iTXtChunk = append(iTXtChunk, byte(compression_flag))
	iTXtChunk = append(iTXtChunk, byte(compression_method))

	iTXtChunk = append(iTXtChunk, []byte(language_tag)...)
	iTXtChunk = append(iTXtChunk, NULL_SEPERATOR)

	iTXtChunk = append(iTXtChunk, []byte(translated_keyword)...)
	iTXtChunk = append(iTXtChunk, NULL_SEPERATOR)
	iTXtChunk = append(iTXtChunk, text...)
	return iTXtChunk

}

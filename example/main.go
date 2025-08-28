package main

////////////////////////////////////////////////////////////////////////////////

import (
	"flag"
	"fmt"
	"os"

	pngembed "github.com/deniz-dilaverler/png-embed"
)

type InnerStruct struct {
	InnerBool bool   `json:"inner_bool"`
	InnerStr  string `json:"inner_str"`
	InnerInt  int    `json:"inner_int"`
}

type SampleStruct struct {
	StrVal    string      `json:"str_val"`
	IntVal    int         `json:"int_val"`
	BoolVal   bool        `json:"bool_val"`
	StructVal InnerStruct `json:"struct_val"`
}

////////////////////////////////////////////////////////////////////////////////

var (
	inputFile  string
	outputFile string
	key        string
	value      string
)

////////////////////////////////////////////////////////////////////////////////

func main() {
	s := SampleStruct{
		StrVal:  "hello",
		IntVal:  42,
		BoolVal: true,
		StructVal: InnerStruct{
			InnerBool: false,
			InnerStr:  "world",
			InnerInt:  7,
		},
	}

	input, err := os.ReadFile(inputFile)
	if err != nil {
		panic(err)
	}
	data, err := pngembed.EmbedITXT(input, key, s)
	if err == nil {
		file, err := os.Create(outputFile)
		if err != nil {
			panic(err)
		}
		defer file.Close()

		// Write binary data
		_, err = file.Write(data)
		if err != nil {
			panic(err)
		}
		fmt.Printf("File '%s' was successfully embedded and saved as '%s' \n", inputFile, outputFile)
	}
}

func init() {
	flag.StringVar(&inputFile, "input", "image.png", "input file name for the png")
	flag.StringVar(&outputFile, "output", "out.png", "output file name for the png")
	flag.StringVar(&key, "key", "TEST_KEY", "key name for the data to inject")
	// flag.StringVar(&value, "value", "TEST_VALUE", "sample value to inject for key")

	flag.Parse()
	if len(inputFile) == 0 {
		fmt.Printf("Fatal error: No input file specified!\n")
		os.Exit(1)
	}
}

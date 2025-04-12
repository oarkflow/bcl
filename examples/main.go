package main

import (
	"fmt"
	"os"

	"github.com/oarkflow/bcl"
)

func main() {
	if len(os.Args) < 1 {
		panic("Provide bcl file")
	}
	file := os.Args[1]
	// Read configuration from file "config.bcl".
	cfg, err := os.ReadFile(file)
	if err != nil {
		panic(err)
	}

	var config map[string]any
	_, err = bcl.Unmarshal(cfg, &config)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshaled configuration from struct:")
	fmt.Printf("%+v\n", config)
}

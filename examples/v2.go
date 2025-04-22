package main

import (
	"errors"
	"fmt"

	"github.com/oarkflow/bcl"
)

func main() {
	bcl.RegisterFunction("test", func(args ...any) (any, error) {
		return nil, errors.New("test error")
	})
	var input = `
data, err = test("test")
if (err == undefined) {
	run = true
}
	`

	var cfg map[string]any
	nodes, err := bcl.Unmarshal([]byte(input), &cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshalled Config:")
	fmt.Printf("%+v\n\n", cfg)

	str := bcl.MarshalAST(nodes)
	fmt.Println("Marshaled AST:")
	fmt.Println(str)
}

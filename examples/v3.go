package main

import (
	"errors"
	"fmt"

	"github.com/oarkflow/bcl"
)

type Config struct {
	Combine []string `json:"combine"`
}

func main() {
	bcl.RegisterFunction("test", func(args ...any) (any, error) {
		return ".", nil
	})
	bcl.RegisterFunction("test_error", func(args ...any) (any, error) {
		return nil, errors.New("test error")
	})
	var input = `
combine = ["test", "best"]
	`

	var cfg Config
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

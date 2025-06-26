package main

import (
	"errors"
	"fmt"

	"github.com/oarkflow/bcl"
)

func main() {
	bcl.RegisterFunction("test", func(args ...any) (any, error) {
		return ".", nil
	})
	bcl.RegisterFunction("test_error", func(args ...any) (any, error) {
		return nil, errors.New("test error")
	})
	var input = `
dir, err = test_error()
if (!isNull(err)) {
	dir = "."
}
cmdOutput = @pipeline {
    step1 = test("pipeline step")
    step2 = add(10, 20)
    step3 = @exec(cmd="echo", args=["Pipeline executed", step1, step2], dir=dir)
	step1 -> step2 #ArrowNode
	step2 -> step3 #ArrowNode
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

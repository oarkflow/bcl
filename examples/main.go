package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/oarkflow/bcl"
)

func main() {
	bcl.RegisterFunction("upper", func(params ...any) (any, error) {
		if len(params) == 0 {
			return nil, errors.New("At least one param required")
		}
		str, ok := params[0].(string)
		if !ok {
			str = fmt.Sprint(params[0])
		}
		return strings.ToUpper(str), nil
	})
	// Set an environment variable for interpolation.
	_ = os.Setenv("APP_NAME", "dev")

	// Read configuration from file "config.bcl".
	cfg, err := os.ReadFile("main.bcl")
	if err != nil {
		panic(err)
	}

	var config map[string]any
	ast, err := bcl.Unmarshal(cfg, &config)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshaled configuration from struct:")
	fmt.Printf("%+v\n", config)

	marshaled := bcl.MarshalAST(ast)
	fmt.Println("\nMarshaled configuration from struct:")
	fmt.Println(string(marshaled))
}

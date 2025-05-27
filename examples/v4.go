package main

import (
	"fmt"

	"github.com/oarkflow/bcl"
)

func main() {
	var input = `
project_name = "myapp"
dist_dir     = "dist"

release = {
  previous_tag = "v1.2.0"
  tag          = "v1.3.0"
}

builds = [
  {
    goos   = "linux"
    goarch = "amd64"
    output = "dist/bin/${project_name}-linux-amd64"
  },
  {
    goos   = "linux"
    goarch = "arm64"
    output = "dist/bin/${project_name}-linux-arm64"
  },
  {
    goos   = "darwin"
    goarch = "amd64"
    output = "dist/bin/${project_name}-darwin-amd64"
  },
  {
    goos   = "darwin"
    goarch = "arm64"
    output = "dist/bin/${project_name}-darwin-arm64"
  },
  {
    goos   = "windows"
    goarch = "amd64"
    output = "dist/bin/${project_name}-windows-amd64.exe"
  },
]

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

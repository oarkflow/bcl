package main

import (
	"fmt"

	"github.com/oarkflow/bcl"
)

type Release struct {
	PreviousTag string `json:"previous_tag"`
	Name        string `json:"name"`
}

// Build filter usage in struct tags
type Build struct {
	GoOS   string `json:"goos"`
	GoArch string `json:"goarch"`
	Output string `json:"output"`
	Name   string `json:"name"`
}

type BuildConfig struct {
	ProjectName string    `json:"project_name"`
	DistDir     string    `json:"dist_dir"`
	Release     []Release `json:"release:all"`
	Build       Build     `json:"build:last"`
	BuildLast   Build     `json:"build:0"`
	BuildFirst  Build     `json:"build:first"`
	BuildArm    Build     `json:"build:name,linux-arm64"`
	Builds      []Build   `json:"build:0-2"`
}

func main() {
	var input = `
project_name = "myapp"
dist_dir     = "dist"

release "v1.3.0" {
  previous_tag = "v1.2.0"
}

build "linux-amd64" {
	goos   = "linux"
    goarch = "amd64"
    output = "dist/bin/${project_name}-linux-amd64"
}

build "linux-arm64" {
	goos   = "linux"
    goarch = "arm64"
    output = "dist/bin/${project_name}-linux-arm64"
}
	`

	var cfg BuildConfig
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

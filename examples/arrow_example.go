package main

import (
	"fmt"
	"log"

	"github.com/oarkflow/bcl"
)

type EdgeConfig struct {
	Edge []Edge `json:"Edge"`
}

type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label"`
	Weight int    `json:"weight"`
}

func main() {
	// An arrow expression (edge) example.
	// Source and target are optionally quoted, and are followed by a block.
	// In this case: "nodeA" -> "nodeB" { label = "Edge from A to B" weight = 100 }
	input := `
"nodeA" -> "nodeB"
`
	var data EdgeConfig
	// Parse input into AST nodes.
	nodes, err := bcl.Unmarshal([]byte(input), &data)
	if err != nil {
		log.Fatalf("Parsing failed: %v", err)
	}
	fmt.Println(data)
	fmt.Println("Parsed Arrow Node:")
	for _, node := range nodes {
		fmt.Println(node.ToBCL(""))
	}
}

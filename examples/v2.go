package main

import (
	"encoding/json"
	"fmt"

	"github.com/oarkflow/bcl"
)

type Migration struct {
	Name        string        `json:"name"`
	Version     string        `json:"Version"`
	Description string        `json:"Description"`
	Up          []Operation   `json:"Up"`
	Down        []Operation   `json:"Down"`
	Transaction []Transaction `json:"Transaction"`
	Validate    []Validation  `json:"Validate"`
}

type Operation struct {
	Name         string         `json:"name"`
	AlterTable   []AlterTable   `json:"AlterTable"`
	DeleteData   []DeleteData   `json:"DeleteData"`
	DropEnumType []DropEnumType `json:"DropEnumType"`
}

type AlterTable struct {
	Name         string         `json:"name"`
	AddColumn    []AddColumn    `json:"AddColumn"`
	DropColumn   []DropColumn   `json:"DropColumn"`
	RenameColumn []RenameColumn `json:"RenameColumn"`
}

type AddColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Check    string `json:"check,omitempty"`
	Size     int    `json:"size,omitempty"`
}

type DropColumn struct {
	Name string `json:"name"`
}

type RenameColumn struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type DeleteData struct {
	Name  string `json:"name"`
	Where string `json:"Where"`
}

type DropEnumType struct {
	Name     string `json:"name"`
	IfExists bool   `json:"IfExists"`
}

type Transaction struct {
	Name           string `json:"name"`
	IsolationLevel string `json:"IsolationLevel"`
	Mode           string `json:"Mode"`
}

type Validation struct {
	Name         string   `json:"name"`
	PreUpChecks  []string `json:"PreUpChecks"`
	PostUpChecks []string `json:"PostUpChecks"`
}

type Config struct {
	Migrations []Migration `json:"Migration"`
}

func main() {
	var input = `

	`
	var cfg Config
	_, err := bcl.Unmarshal([]byte(input), &cfg)
	if err != nil {
		panic(err)
	}
	bt, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshalled Config:")
	fmt.Printf("%+v\n\n", string(bt))

}

package main

import (
	"fmt"
	"os"

	"github.com/oarkflow/bcl"
)

func main() {
	file := "migration.bcl"
	// Read configuration from file "config.bcl".
	cfg, err := os.ReadFile(file)
	if err != nil {
		panic(err)
	}

	var config Config
	_, err = bcl.Unmarshal(cfg, &config)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshaled configuration from struct:")
	fmt.Printf("%+v\n", config.Migration.Up.CreateTable)
}

type Config struct {
	Migration Migration `json:"Migration"`
}

type Migration struct {
	Name        string    `json:"name"`
	Version     string    `json:"Version"`
	Description string    `json:"Description"`
	Connection  string    `json:"Connection"`
	Up          Operation `json:"Up"`
	Down        Operation `json:"Down"`
}

type Operation struct {
	Name        string        `json:"name"`
	AlterTable  []AlterTable  `json:"AlterTable,omitempty"`
	CreateTable []CreateTable `json:"CreateTable,omitempty"`
}

type AlterTable struct {
	Name      string      `json:"name"`
	AddColumn []AddColumn `json:"AddColumn"`
}

type CreateTable struct {
	Name       string      `json:"name"`
	Columns    []AddColumn `json:"Column"`
	PrimaryKey []string    `json:"PrimaryKey,omitempty"`
}

type AddColumn struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Nullable      bool   `json:"nullable"`
	Default       any    `json:"default,omitempty"`
	Check         string `json:"check,omitempty"`
	Size          int    `json:"size,omitempty"`
	Scale         int    `json:"scale,omitempty"`
	AutoIncrement bool   `json:"auto_increment,omitempty"`
	PrimaryKey    bool   `json:"primary_key,omitempty"`
	Unique        bool   `json:"unique,omitempty"`
	Index         bool   `json:"index,omitempty"`
}

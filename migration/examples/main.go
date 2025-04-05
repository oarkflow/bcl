package main

import (
	"github.com/oarkflow/squealx"

	"github.com/oarkflow/migration/cmd"
)

func main() {
	dbConfig := squealx.Config{
		Host:     "localhost",
		Port:     3306,
		Driver:   "mysql",
		Username: "root",
		Password: "T#sT1234",
		Database: "tests",
		Params: map[string]any{
			"parseTime": true,
		},
	}
	config := cmd.Config{Config: dbConfig}
	err := cmd.Run(dbConfig.Driver, config)
	if err != nil {
		panic(err)
	}
}

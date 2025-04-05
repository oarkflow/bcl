package main

import (
	"github.com/oarkflow/squealx"

	"github.com/oarkflow/migration"
	"github.com/oarkflow/migration/cmd"
)

func main() {
	dbConfig := squealx.Config{
		Host:     "localhost",
		Port:     5432,
		Driver:   "postgres",
		Username: "postgres",
		Password: "postgres",
		Database: "tests",
	}
	config := cmd.Config{Config: dbConfig}
	err := cmd.Run(migration.DialectPostgres, config)
	if err != nil {
		panic(err)
	}
}

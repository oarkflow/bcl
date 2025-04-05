package main

import (
	"github.com/oarkflow/migration"
	"github.com/oarkflow/migration/cmd"
)

func main() {
	cmd.Run(migration.DialectPostgres)
}

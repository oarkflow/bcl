package main

import (
	"flag"

	"github.com/oarkflow/migration"
	"github.com/oarkflow/migration/cmd"
)

func main() {
	dialectFlag := flag.String("dialect", migration.DialectPostgres, "SQL dialect to use (postgres, mysql, sqlite)")
	flag.Parse()

	// Initialize the migration driver.
	// For this implementation, we use the DummyDriver.
	migrationDir := "migrations"
	historyFile := "migration_history.txt"
	driver := migration.NewDummyDriver(migrationDir, historyFile, *dialectFlag)

	cmd.Run(driver, migrationDir)
}

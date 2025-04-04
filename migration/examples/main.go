package main

import (
	"flag"
	"io/fs"
	"log"
	"os"

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

	// Create migrations directory if it does not exist.
	if err := os.MkdirAll(migrationDir, fs.ModePerm); err != nil {
		log.Fatalf("Failed to create migration directory: %v", err)
	}
	cmd.Run(driver)
}

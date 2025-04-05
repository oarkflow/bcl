package main

import (
	"log"
	"os"

	"github.com/oarkflow/bcl"

	"github.com/oarkflow/migration"
)

func main() {
	data, err := os.ReadFile("migrations/migration.bcl")
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	var cfg migration.Config
	if _, err := bcl.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to unmarshal migration file: %v", err)
	}
	dialect := "postgres"
	for _, mig := range cfg.Migrations {
		upQueries, err := mig.ToSQL(dialect, true)
		if err != nil {
			log.Fatalf("Error generating SQL for up migration '%s': %v", mig.Name, err)
		}
		if len(mig.Transaction) > 1 {
			log.Printf("Warning: More than one transaction provided in migration '%s'. Only the first one will be used.", mig.Name)
		}
		log.Printf("Generated SQL for migration (up) - %s:", mig.Name)
		for _, query := range upQueries {
			log.Println(query)
		}
		downQueries, err := mig.ToSQL(dialect, false)
		if err != nil {
			log.Fatalf("Error generating SQL for down migration '%s': %v", mig.Name, err)
		}
		if len(downQueries) == 0 {
			log.Printf("Warning: No down migration queries generated for migration '%s'.", mig.Name)
		}
		log.Printf("Generated SQL for migration (down) - %s:", mig.Name)
		for _, query := range downQueries {
			log.Println(query)
		}
		log.Printf("Completed migration: %s", mig.Name)
	}
	log.Println("All migrations completed successfully.")
}

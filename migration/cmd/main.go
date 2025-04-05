package cmd

import (
	"github.com/oarkflow/migration"
)

func Run(dialect string) {
	manager := migration.NewManager()
	manager.SetDialect(dialect)
	manager.Run()
}

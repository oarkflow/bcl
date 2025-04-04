package cmd

import (
	"io/fs"
	"log"
	"os"

	"github.com/oarkflow/cli"
	"github.com/oarkflow/cli/console"
	"github.com/oarkflow/cli/contracts"

	"github.com/oarkflow/migration"
)

func Run(driver migration.Driver, dir string) {
	if err := os.MkdirAll(dir, fs.ModePerm); err != nil {
		log.Fatalf("Failed to create migration directory: %v", err)
	}

	cli.SetName("Migration")
	cli.SetVersion("v0.0.1")
	app := cli.New()
	client := app.Instance.Client()
	client.Register([]contracts.Command{
		console.NewListCommand(client),
		&migration.MakeMigrationCommand{
			Driver: driver,
		},
		&migration.MigrateCommand{
			Driver: driver,
		},
		&migration.RollbackCommand{
			Driver: driver,
		},
		&migration.ResetCommand{
			Driver: driver,
		},
		&migration.ValidateCommand{
			Driver: driver,
		},
	})
	client.Run(os.Args, true)
}

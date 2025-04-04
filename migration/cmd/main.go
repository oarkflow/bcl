package cmd

import (
	"os"

	"github.com/oarkflow/cli"
	"github.com/oarkflow/cli/console"
	"github.com/oarkflow/cli/contracts"

	"github.com/oarkflow/migration"
)

func Run(driver migration.MigrationDriver) {
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

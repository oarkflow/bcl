package cmd

import (
	"github.com/oarkflow/squealx"

	"github.com/oarkflow/migration"
)

type Config struct {
	squealx.Config
}

func Run(dialect string, cfg ...Config) error {
	var config Config
	if len(cfg) > 0 {
		config = cfg[0]
	}
	var opts []migration.ManagerOption
	if config.Config.Driver != "" {
		dsn := config.ToString()
		if dsn != "" {
			driver, err := migration.NewDriver(config.Config.Driver, dsn)
			if err != nil {
				return err
			}
			opts = append(opts, migration.WithDriver(driver))
			historyDriver, err := migration.NewHistoryDriver("db", dialect, dsn)
			if err != nil {
				return err
			}
			opts = append(opts, migration.WithHistoryDriver(historyDriver))
		}
	}
	manager := migration.NewManager(opts...)
	manager.SetDialect(dialect)
	manager.Run()
	return nil
}

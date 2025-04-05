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
	manager := migration.NewManager()
	if config.Config.Driver != "" {
		dsn := config.ToString()
		if dsn != "" {
			driver, err := migration.NewDriver(config.Config.Driver, dsn)
			if err != nil {
				return err
			}
			manager.SetDriver(driver)
		}
	}
	manager.SetDialect(dialect)
	manager.Run()
	return nil
}

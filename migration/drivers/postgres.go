package drivers

import (
	"fmt"

	"github.com/oarkflow/squealx"
	"github.com/oarkflow/squealx/drivers/postgres"
)

type PostgresDriver struct {
	db *squealx.DB
}

func NewPostgresDriver(dsn string) (*PostgresDriver, error) {
	db, err := postgres.Open(dsn, "postgres")
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return &PostgresDriver{db: db}, nil
}

func (p *PostgresDriver) ApplySQL(queries []string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	for _, query := range queries {
		if _, err := tx.Exec(query); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute query [%s]: %w", query, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (m *PostgresDriver) DB() *squealx.DB {
	return m.db
}

package drivers

import (
	"fmt"
	"strings"

	"github.com/oarkflow/squealx"
	"github.com/oarkflow/squealx/drivers/mysql"
)

type MySQLDriver struct {
	db *squealx.DB
}

func NewMySQLDriver(dsn string) (*MySQLDriver, error) {
	db, err := mysql.Open(dsn, "mysql")
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return &MySQLDriver{db: db}, nil
}

func (m *MySQLDriver) ApplySQL(migrations []string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	for _, query := range migrations {
		queries := strings.Split(query, ";")
		for _, q := range queries {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}
			if _, err := tx.Exec(q); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to execute query [%s]: %w", query, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (m *MySQLDriver) DB() *squealx.DB {
	return m.db
}

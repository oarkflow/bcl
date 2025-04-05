package drivers

import (
	"fmt"
	"strings"

	"github.com/oarkflow/squealx"
	"github.com/oarkflow/squealx/drivers/sqlite"
)

type SQLiteDriver struct {
	db *squealx.DB
}

func NewSQLiteDriver(dbPath string) (*SQLiteDriver, error) {
	db, err := sqlite.Open(dbPath, "sqlite3")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}
	return &SQLiteDriver{db: db}, nil
}

func (s *SQLiteDriver) ApplySQL(migrations []string) error {
	tx, err := s.db.Begin()
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

func (m *SQLiteDriver) DB() *squealx.DB {
	return m.db
}

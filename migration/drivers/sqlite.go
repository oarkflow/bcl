package drivers

import (
	"fmt"

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

func (s *SQLiteDriver) ApplySQL(queries []string) error {
	tx, err := s.db.Begin()
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

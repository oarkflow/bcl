package migration

import (
	"fmt"
	"os"
	"time"

	"github.com/oarkflow/json"
	"github.com/oarkflow/squealx"
	"github.com/oarkflow/squealx/drivers/mysql"
	"github.com/oarkflow/squealx/drivers/postgres"
	"github.com/oarkflow/squealx/drivers/sqlite"
)

// MigrationHistory holds a migration history record.
type MigrationHistory struct {
	Name        string    `json:"name" db:"name"`
	Version     string    `json:"version" db:"version"`
	Description string    `json:"description" db:"description"`
	Checksum    string    `json:"checksum" db:"checksum"`
	AppliedAt   time.Time `json:"applied_at" db:"applied_at"`
}

// HistoryDriver defines an interface to store migration history.
type HistoryDriver interface {
	Save(history MigrationHistory) error
	Load() ([]MigrationHistory, error)
	// ValidateStorage checks if the history storage exists and is accessible.
	ValidateStorage() error
}

// FileHistoryDriver implements the HistoryDriver interface using a file.
type FileHistoryDriver struct {
	filePath string
}

// NewFileHistoryDriver creates a new file-based history driver.
func NewFileHistoryDriver(filePath string) *FileHistoryDriver {
	return &FileHistoryDriver{
		filePath: filePath,
	}
}

func (f *FileHistoryDriver) Save(history MigrationHistory) error {
	histories, err := f.Load()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	histories = append(histories, history)
	data, err := json.Marshal(histories)
	if err != nil {
		return err
	}
	return os.WriteFile(f.filePath, data, 0644)
}

func (f *FileHistoryDriver) Load() ([]MigrationHistory, error) {
	data, err := os.ReadFile(f.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []MigrationHistory{}, nil
		}
		return nil, err
	}
	var histories []MigrationHistory
	if err := json.Unmarshal(data, &histories); err != nil {
		return nil, err
	}
	return histories, nil
}

func (f *FileHistoryDriver) ValidateStorage() error {
	// If file does not exist, create an empty storage file.
	if _, err := os.Stat(f.filePath); os.IsNotExist(err) {
		empty := []byte("[]")
		if err := os.WriteFile(f.filePath, empty, 0644); err != nil {
			return err
		}
	}
	return nil
}

// DatabaseHistoryDriver implements the HistoryDriver interface using a database.
type DatabaseHistoryDriver struct {
	db      *squealx.DB
	dialect string
	table   string
}

func NewDB(dialect, dsn string) (*squealx.DB, error) {
	switch dialect {
	case "postgres":
		return postgres.Open(dsn, "postgres")
	case "mysql":
		return mysql.Open(dsn, "mysql")
	case "sqlite":
		return sqlite.Open(dsn, "sqlite")
	default:
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// NewDatabaseHistoryDriver creates a new database history driver using squealx.
func NewDatabaseHistoryDriver(dialect, dsn string, tables ...string) (HistoryDriver, error) {
	db, err := NewDB(dialect, dsn)
	if err != nil {
		return nil, err
	}
	table := "migrations"
	if len(tables) > 0 {
		table = tables[0]
	}
	dial := getDialect(dialect)
	stmt := CreateTable{
		Name: table,
		Columns: []AddColumn{
			{Name: "id", Type: "SERIAL", PrimaryKey: true, Unique: true, Index: true},
			{Name: "name", Type: "string", Index: true},
			{Name: "version", Type: "string"},
			{Name: "description", Type: "string", Size: 500},
			{Name: "checksum", Type: "string"},
			{Name: "applied_at", Type: "datetime"},
		},
	}
	existsQuery := dial.TableExistsSQL(table)
	var exists bool
	err = db.Select(&exists, existsQuery)
	if err != nil {
		return nil, err
	}
	if !exists {
		query, err := dial.CreateTableSQL(stmt, true)
		if err != nil {
			return nil, err
		}
		if _, err := db.Exec(query); err != nil {
			return nil, err
		}
	}
	return &DatabaseHistoryDriver{db: db, dialect: dialect, table: table}, nil
}

func (d *DatabaseHistoryDriver) Save(history MigrationHistory) error {
	dial := getDialect(d.dialect)
	cols := []string{"name", "version", "description", "checksum", "applied_at"}
	vals := []any{history.Name, history.Version, history.Description, history.Checksum, history.AppliedAt.Format(time.RFC3339)}
	query, err := dial.InsertSQL(d.table, cols, vals)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(query)
	return err
}

func (d *DatabaseHistoryDriver) Load() ([]MigrationHistory, error) {
	var histories []MigrationHistory
	query := fmt.Sprintf(`SELECT * FROM %s ORDER BY applied_at ASC;`, d.table)
	err := d.db.Select(&histories, query)
	if err != nil {
		return nil, err
	}
	return histories, nil
}

func (d *DatabaseHistoryDriver) ValidateStorage() error {
	// Assume that the migration_history table has been created already.
	return nil
}

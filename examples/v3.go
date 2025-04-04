package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
)

// ----------------------
// Dialect Constants
// ----------------------
const (
	DialectPostgres = "postgres"
	DialectMySQL    = "mysql"
	DialectSQLite   = "sqlite"
)

// ----------------------
// Migration Configuration Types
// ----------------------
type Migration struct {
	Name        string        `json:"name"`
	Version     string        `json:"Version"`
	Description string        `json:"Description"`
	Up          []Operation   `json:"Up"`
	Down        []Operation   `json:"Down"`
	Transaction []Transaction `json:"Transaction"`
	Validate    []Validation  `json:"Validate"`
}

type Operation struct {
	Name                 string                 `json:"name"`
	AlterTable           []AlterTable           `json:"AlterTable"`
	DeleteData           []DeleteData           `json:"DeleteData"`
	DropEnumType         []DropEnumType         `json:"DropEnumType"`
	DropRowPolicy        []DropRowPolicy        `json:"DropRowPolicy,omitempty"`
	DropMaterializedView []DropMaterializedView `json:"DropMaterializedView,omitempty"`
	DropTable            []DropTable            `json:"DropTable,omitempty"`
	DropSchema           []DropSchema           `json:"DropSchema,omitempty"`
}

type AlterTable struct {
	Name         string         `json:"name"`
	AddColumn    []AddColumn    `json:"AddColumn"`
	DropColumn   []DropColumn   `json:"DropColumn"`
	RenameColumn []RenameColumn `json:"RenameColumn"`
}

type AddColumn struct {
	Name          string      `json:"name"`
	Type          string      `json:"type"`
	Nullable      bool        `json:"nullable"`
	Default       string      `json:"default,omitempty"`
	Check         string      `json:"check,omitempty"`
	Size          int         `json:"size,omitempty"`
	AutoIncrement bool        `json:"auto_increment,omitempty"`
	PrimaryKey    bool        `json:"primary_key,omitempty"`
	Unique        bool        `json:"unique,omitempty"`
	Index         bool        `json:"index,omitempty"`
	ForeignKey    *ForeignKey `json:"foreign_key,omitempty"`
}

type ForeignKey struct {
	ReferenceTable  string `json:"reference_table"`
	ReferenceColumn string `json:"reference_column"`
	OnDelete        string `json:"on_delete,omitempty"`
	OnUpdate        string `json:"on_update,omitempty"`
}

type DropColumn struct {
	Name string `json:"name"`
}

type RenameColumn struct {
	From string `json:"from"`
	To   string `json:"to"`
	// For MySQL, we now require the new column type to be provided.
	Type string `json:"type,omitempty"`
}

type DeleteData struct {
	Name  string `json:"name"`
	Where string `json:"Where"`
}

type DropEnumType struct {
	Name     string `json:"name"`
	IfExists bool   `json:"IfExists"`
}

type DropRowPolicy struct {
	Name     string `json:"name"`
	Table    string `json:"Table"`
	IfExists bool   `json:"if_exists,omitempty"`
}

type DropMaterializedView struct {
	Name     string `json:"name"`
	IfExists bool   `json:"if_exists,omitempty"`
}

type DropTable struct {
	Name    string `json:"name"`
	Cascade bool   `json:"cascade,omitempty"`
}

type DropSchema struct {
	Name     string `json:"name"`
	Cascade  bool   `json:"cascade,omitempty"`
	IfExists bool   `json:"if_exists,omitempty"`
}

type Transaction struct {
	Name           string `json:"name"`
	IsolationLevel string `json:"IsolationLevel"`
	Mode           string `json:"Mode"`
}

type Validation struct {
	Name         string   `json:"name"`
	PreUpChecks  []string `json:"PreUpChecks"`
	PostUpChecks []string `json:"PostUpChecks"`
}

type Config struct {
	Migrations []Migration `json:"Migration"`
}

// ----------------------
// DataType Mapping Functions
// ----------------------
// mapDataType maps a generic type to a dialect-specific SQL type.
func mapDataType(dialect, genericType string, size int, autoIncrement bool, primaryKey bool) string {
	lowerType := strings.ToLower(genericType)
	switch dialect {
	case DialectPostgres:
		switch lowerType {
		case "string":
			if size > 0 {
				return fmt.Sprintf("VARCHAR(%d)", size)
			}
			return "TEXT"
		case "number":
			if autoIncrement {
				return "SERIAL"
			}
			return "INTEGER"
		case "boolean":
			return "BOOLEAN"
		case "date":
			return "DATE"
		case "datetime":
			return "TIMESTAMP"
		default:
			return genericType
		}
	case DialectMySQL:
		switch lowerType {
		case "string":
			if size > 0 {
				return fmt.Sprintf("VARCHAR(%d)", size)
			}
			return "TEXT"
		case "number":
			// For auto increment, the type remains INT and AUTO_INCREMENT is appended.
			return "INT"
		case "boolean":
			return "TINYINT(1)"
		case "date":
			return "DATE"
		case "datetime":
			return "DATETIME"
		default:
			return genericType
		}
	case DialectSQLite:
		switch lowerType {
		case "string":
			if size > 0 {
				return fmt.Sprintf("VARCHAR(%d)", size)
			}
			return "TEXT"
		case "number":
			if autoIncrement && primaryKey {
				return "INTEGER" // SQLite requires INTEGER PRIMARY KEY AUTOINCREMENT.
			}
			return "INTEGER"
		case "boolean":
			return "BOOLEAN"
		case "date":
			return "DATE"
		case "datetime":
			return "DATETIME"
		default:
			return genericType
		}
	default:
		return genericType
	}
}

// ----------------------
// SQL Generation Methods for Each Operation
// ----------------------

// AddColumn: Generates SQL for adding a column.
// Returns the main ALTER TABLE statement plus any additional statements (unique indexes, foreign keys, etc.)
func (a AddColumn) ToSQL(dialect, tableName string) ([]string, error) {
	var queries []string
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", tableName, a.Name))
	dataType := mapDataType(dialect, a.Type, a.Size, a.AutoIncrement, a.PrimaryKey)
	sb.WriteString(dataType)

	if dialect == DialectMySQL && a.AutoIncrement {
		sb.WriteString(" AUTO_INCREMENT")
	}

	if dialect == DialectSQLite && a.AutoIncrement && a.PrimaryKey {
		// In SQLite, auto increment primary key must be defined as INTEGER PRIMARY KEY AUTOINCREMENT.
		sb.Reset()
		sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INTEGER PRIMARY KEY AUTOINCREMENT", tableName, a.Name))
	}

	if !a.Nullable {
		sb.WriteString(" NOT NULL")
	}

	if a.Default != "" {
		sb.WriteString(fmt.Sprintf(" DEFAULT %s", a.Default))
	}

	if a.Check != "" {
		sb.WriteString(fmt.Sprintf(" CHECK (%s)", a.Check))
	}

	sb.WriteString(";")
	queries = append(queries, sb.String())

	// If Unique is true, create a separate unique index.
	if a.Unique {
		uniqueSQL := createUniqueIndexSQL(dialect, tableName, a.Name)
		queries = append(queries, uniqueSQL)
	}

	// If Index is true, create a normal index.
	if a.Index {
		indexSQL := createIndexSQL(dialect, tableName, a.Name)
		queries = append(queries, indexSQL)
	}

	// If a ForeignKey is defined, generate its SQL.
	if a.ForeignKey != nil {
		fkSQL, err := foreignKeyToSQL(dialect, tableName, a.Name, *a.ForeignKey)
		if err != nil {
			return nil, err
		}
		queries = append(queries, fkSQL)
	}

	return queries, nil
}

// foreignKeyToSQL generates SQL for adding a foreign key constraint.
func foreignKeyToSQL(dialect, tableName, column string, fk ForeignKey) (string, error) {
	const constraintPrefix = "fk_"
	switch dialect {
	case DialectPostgres:
		sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s%s FOREIGN KEY (%s) REFERENCES %s(%s)",
			tableName, constraintPrefix, column, column, fk.ReferenceTable, fk.ReferenceColumn)
		if fk.OnDelete != "" {
			sql += fmt.Sprintf(" ON DELETE %s", fk.OnDelete)
		}
		if fk.OnUpdate != "" {
			sql += fmt.Sprintf(" ON UPDATE %s", fk.OnUpdate)
		}
		return sql + ";", nil
	case DialectMySQL:
		sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s%s FOREIGN KEY (%s) REFERENCES %s(%s)",
			tableName, constraintPrefix, column, column, fk.ReferenceTable, fk.ReferenceColumn)
		if fk.OnDelete != "" {
			sql += fmt.Sprintf(" ON DELETE %s", fk.OnDelete)
		}
		if fk.OnUpdate != "" {
			sql += fmt.Sprintf(" ON UPDATE %s", fk.OnUpdate)
		}
		return sql + ";", nil
	case DialectSQLite:
		return "", errors.New("SQLite foreign keys must be defined at table creation")
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// createUniqueIndexSQL generates SQL for creating a unique index.
func createUniqueIndexSQL(dialect, tableName, column string) string {
	indexName := fmt.Sprintf("uniq_%s_%s", tableName, column)
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s);", indexName, tableName, column)
}

// createIndexSQL generates SQL for creating a normal index.
func createIndexSQL(dialect, tableName, column string) string {
	indexName := fmt.Sprintf("idx_%s_%s", tableName, column)
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s);", indexName, tableName, column)
}

// DropColumn: Generates SQL for dropping a column.
func (d DropColumn) ToSQL(dialect, tableName string) (string, error) {
	switch dialect {
	case DialectPostgres, DialectMySQL, DialectSQLite:
		// Note: SQLite does not support DROP COLUMN directly.
		if dialect == DialectSQLite {
			return "", errors.New("SQLite does not support DROP COLUMN directly; table recreation is required")
		}
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, d.Name), nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// RenameColumn: Generates SQL for renaming a column.
// For MySQL, the new column type must be provided.
func (r RenameColumn) ToSQL(dialect, tableName string) (string, error) {
	switch dialect {
	case DialectPostgres:
		return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", tableName, r.From, r.To), nil
	case DialectMySQL:
		if r.Type == "" {
			return "", errors.New("MySQL requires column type for renaming column")
		}
		return fmt.Sprintf("ALTER TABLE %s CHANGE %s %s %s;", tableName, r.From, r.To, r.Type), nil
	case DialectSQLite:
		return "", errors.New("SQLite does not support RENAME COLUMN directly; table recreation is required")
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// DeleteData: Generates SQL for a DELETE statement.
func (d DeleteData) ToSQL(dialect string) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", d.Name, d.Where), nil
}

// DropEnumType: Generates SQL for dropping an enum type.
func (d DropEnumType) ToSQL(dialect string) (string, error) {
	switch dialect {
	case DialectPostgres:
		if d.IfExists {
			return fmt.Sprintf("DROP TYPE IF EXISTS %s;", d.Name), nil
		}
		return fmt.Sprintf("DROP TYPE %s;", d.Name), nil
	case DialectMySQL, DialectSQLite:
		return "", errors.New("enum types are not supported in this dialect")
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// DropRowPolicy: Generates SQL for dropping a row-level security policy.
func (drp DropRowPolicy) ToSQL(dialect string) (string, error) {
	switch dialect {
	case DialectPostgres:
		if drp.IfExists {
			return fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s;", drp.Name, drp.Table), nil
		}
		return fmt.Sprintf("DROP POLICY %s ON %s;", drp.Name, drp.Table), nil
	case DialectMySQL, DialectSQLite:
		return "", errors.New("DROP ROW POLICY is not supported in this dialect")
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// DropMaterializedView: Generates SQL for dropping a materialized view.
func (dmv DropMaterializedView) ToSQL(dialect string) (string, error) {
	switch dialect {
	case DialectPostgres:
		if dmv.IfExists {
			return fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s;", dmv.Name), nil
		}
		return fmt.Sprintf("DROP MATERIALIZED VIEW %s;", dmv.Name), nil
	case DialectMySQL, DialectSQLite:
		return "", errors.New("DROP MATERIALIZED VIEW is not supported in this dialect")
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// DropTable: Generates SQL for dropping a table.
func (dt DropTable) ToSQL(dialect string) (string, error) {
	switch dialect {
	case DialectPostgres:
		cascade := ""
		if dt.Cascade {
			cascade = " CASCADE"
		}
		return fmt.Sprintf("DROP TABLE IF EXISTS %s%s;", dt.Name, cascade), nil
	case DialectMySQL, DialectSQLite:
		return fmt.Sprintf("DROP TABLE IF EXISTS %s;", dt.Name), nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// DropSchema: Generates SQL for dropping a schema.
func (ds DropSchema) ToSQL(dialect string) (string, error) {
	switch dialect {
	case DialectPostgres:
		exists := ""
		if ds.IfExists {
			exists = " IF EXISTS"
		}
		cascade := ""
		if ds.Cascade {
			cascade = " CASCADE"
		}
		return fmt.Sprintf("DROP SCHEMA%s %s%s;", exists, ds.Name, cascade), nil
	case DialectMySQL, DialectSQLite:
		return "", errors.New("DROP SCHEMA is not supported in this dialect")
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// ----------------------
// Higher-level SQL Generation Methods
// ----------------------

// AlterTable: Generates SQL for all AlterTable operations.
func (at AlterTable) ToSQL(dialect string) ([]string, error) {
	var queries []string
	// Process AddColumn statements.
	for _, addCol := range at.AddColumn {
		qList, err := addCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	// Process DropColumn statements.
	for _, dropCol := range at.DropColumn {
		q, err := dropCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	// Process RenameColumn statements.
	for _, renameCol := range at.RenameColumn {
		q, err := renameCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, nil
}

// Operation: Generates SQL for all operations within an Operation.
func (op Operation) ToSQL(dialect string) ([]string, error) {
	var queries []string
	// AlterTable operations.
	for _, at := range op.AlterTable {
		qList, err := at.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	// DeleteData operations.
	for _, dd := range op.DeleteData {
		q, err := dd.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	// DropEnumType operations.
	for _, de := range op.DropEnumType {
		q, err := de.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	// DropRowPolicy operations.
	for _, drp := range op.DropRowPolicy {
		q, err := drp.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	// DropMaterializedView operations.
	for _, dmv := range op.DropMaterializedView {
		q, err := dmv.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	// DropTable operations.
	for _, dt := range op.DropTable {
		q, err := dt.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	// DropSchema operations.
	for _, ds := range op.DropSchema {
		q, err := ds.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, nil
}

// Migration: Generates SQL for the entire migration (either Up or Down).
func (m Migration) ToSQL(dialect string, up bool) ([]string, error) {
	var queries []string
	var ops []Operation
	if up {
		ops = m.Up
	} else {
		ops = m.Down
	}
	for _, op := range ops {
		qList, err := op.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	return queries, nil
}

// wrapInTransaction wraps the list of queries in a transaction block.
func wrapInTransaction(queries []string) []string {
	txQueries := []string{"BEGIN;"}
	txQueries = append(txQueries, queries...)
	txQueries = append(txQueries, "COMMIT;")
	return txQueries
}

// createMigrationHistoryTableSQL returns the SQL to create a migration history table.
func createMigrationHistoryTableSQL(dialect string) (string, error) {
	switch dialect {
	case DialectPostgres:
		return "CREATE TABLE IF NOT EXISTS migrations (id SERIAL PRIMARY KEY, name VARCHAR(255) NOT NULL, version VARCHAR(50) NOT NULL, applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);", nil
	case DialectMySQL:
		return "CREATE TABLE IF NOT EXISTS migrations (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255) NOT NULL, version VARCHAR(50) NOT NULL, applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);", nil
	case DialectSQLite:
		return "CREATE TABLE IF NOT EXISTS migrations (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, version TEXT NOT NULL, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP);", nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

// ----------------------
// Main Function & Example Usage
// ----------------------
func main() {
	// Example input in BCL DSL format.
	input := []byte(`
Migration "explicit_operations" {
  Version = "1.0.0-beta"
  Description = "Migration with explicit operation labeling"
  Up {
    AlterTable "core.users" {
      AddColumn "email" {
        type = "string"
        size = 255
        unique = true
        check = "email ~* '@'"
      }
      DropColumn "temporary_flag" {}
      RenameColumn {
        from = "signup_date"
        to = "created_at"
        type = "TIMESTAMP" // Required for MySQL; ignored for Postgres
      }
    }
    AlterTable "core.products" {
      AddColumn "sku" {
        type = "number"
        size = 255
      }
      RenameColumn {
        from = "added_date"
        to = "created_at"
        type = "DATETIME"
      }
    }
  }
  Down {
    DropRowPolicy "user_access_policy" {
      Table = "core.users"
      if_exists = true
    }
    DropMaterializedView "core.active_users" {
      if_exists = true
    }
    AlterTable "core.users" {
      AddColumn "temporary_flag" {
        type = "boolean"
        nullable = true
      }
      DropColumn "email" {}
      RenameColumn {
        from = "created_at"
        to = "signup_date"
        type = "TIMESTAMP"
      }
    }
    DeleteData "core.users" {
      Where = "username LIKE 'admin%'"
    }
    DropTable "core.profiles" {
      cascade = true
    }
    DropTable "core.users" {
      cascade = true
    }
    DropEnumType "core.user_role" {
      IfExists = true
    }
    DropSchema "core" {
      cascade = true
      if_exists = true
    }
  }
  Validate {
    PreUpChecks = [
      "schema_not_exists('core')",
      "table_empty('legacy.users')"
    ]
    PostUpChecks = [
      "index_exists('core.idx_active_users')",
      "fk_exists('core.profiles_user_id_fkey')"
    ]
  }
  Transaction {
    Mode = "atomic"
    IsolationLevel = "read_committed"
  }
}
`)

	var cfg Config
	if _, err := bcl.Unmarshal(input, &cfg); err != nil {
		panic(err)
	}

	// Choose the target dialect.
	dialect := DialectPostgres // Change to DialectMySQL or DialectSQLite as needed.

	// Generate migration history table SQL.
	historySQL, err := createMigrationHistoryTableSQL(dialect)
	if err != nil {
		fmt.Println("Error generating migration history table SQL:", err)
		return
	}
	fmt.Println("Migration History Table SQL:")
	fmt.Println(historySQL)
	fmt.Println()

	// For each migration in the config, generate both up and down SQL.
	for _, migration := range cfg.Migrations {
		// Generate UP migration SQL.
		upQueries, err := migration.ToSQL(dialect, true)
		if err != nil {
			fmt.Println("Error generating SQL for up migration:", err)
			return
		}
		// Wrap in a transaction.
		upQueries = wrapInTransaction(upQueries)
		fmt.Printf("Generated SQL for migration (up) - %s:\n", migration.Name)
		for _, query := range upQueries {
			fmt.Println(query)
		}
		fmt.Println()

		// Generate DOWN migration SQL.
		downQueries, err := migration.ToSQL(dialect, false)
		if err != nil {
			fmt.Println("Error generating SQL for down migration:", err)
			return
		}
		// Wrap in a transaction.
		downQueries = wrapInTransaction(downQueries)
		fmt.Printf("Generated SQL for migration (down) - %s:\n", migration.Name)
		for _, query := range downQueries {
			fmt.Println(query)
		}
		fmt.Println()
	}

	// In a production system, you would now execute these queries against your database,
	// track migration history, and provide a robust CLI for applying/rolling back migrations.
	// Also, comprehensive logging and error handling would be implemented.
	// This code serves as a complete foundation for a generic migration system.

	// Sleep for a moment so the output is visible when running interactively.
	time.Sleep(100 * time.Millisecond)
}

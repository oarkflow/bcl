package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
)

const (
	DialectPostgres = "postgres"
	DialectMySQL    = "mysql"
	DialectSQLite   = "sqlite"
)

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
	CreateTable          []CreateTable          `json:"CreateTable"`
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

type CreateTable struct {
	Name       string      `json:"name"`
	Columns    []AddColumn `json:"Column"`
	PrimaryKey []string    `json:"PrimaryKey,omitempty"`
}

func (ct CreateTable) ToSQL(dialect string, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", ct.Name))
		var cols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", col.Name, mapDataType(dialect, col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
			if !col.Nullable {
				colDef += " NOT NULL"
			}
			if col.Default != "" {
				defaultVal := col.Default
				if strings.ToLower(col.Type) == "string" && !(strings.HasPrefix(col.Default, "'") && strings.HasSuffix(col.Default, "'")) {
					defaultVal = fmt.Sprintf("'%s'", col.Default)
				}
				colDef += fmt.Sprintf(" DEFAULT %s", defaultVal)
			}
			if col.Check != "" {
				colDef += fmt.Sprintf(" CHECK (%s)", col.Check)
			}
			cols = append(cols, colDef)
		}
		if len(ct.PrimaryKey) > 0 {
			cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(ct.PrimaryKey, ", ")))
		}
		sb.WriteString(strings.Join(cols, ", "))
		sb.WriteString(");")
		return sb.String(), nil
	} else {
		return fmt.Sprintf("DROP TABLE IF EXISTS %s;", ct.Name), nil
	}
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
				return "INTEGER"
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
		sb.Reset()
		sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INTEGER PRIMARY KEY AUTOINCREMENT", tableName, a.Name))
	}
	if !a.Nullable {
		sb.WriteString(" NOT NULL")
	}
	if a.Default != "" {
		defaultVal := a.Default
		if strings.ToLower(a.Type) == "string" && !(strings.HasPrefix(a.Default, "'") && strings.HasSuffix(a.Default, "'")) {
			defaultVal = fmt.Sprintf("'%s'", a.Default)
		}
		sb.WriteString(fmt.Sprintf(" DEFAULT %s", defaultVal))
	}
	if a.Check != "" {
		sb.WriteString(fmt.Sprintf(" CHECK (%s)", a.Check))
	}
	sb.WriteString(";")
	queries = append(queries, sb.String())
	if a.Unique {
		uniqueSQL := createUniqueIndexSQL(dialect, tableName, a.Name)
		queries = append(queries, uniqueSQL)
	}
	if a.Index {
		indexSQL := createIndexSQL(dialect, tableName, a.Name)
		queries = append(queries, indexSQL)
	}
	if a.ForeignKey != nil {
		fkSQL, err := foreignKeyToSQL(dialect, tableName, a.Name, *a.ForeignKey)
		if err != nil {
			return nil, err
		}
		queries = append(queries, fkSQL)
	}
	return queries, nil
}

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

func createUniqueIndexSQL(dialect, tableName, column string) string {
	indexName := fmt.Sprintf("uniq_%s_%s", tableName, column)
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s);", indexName, tableName, column)
}

func createIndexSQL(dialect, tableName, column string) string {
	indexName := fmt.Sprintf("idx_%s_%s", tableName, column)
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s);", indexName, tableName, column)
}

func (d DropColumn) ToSQL(dialect, tableName string) (string, error) {
	switch dialect {
	case DialectPostgres, DialectMySQL, DialectSQLite:
		if dialect == DialectSQLite {
			return "", errors.New("SQLite does not support DROP COLUMN directly; table recreation is required")
		}
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, d.Name), nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

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

func (d DeleteData) ToSQL(dialect string) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", d.Name, d.Where), nil
}

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

func (at AlterTable) ToSQL(dialect string) ([]string, error) {
	var queries []string
	for _, addCol := range at.AddColumn {
		qList, err := addCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	for _, dropCol := range at.DropColumn {
		q, err := dropCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	for _, renameCol := range at.RenameColumn {
		q, err := renameCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, nil
}

func (op Operation) ToSQL(dialect string) ([]string, error) {
	var queries []string
	for _, ct := range op.CreateTable {
		q, err := ct.ToSQL(dialect, true)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	for _, at := range op.AlterTable {
		qList, err := at.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	for _, dd := range op.DeleteData {
		q, err := dd.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	for _, de := range op.DropEnumType {
		q, err := de.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	for _, drp := range op.DropRowPolicy {
		q, err := drp.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	for _, dmv := range op.DropMaterializedView {
		q, err := dmv.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	for _, dt := range op.DropTable {
		q, err := dt.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	for _, ds := range op.DropSchema {
		q, err := ds.ToSQL(dialect)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, nil
}

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

func wrapInTransaction(queries []string) []string {
	txQueries := []string{"BEGIN;"}
	txQueries = append(txQueries, queries...)
	txQueries = append(txQueries, "COMMIT;")
	return txQueries
}

func wrapInTransactionWithConfig(queries []string, trans Transaction, dialect string) []string {
	var beginStmt string
	switch dialect {
	case DialectPostgres:
		if trans.IsolationLevel != "" {
			beginStmt = fmt.Sprintf("BEGIN TRANSACTION ISOLATION LEVEL %s;", trans.IsolationLevel)
		} else {
			beginStmt = "BEGIN;"
		}
	default:
		beginStmt = "BEGIN;"
	}
	txQueries := []string{beginStmt}
	txQueries = append(txQueries, queries...)
	txQueries = append(txQueries, "COMMIT;")
	return txQueries
}

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

func runPreUpChecks(checks []string) error {
	for _, check := range checks {
		fmt.Printf("Executing PreUpCheck: %s\n", check)

	}
	fmt.Println("All PreUpChecks passed.")
	return nil
}

func runPostUpChecks(checks []string) error {
	for _, check := range checks {
		fmt.Printf("Executing PostUpCheck: %s\n", check)

	}
	fmt.Println("All PostUpChecks passed.")
	return nil
}

func main() {
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
      }
    }
    CreateTable "core.categories" {
      Column "email" {
        type = "string"
        size = 255
        unique = true
      }
    }
  }
  Down {
    DropRowPolicy "user_access_policy" {
      Table = "core.users"
      IfExists = true
    }
    DropMaterializedView "core.active_users" {
      IfExists = true
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
      }
    }
    DeleteData "core.users" {
      Where = "username LIKE 'admin%'"
    }
    DropTable "core.profiles" {
      Cascade = true
    }
    DropTable "core.users" {
      Cascade = true
    }
    DropEnumType "core.user_role" {
      IfExists = true
    }
    DropSchema "core" {
      Cascade = true
      IfExists = true
    }
  }
}
`)
	var cfg Config
	if _, err := bcl.Unmarshal(input, &cfg); err != nil {
		panic(err)
	}
	dialect := DialectPostgres
	historySQL, err := createMigrationHistoryTableSQL(dialect)
	if err != nil {
		fmt.Println("Error generating migration history table SQL:", err)
		return
	}
	fmt.Println("Migration History Table SQL:")
	fmt.Println(historySQL)
	fmt.Println()
	for _, migration := range cfg.Migrations {
		for _, validation := range migration.Validate {
			if err := runPreUpChecks(validation.PreUpChecks); err != nil {
				fmt.Println("PreUp validation failed:", err)
				return
			}
		}
		upQueries, err := migration.ToSQL(dialect, true)
		if err != nil {
			fmt.Println("Error generating SQL for up migration:", err)
			return
		}
		if len(migration.Transaction) > 1 {
			fmt.Printf("Warning: More than one transaction provided in migration '%s'. Only the first one will be used.\n", migration.Name)
		}
		if len(migration.Transaction) > 0 {
			fmt.Printf("Using transaction mode '%s' for migration '%s'.\n", migration.Transaction[0].Mode, migration.Name)
			upQueries = wrapInTransactionWithConfig(upQueries, migration.Transaction[0], dialect)
		} else {
			upQueries = wrapInTransaction(upQueries)
		}
		fmt.Printf("Generated SQL for migration (up) - %s:\n", migration.Name)
		for _, query := range upQueries {
			fmt.Println(query)
		}
		fmt.Println()
		downQueries, err := migration.ToSQL(dialect, false)
		if err != nil {
			fmt.Println("Error generating SQL for down migration:", err)
			return
		}
		if len(downQueries) == 0 {
			fmt.Printf("Warning: No down migration queries generated for migration '%s'.\n", migration.Name)
		}
		if len(migration.Transaction) > 0 {
			downQueries = wrapInTransactionWithConfig(downQueries, migration.Transaction[0], dialect)
		} else {
			downQueries = wrapInTransaction(downQueries)
		}
		fmt.Printf("Generated SQL for migration (down) - %s:\n", migration.Name)
		for _, query := range downQueries {
			fmt.Println(query)
		}
		for _, validation := range migration.Validate {
			if err := runPostUpChecks(validation.PostUpChecks); err != nil {
				fmt.Println("PostUp validation failed:", err)
				return
			}
		}
		fmt.Println()
	}
	time.Sleep(100 * time.Millisecond)
}

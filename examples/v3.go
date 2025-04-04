package main

import (
	"errors"
	"fmt"
	"strings"

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
	Name         string         `json:"name"`
	AlterTable   []AlterTable   `json:"AlterTable"`
	DeleteData   []DeleteData   `json:"DeleteData"`
	DropEnumType []DropEnumType `json:"DropEnumType"`
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
}

type DeleteData struct {
	Name  string `json:"name"`
	Where string `json:"Where"`
}

type DropEnumType struct {
	Name     string `json:"name"`
	IfExists bool   `json:"IfExists"`
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
			if autoIncrement {
				return "INT"
			}
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

func (a AddColumn) ToSQL(dialect, tableName string) (string, []string, error) {
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
		sb.WriteString(fmt.Sprintf(" DEFAULT %s", a.Default))
	}
	if a.Check != "" {
		sb.WriteString(fmt.Sprintf(" CHECK (%s)", a.Check))
	}
	sb.WriteString(";")
	mainSQL := sb.String()
	queries = append(queries, mainSQL)
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
			return "", nil, err
		}
		queries = append(queries, fkSQL)
	}
	return "", queries, nil
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
	switch dialect {
	case DialectPostgres, DialectMySQL, DialectSQLite:
		return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s);", indexName, tableName, column)
	default:
		return ""
	}
}

func createIndexSQL(dialect, tableName, column string) string {
	indexName := fmt.Sprintf("idx_%s_%s", tableName, column)
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s);", indexName, tableName, column)
}

func (d DropColumn) ToSQL(dialect, tableName string) (string, error) {
	switch dialect {
	case DialectPostgres, DialectMySQL:
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, d.Name), nil
	case DialectSQLite:
		return "", errors.New("SQLite does not support DROP COLUMN directly; table recreation is required")
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

func (r RenameColumn) ToSQL(dialect, tableName string) (string, error) {
	switch dialect {
	case DialectPostgres:
		return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", tableName, r.From, r.To), nil
	case DialectMySQL:
		return fmt.Sprintf("ALTER TABLE %s CHANGE %s %s <COLUMN_TYPE>;", tableName, r.From, r.To), nil
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

func (at AlterTable) ToSQL(dialect string) ([]string, error) {
	var queries []string
	for _, addCol := range at.AddColumn {
		_, addQueries, err := addCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, addQueries...)
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
	_, err := bcl.Unmarshal(input, &cfg)
	if err != nil {
		panic(err)
	}
	dialect := DialectPostgres
	for _, migration := range cfg.Migrations {
		sqlQueries, err := migration.ToSQL(dialect, true)
		if err != nil {
			fmt.Println("Error generating SQL for up migration:", err)
			return
		}
		fmt.Println("Generated SQL for migration (up):")
		for _, query := range sqlQueries {
			fmt.Println(query)
		}
		downQueries, err := migration.ToSQL(dialect, false)
		if err != nil {
			fmt.Println("Error generating SQL for down migration:", err)
			return
		}
		fmt.Println("\nGenerated SQL for migration (down):")
		for _, query := range downQueries {
			fmt.Println(query)
		}
	}

}

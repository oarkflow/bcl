package migration

import (
	"errors"
	"fmt"
	"strings"
)

// Extend the Dialect interface.
type Dialect interface {
	CreateTableSQL(ct CreateTable, up bool) (string, error)
	RenameTableSQL(rt RenameTable) (string, error)
	DeleteDataSQL(dd DeleteData) (string, error)
	DropEnumTypeSQL(de DropEnumType) (string, error)
	DropRowPolicySQL(drp DropRowPolicy) (string, error)
	DropMaterializedViewSQL(dmv DropMaterializedView) (string, error)
	DropTableSQL(dt DropTable) (string, error)
	DropSchemaSQL(ds DropSchema) (string, error)
	AddColumnSQL(ac AddColumn, tableName string) ([]string, error)
	DropColumnSQL(dc DropColumn, tableName string) (string, error)
	RenameColumnSQL(rc RenameColumn, tableName string) (string, error)
	MapDataType(genericType string, size int, autoIncrement, primaryKey bool) string

	// New transaction wrappers.
	WrapInTransaction(queries []string) []string
	WrapInTransactionWithConfig(queries []string, trans Transaction) []string
}

// ---------------------
// Postgres Implementation
// ---------------------
type PostgresDialect struct{}

func (p *PostgresDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", ct.Name))
		var cols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", col.Name, p.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
			if !col.Nullable {
				colDef += " NOT NULL"
			}
			if col.Default != "" {
				def := col.Default
				if strings.ToLower(col.Type) == "string" && !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
					def = fmt.Sprintf("'%s'", def)
				}
				colDef += fmt.Sprintf(" DEFAULT %s", def)
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
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", ct.Name), nil
}

func (p *PostgresDialect) RenameTableSQL(rt RenameTable) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", rt.OldName, rt.NewName), nil
}

func (p *PostgresDialect) DeleteDataSQL(dd DeleteData) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", dd.Name, dd.Where), nil
}

func (p *PostgresDialect) DropEnumTypeSQL(de DropEnumType) (string, error) {
	if de.IfExists {
		return fmt.Sprintf("DROP TYPE IF EXISTS %s;", de.Name), nil
	}
	return fmt.Sprintf("DROP TYPE %s;", de.Name), nil
}

func (p *PostgresDialect) DropRowPolicySQL(drp DropRowPolicy) (string, error) {
	if drp.IfExists {
		return fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s;", drp.Name, drp.Table), nil
	}
	return fmt.Sprintf("DROP POLICY %s ON %s;", drp.Name, drp.Table), nil
}

func (p *PostgresDialect) DropMaterializedViewSQL(dmv DropMaterializedView) (string, error) {
	if dmv.IfExists {
		return fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s;", dmv.Name), nil
	}
	return fmt.Sprintf("DROP MATERIALIZED VIEW %s;", dmv.Name), nil
}

func (p *PostgresDialect) DropTableSQL(dt DropTable) (string, error) {
	cascade := ""
	if dt.Cascade {
		cascade = " CASCADE"
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s%s;", dt.Name, cascade), nil
}

func (p *PostgresDialect) DropSchemaSQL(ds DropSchema) (string, error) {
	exists := ""
	if ds.IfExists {
		exists = " IF EXISTS"
	}
	cascade := ""
	if ds.Cascade {
		cascade = " CASCADE"
	}
	return fmt.Sprintf("DROP SCHEMA%s %s%s;", exists, ds.Name, cascade), nil
}

func (p *PostgresDialect) AddColumnSQL(ac AddColumn, tableName string) ([]string, error) {
	var queries []string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", tableName, ac.Name))
	sb.WriteString(p.MapDataType(ac.Type, ac.Size, ac.AutoIncrement, ac.PrimaryKey))
	if !ac.Nullable {
		sb.WriteString(" NOT NULL")
	}
	if ac.Default != "" {
		def := ac.Default
		if strings.ToLower(ac.Type) == "string" && !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
			def = fmt.Sprintf("'%s'", def)
		}
		sb.WriteString(fmt.Sprintf(" DEFAULT %s", def))
	}
	if ac.Check != "" {
		sb.WriteString(fmt.Sprintf(" CHECK (%s)", ac.Check))
	}
	sb.WriteString(";")
	queries = append(queries, sb.String())
	if ac.Unique {
		queries = append(queries, fmt.Sprintf("CREATE UNIQUE INDEX uniq_%s_%s ON %s (%s);", tableName, ac.Name, tableName, ac.Name))
	}
	if ac.Index {
		queries = append(queries, fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s);", tableName, ac.Name, tableName, ac.Name))
	}
	if ac.ForeignKey != nil {
		fk := ac.ForeignKey
		sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT fk_%s FOREIGN KEY (%s) REFERENCES %s(%s)", tableName, ac.Name, ac.Name, fk.ReferenceTable, fk.ReferenceColumn)
		if fk.OnDelete != "" {
			sql += fmt.Sprintf(" ON DELETE %s", fk.OnDelete)
		}
		if fk.OnUpdate != "" {
			sql += fmt.Sprintf(" ON UPDATE %s", fk.OnUpdate)
		}
		queries = append(queries, sql+";")
	}
	return queries, nil
}

func (p *PostgresDialect) DropColumnSQL(dc DropColumn, tableName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, dc.Name), nil
}

func (p *PostgresDialect) RenameColumnSQL(rc RenameColumn, tableName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", tableName, rc.From, rc.To), nil
}

func (p *PostgresDialect) MapDataType(genericType string, size int, autoIncrement, primaryKey bool) string {
	lt := strings.ToLower(genericType)
	switch lt {
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
}

// Transaction wrappers for Postgres.
func (p *PostgresDialect) WrapInTransaction(queries []string) []string {
	tx := []string{"BEGIN;"}
	tx = append(tx, queries...)
	tx = append(tx, "COMMIT;")
	return tx
}

func (p *PostgresDialect) WrapInTransactionWithConfig(queries []string, trans Transaction) []string {
	var beginStmt string
	if trans.IsolationLevel != "" {
		beginStmt = fmt.Sprintf("BEGIN TRANSACTION ISOLATION LEVEL %s;", trans.IsolationLevel)
	} else {
		beginStmt = "BEGIN;"
	}
	tx := []string{beginStmt}
	tx = append(tx, queries...)
	tx = append(tx, "COMMIT;")
	return tx
}

// ---------------------
// MySQL Implementation
// ---------------------
type MySQLDialect struct{}

func (m *MySQLDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", ct.Name))
		var cols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", col.Name, m.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
			if !col.Nullable {
				colDef += " NOT NULL"
			}
			if col.Default != "" {
				def := col.Default
				if strings.ToLower(col.Type) == "string" && !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
					def = fmt.Sprintf("'%s'", def)
				}
				colDef += fmt.Sprintf(" DEFAULT %s", def)
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
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", ct.Name), nil
}

func (m *MySQLDialect) RenameTableSQL(rt RenameTable) (string, error) {
	return fmt.Sprintf("RENAME TABLE %s TO %s;", rt.OldName, rt.NewName), nil
}

func (m *MySQLDialect) DeleteDataSQL(dd DeleteData) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", dd.Name, dd.Where), nil
}

func (m *MySQLDialect) DropEnumTypeSQL(de DropEnumType) (string, error) {
	return "", errors.New("enum types are not supported in MySQL")
}

func (m *MySQLDialect) DropRowPolicySQL(drp DropRowPolicy) (string, error) {
	return "", errors.New("DROP ROW POLICY is not supported in MySQL")
}

func (m *MySQLDialect) DropMaterializedViewSQL(dmv DropMaterializedView) (string, error) {
	return "", errors.New("DROP MATERIALIZED VIEW is not supported in MySQL")
}

func (m *MySQLDialect) DropTableSQL(dt DropTable) (string, error) {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", dt.Name), nil
}

func (m *MySQLDialect) DropSchemaSQL(ds DropSchema) (string, error) {
	return "", errors.New("DROP SCHEMA is not supported in MySQL")
}

func (m *MySQLDialect) AddColumnSQL(ac AddColumn, tableName string) ([]string, error) {
	var queries []string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", tableName, ac.Name))
	sb.WriteString(m.MapDataType(ac.Type, ac.Size, ac.AutoIncrement, ac.PrimaryKey))
	if ac.AutoIncrement {
		sb.WriteString(" AUTO_INCREMENT")
	}
	if !ac.Nullable {
		sb.WriteString(" NOT NULL")
	}
	if ac.Default != "" {
		def := ac.Default
		if strings.ToLower(ac.Type) == "string" && !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
			def = fmt.Sprintf("'%s'", def)
		}
		sb.WriteString(fmt.Sprintf(" DEFAULT %s", def))
	}
	if ac.Check != "" {
		sb.WriteString(fmt.Sprintf(" CHECK (%s)", ac.Check))
	}
	sb.WriteString(";")
	queries = append(queries, sb.String())
	if ac.Unique {
		queries = append(queries, fmt.Sprintf("CREATE UNIQUE INDEX uniq_%s_%s ON %s (%s);", tableName, ac.Name, tableName, ac.Name))
	}
	if ac.Index {
		queries = append(queries, fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s);", tableName, ac.Name, tableName, ac.Name))
	}
	if ac.ForeignKey != nil {
		fk := ac.ForeignKey
		sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT fk_%s FOREIGN KEY (%s) REFERENCES %s(%s)", tableName, ac.Name, ac.Name, fk.ReferenceTable, fk.ReferenceColumn)
		if fk.OnDelete != "" {
			sql += fmt.Sprintf(" ON DELETE %s", fk.OnDelete)
		}
		if fk.OnUpdate != "" {
			sql += fmt.Sprintf(" ON UPDATE %s", fk.OnUpdate)
		}
		queries = append(queries, sql+";")
	}
	return queries, nil
}

func (m *MySQLDialect) DropColumnSQL(dc DropColumn, tableName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, dc.Name), nil
}

func (m *MySQLDialect) RenameColumnSQL(rc RenameColumn, tableName string) (string, error) {
	if rc.Type == "" {
		return "", errors.New("MySQL requires column type for renaming column")
	}
	return fmt.Sprintf("ALTER TABLE %s CHANGE %s %s %s;", tableName, rc.From, rc.To, rc.Type), nil
}

func (m *MySQLDialect) MapDataType(genericType string, size int, autoIncrement, primaryKey bool) string {
	lt := strings.ToLower(genericType)
	switch lt {
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
}

func (m *MySQLDialect) WrapInTransaction(queries []string) []string {
	tx := []string{"START TRANSACTION;"}
	tx = append(tx, queries...)
	tx = append(tx, "COMMIT;")
	return tx
}

func (m *MySQLDialect) WrapInTransactionWithConfig(queries []string, trans Transaction) []string {
	var beginStmt string
	if trans.IsolationLevel != "" {
		beginStmt = fmt.Sprintf("SET TRANSACTION ISOLATION LEVEL %s; START TRANSACTION;", trans.IsolationLevel)
	} else {
		beginStmt = "START TRANSACTION;"
	}
	tx := []string{beginStmt}
	tx = append(tx, queries...)
	tx = append(tx, "COMMIT;")
	return tx
}

// ---------------------
// SQLite Implementation
// ---------------------
type SQLiteDialect struct{}

func (s *SQLiteDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", ct.Name))
		var cols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", col.Name, s.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
			if !col.Nullable {
				colDef += " NOT NULL"
			}
			if col.Default != "" {
				def := col.Default
				if strings.ToLower(col.Type) == "string" && !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
					def = fmt.Sprintf("'%s'", def)
				}
				colDef += fmt.Sprintf(" DEFAULT %s", def)
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
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", ct.Name), nil
}

func (s *SQLiteDialect) RenameTableSQL(rt RenameTable) (string, error) {
	// SQLite uses ALTER TABLE ... RENAME TO ...
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", rt.OldName, rt.NewName), nil
}

func (s *SQLiteDialect) DeleteDataSQL(dd DeleteData) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", dd.Name, dd.Where), nil
}

func (s *SQLiteDialect) DropEnumTypeSQL(de DropEnumType) (string, error) {
	return "", errors.New("enum types are not supported in SQLite")
}

func (s *SQLiteDialect) DropRowPolicySQL(drp DropRowPolicy) (string, error) {
	return "", errors.New("DROP ROW POLICY is not supported in SQLite")
}

func (s *SQLiteDialect) DropMaterializedViewSQL(dmv DropMaterializedView) (string, error) {
	return "", errors.New("DROP MATERIALIZED VIEW is not supported in SQLite")
}

func (s *SQLiteDialect) DropTableSQL(dt DropTable) (string, error) {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", dt.Name), nil
}

func (s *SQLiteDialect) DropSchemaSQL(ds DropSchema) (string, error) {
	return "", errors.New("DROP SCHEMA is not supported in SQLite")
}

func (s *SQLiteDialect) AddColumnSQL(ac AddColumn, tableName string) ([]string, error) {
	// For SQLite, ADD COLUMN is more limited; foreign keys must be defined at table creation.
	var queries []string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", tableName, ac.Name))
	sb.WriteString(s.MapDataType(ac.Type, ac.Size, ac.AutoIncrement, ac.PrimaryKey))
	if !ac.Nullable {
		sb.WriteString(" NOT NULL")
	}
	if ac.Default != "" {
		def := ac.Default
		if strings.ToLower(ac.Type) == "string" && !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
			def = fmt.Sprintf("'%s'", def)
		}
		sb.WriteString(fmt.Sprintf(" DEFAULT %s", def))
	}
	if ac.Check != "" {
		sb.WriteString(fmt.Sprintf(" CHECK (%s)", ac.Check))
	}
	sb.WriteString(";")
	queries = append(queries, sb.String())
	if ac.Unique {
		queries = append(queries, fmt.Sprintf("CREATE UNIQUE INDEX uniq_%s_%s ON %s (%s);", tableName, ac.Name, tableName, ac.Name))
	}
	if ac.Index {
		queries = append(queries, fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s);", tableName, ac.Name, tableName, ac.Name))
	}
	if ac.ForeignKey != nil {
		return nil, errors.New("SQLite foreign keys must be defined at table creation")
	}
	return queries, nil
}

func (s *SQLiteDialect) DropColumnSQL(dc DropColumn, tableName string) (string, error) {
	return "", errors.New("SQLite DROP COLUMN must use table recreation")
}

func (s *SQLiteDialect) RenameColumnSQL(rc RenameColumn, tableName string) (string, error) {
	return "", errors.New("SQLite RENAME COLUMN must use table recreation")
}

func (s *SQLiteDialect) MapDataType(genericType string, size int, autoIncrement, primaryKey bool) string {
	lt := strings.ToLower(genericType)
	switch lt {
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
}

func (s *SQLiteDialect) WrapInTransaction(queries []string) []string {
	// SQLite uses BEGIN and COMMIT similarly.
	tx := []string{"BEGIN;"}
	tx = append(tx, queries...)
	tx = append(tx, "COMMIT;")
	return tx
}

func (s *SQLiteDialect) WrapInTransactionWithConfig(queries []string, trans Transaction) []string {
	// SQLite does not support isolation level configuration.
	return s.WrapInTransaction(queries)
}

// Move the table recreation logic into SQLiteDialect.
func (s *SQLiteDialect) RecreateTableForAlter(tableName string, newSchema CreateTable, renameMap map[string]string) ([]string, error) {
	var newCols, selectCols []string
	for _, col := range newSchema.Columns {
		newCols = append(newCols, col.Name)
		orig := col.Name
		for old, newName := range renameMap {
			if newName == col.Name {
				orig = old
				break
			}
		}
		selectCols = append(selectCols, orig)
	}
	queries := []string{
		"PRAGMA foreign_keys=off;",
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s_backup;", tableName, tableName),
	}
	ctSQL, err := newSchema.ToSQL(DialectSQLite, true)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new schema for table %s: %w", tableName, err)
	}
	queries = append(queries, ctSQL)
	queries = append(queries, fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s_backup;", tableName, strings.Join(newCols, ", "), strings.Join(selectCols, ", "), tableName))
	queries = append(queries, fmt.Sprintf("DROP TABLE %s_backup;", tableName))
	queries = append(queries, "PRAGMA foreign_keys=on;")
	return queries, nil
}

// ---------------------
// Registry & Helper
// ---------------------
var dialectRegistry = map[string]Dialect{}

func init() {
	dialectRegistry[DialectPostgres] = &PostgresDialect{}
	dialectRegistry[DialectMySQL] = &MySQLDialect{}
	dialectRegistry[DialectSQLite] = &SQLiteDialect{}
}

func getDialect(name string) Dialect {
	if d, ok := dialectRegistry[name]; ok {
		return d
	}
	// Fallback: return PostgresDialect.
	return dialectRegistry[DialectPostgres]
}

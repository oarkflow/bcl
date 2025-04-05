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

	// New functions for views, functions, procedures, and triggers.
	CreateViewSQL(cv CreateView) (string, error)
	DropViewSQL(dv DropView) (string, error)
	RenameViewSQL(rv RenameView) (string, error)

	CreateFunctionSQL(cf CreateFunction) (string, error)
	DropFunctionSQL(df DropFunction) (string, error)
	RenameFunctionSQL(rf RenameFunction) (string, error)

	CreateProcedureSQL(cp CreateProcedure) (string, error)
	DropProcedureSQL(dp DropProcedure) (string, error)
	RenameProcedureSQL(rp RenameProcedure) (string, error)

	CreateTriggerSQL(ct CreateTrigger) (string, error)
	DropTriggerSQL(dt DropTrigger) (string, error)
	RenameTriggerSQL(rt RenameTrigger) (string, error)

	// New transaction wrappers.
	WrapInTransaction(queries []string) []string
	WrapInTransactionWithConfig(queries []string, trans Transaction) []string
}

// ---------------------
// Postgres Implementation
// ---------------------
type PostgresDialect struct{}

func (p *PostgresDialect) quoteIdentifier(id string) string {
	return fmt.Sprintf("\"%s\"", id)
}

func (p *PostgresDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", p.quoteIdentifier(ct.Name)))
		var cols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", p.quoteIdentifier(col.Name), p.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
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
			var pkCols []string
			for _, col := range ct.PrimaryKey {
				pkCols = append(pkCols, p.quoteIdentifier(col))
			}
			cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
		}
		sb.WriteString(strings.Join(cols, ", "))
		sb.WriteString(");")
		return sb.String(), nil
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", p.quoteIdentifier(ct.Name)), nil
}

func (p *PostgresDialect) RenameTableSQL(rt RenameTable) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", p.quoteIdentifier(rt.OldName), p.quoteIdentifier(rt.NewName)), nil
}

func (p *PostgresDialect) DeleteDataSQL(dd DeleteData) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", p.quoteIdentifier(dd.Name), dd.Where), nil
}

func (p *PostgresDialect) DropEnumTypeSQL(de DropEnumType) (string, error) {
	if de.IfExists {
		return fmt.Sprintf("DROP TYPE IF EXISTS %s;", p.quoteIdentifier(de.Name)), nil
	}
	return fmt.Sprintf("DROP TYPE %s;", p.quoteIdentifier(de.Name)), nil
}

func (p *PostgresDialect) DropRowPolicySQL(drp DropRowPolicy) (string, error) {
	if drp.IfExists {
		return fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s;", drp.Name, p.quoteIdentifier(drp.Table)), nil
	}
	return fmt.Sprintf("DROP POLICY %s ON %s;", drp.Name, p.quoteIdentifier(drp.Table)), nil
}

func (p *PostgresDialect) DropMaterializedViewSQL(dmv DropMaterializedView) (string, error) {
	if dmv.IfExists {
		return fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s;", p.quoteIdentifier(dmv.Name)), nil
	}
	return fmt.Sprintf("DROP MATERIALIZED VIEW %s;", p.quoteIdentifier(dmv.Name)), nil
}

func (p *PostgresDialect) DropTableSQL(dt DropTable) (string, error) {
	cascade := ""
	if dt.Cascade {
		cascade = " CASCADE"
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s%s;", p.quoteIdentifier(dt.Name), cascade), nil
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
	return fmt.Sprintf("DROP SCHEMA%s %s%s;", exists, p.quoteIdentifier(ds.Name), cascade), nil
}

func (p *PostgresDialect) AddColumnSQL(ac AddColumn, tableName string) ([]string, error) {
	var queries []string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", p.quoteIdentifier(tableName), p.quoteIdentifier(ac.Name)))
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
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", p.quoteIdentifier(tableName), p.quoteIdentifier(dc.Name)), nil
}

func (p *PostgresDialect) RenameColumnSQL(rc RenameColumn, tableName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", p.quoteIdentifier(tableName), p.quoteIdentifier(rc.From), p.quoteIdentifier(rc.To)), nil
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

// New implementations for views.
func (p *PostgresDialect) CreateViewSQL(cv CreateView) (string, error) {
	if cv.OrReplace {
		return fmt.Sprintf("CREATE OR REPLACE VIEW %s AS %s;", p.quoteIdentifier(cv.Name), cv.Definition), nil
	}
	return fmt.Sprintf("CREATE VIEW %s AS %s;", p.quoteIdentifier(cv.Name), cv.Definition), nil
}

func (p *PostgresDialect) DropViewSQL(dv DropView) (string, error) {
	cascade := ""
	if dv.Cascade {
		cascade = " CASCADE"
	}
	if dv.IfExists {
		return fmt.Sprintf("DROP VIEW IF EXISTS %s%s;", p.quoteIdentifier(dv.Name), cascade), nil
	}
	return fmt.Sprintf("DROP VIEW %s%s;", p.quoteIdentifier(dv.Name), cascade), nil
}

func (p *PostgresDialect) RenameViewSQL(rv RenameView) (string, error) {
	return fmt.Sprintf("ALTER VIEW %s RENAME TO %s;", p.quoteIdentifier(rv.OldName), p.quoteIdentifier(rv.NewName)), nil
}

// New implementations for functions.
func (p *PostgresDialect) CreateFunctionSQL(cf CreateFunction) (string, error) {
	if cf.OrReplace {
		return fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS %s;", p.quoteIdentifier(cf.Name), cf.Definition), nil
	}
	return fmt.Sprintf("CREATE FUNCTION %s AS %s;", p.quoteIdentifier(cf.Name), cf.Definition), nil
}

func (p *PostgresDialect) DropFunctionSQL(df DropFunction) (string, error) {
	cascade := ""
	if df.Cascade {
		cascade = " CASCADE"
	}
	if df.IfExists {
		return fmt.Sprintf("DROP FUNCTION IF EXISTS %s%s;", p.quoteIdentifier(df.Name), cascade), nil
	}
	return fmt.Sprintf("DROP FUNCTION %s%s;", p.quoteIdentifier(df.Name), cascade), nil
}

func (p *PostgresDialect) RenameFunctionSQL(rf RenameFunction) (string, error) {
	return fmt.Sprintf("ALTER FUNCTION %s RENAME TO %s;", p.quoteIdentifier(rf.OldName), p.quoteIdentifier(rf.NewName)), nil
}

// New implementations for procedures.
func (p *PostgresDialect) CreateProcedureSQL(cp CreateProcedure) (string, error) {
	if cp.OrReplace {
		return fmt.Sprintf("CREATE OR REPLACE PROCEDURE %s AS %s;", p.quoteIdentifier(cp.Name), cp.Definition), nil
	}
	return fmt.Sprintf("CREATE PROCEDURE %s AS %s;", p.quoteIdentifier(cp.Name), cp.Definition), nil
}

func (p *PostgresDialect) DropProcedureSQL(dp DropProcedure) (string, error) {
	cascade := ""
	if dp.Cascade {
		cascade = " CASCADE"
	}
	if dp.IfExists {
		return fmt.Sprintf("DROP PROCEDURE IF EXISTS %s%s;", p.quoteIdentifier(dp.Name), cascade), nil
	}
	return fmt.Sprintf("DROP PROCEDURE %s%s;", p.quoteIdentifier(dp.Name), cascade), nil
}

func (p *PostgresDialect) RenameProcedureSQL(rp RenameProcedure) (string, error) {
	return fmt.Sprintf("ALTER PROCEDURE %s RENAME TO %s;", p.quoteIdentifier(rp.OldName), p.quoteIdentifier(rp.NewName)), nil
}

// New implementations for triggers.
func (p *PostgresDialect) CreateTriggerSQL(ct CreateTrigger) (string, error) {
	if ct.OrReplace {
		return fmt.Sprintf("CREATE OR REPLACE TRIGGER %s %s;", p.quoteIdentifier(ct.Name), ct.Definition), nil
	}
	return fmt.Sprintf("CREATE TRIGGER %s %s;", p.quoteIdentifier(ct.Name), ct.Definition), nil
}

func (p *PostgresDialect) DropTriggerSQL(dt DropTrigger) (string, error) {
	cascade := ""
	if dt.Cascade {
		cascade = " CASCADE"
	}
	if dt.IfExists {
		return fmt.Sprintf("DROP TRIGGER IF EXISTS %s%s;", p.quoteIdentifier(dt.Name), cascade), nil
	}
	return fmt.Sprintf("DROP TRIGGER %s%s;", p.quoteIdentifier(dt.Name), cascade), nil
}

func (p *PostgresDialect) RenameTriggerSQL(rt RenameTrigger) (string, error) {
	return fmt.Sprintf("ALTER TRIGGER %s RENAME TO %s;", p.quoteIdentifier(rt.OldName), p.quoteIdentifier(rt.NewName)), nil
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

func (m *MySQLDialect) quoteIdentifier(id string) string {
	return fmt.Sprintf("`%s`", id)
}

func (m *MySQLDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", m.quoteIdentifier(ct.Name)))
		var cols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", m.quoteIdentifier(col.Name), m.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
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
			var pkCols []string
			for _, col := range ct.PrimaryKey {
				pkCols = append(pkCols, m.quoteIdentifier(col))
			}
			cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
		}
		sb.WriteString(strings.Join(cols, ", "))
		sb.WriteString(");")
		return sb.String(), nil
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", m.quoteIdentifier(ct.Name)), nil
}

func (m *MySQLDialect) RenameTableSQL(rt RenameTable) (string, error) {
	return fmt.Sprintf("RENAME TABLE %s TO %s;", m.quoteIdentifier(rt.OldName), m.quoteIdentifier(rt.NewName)), nil
}

func (m *MySQLDialect) DeleteDataSQL(dd DeleteData) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", m.quoteIdentifier(dd.Name), dd.Where), nil
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
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", m.quoteIdentifier(dt.Name)), nil
}

func (m *MySQLDialect) DropSchemaSQL(ds DropSchema) (string, error) {
	return "", errors.New("DROP SCHEMA is not supported in MySQL")
}

func (m *MySQLDialect) AddColumnSQL(ac AddColumn, tableName string) ([]string, error) {
	var queries []string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", m.quoteIdentifier(tableName), m.quoteIdentifier(ac.Name)))
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
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", m.quoteIdentifier(tableName), m.quoteIdentifier(dc.Name)), nil
}

func (m *MySQLDialect) RenameColumnSQL(rc RenameColumn, tableName string) (string, error) {
	if rc.Type == "" {
		return "", errors.New("MySQL requires column type for renaming column")
	}
	return fmt.Sprintf("ALTER TABLE %s CHANGE %s %s %s;", m.quoteIdentifier(tableName), m.quoteIdentifier(rc.From), m.quoteIdentifier(rc.To), rc.Type), nil
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

// New implementations for views.
func (m *MySQLDialect) CreateViewSQL(cv CreateView) (string, error) {
	if cv.OrReplace {
		return fmt.Sprintf("CREATE OR REPLACE VIEW %s AS %s;", m.quoteIdentifier(cv.Name), cv.Definition), nil
	}
	return fmt.Sprintf("CREATE VIEW %s AS %s;", m.quoteIdentifier(cv.Name), cv.Definition), nil
}

func (m *MySQLDialect) DropViewSQL(dv DropView) (string, error) {
	cascade := ""
	if dv.Cascade {
		cascade = " CASCADE"
	}
	if dv.IfExists {
		return fmt.Sprintf("DROP VIEW IF EXISTS %s%s;", m.quoteIdentifier(dv.Name), cascade), nil
	}
	return fmt.Sprintf("DROP VIEW %s%s;", m.quoteIdentifier(dv.Name), cascade), nil
}

func (m *MySQLDialect) RenameViewSQL(rv RenameView) (string, error) {
	return "", errors.New("RENAME VIEW is not supported in MySQL")
}

// New implementations for functions.
func (m *MySQLDialect) CreateFunctionSQL(cf CreateFunction) (string, error) {
	return "", errors.New("CREATE FUNCTION is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) DropFunctionSQL(df DropFunction) (string, error) {
	return "", errors.New("DROP FUNCTION is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) RenameFunctionSQL(rf RenameFunction) (string, error) {
	return "", errors.New("RENAME FUNCTION is not supported in this MySQL dialect implementation")
}

// New implementations for procedures.
func (m *MySQLDialect) CreateProcedureSQL(cp CreateProcedure) (string, error) {
	return "", errors.New("CREATE PROCEDURE is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) DropProcedureSQL(dp DropProcedure) (string, error) {
	return "", errors.New("DROP PROCEDURE is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) RenameProcedureSQL(rp RenameProcedure) (string, error) {
	return "", errors.New("RENAME PROCEDURE is not supported in this MySQL dialect implementation")
}

// New implementations for triggers.
func (m *MySQLDialect) CreateTriggerSQL(ct CreateTrigger) (string, error) {
	return "", errors.New("CREATE TRIGGER is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) DropTriggerSQL(dt DropTrigger) (string, error) {
	return "", errors.New("DROP TRIGGER is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) RenameTriggerSQL(rt RenameTrigger) (string, error) {
	return "", errors.New("RENAME TRIGGER is not supported in this MySQL dialect implementation")
}

// ---------------------
// SQLite Implementation
// ---------------------
type SQLiteDialect struct{}

func (s *SQLiteDialect) quoteIdentifier(id string) string {
	return fmt.Sprintf("\"%s\"", id)
}

func (s *SQLiteDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", s.quoteIdentifier(ct.Name)))
		var cols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", s.quoteIdentifier(col.Name), s.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
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
			var pkCols []string
			for _, col := range ct.PrimaryKey {
				pkCols = append(pkCols, s.quoteIdentifier(col))
			}
			cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
		}
		sb.WriteString(strings.Join(cols, ", "))
		sb.WriteString(");")
		return sb.String(), nil
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", s.quoteIdentifier(ct.Name)), nil
}

func (s *SQLiteDialect) RenameTableSQL(rt RenameTable) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", s.quoteIdentifier(rt.OldName), s.quoteIdentifier(rt.NewName)), nil
}

func (s *SQLiteDialect) DeleteDataSQL(dd DeleteData) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", s.quoteIdentifier(dd.Name), dd.Where), nil
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
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", s.quoteIdentifier(dt.Name)), nil
}

func (s *SQLiteDialect) DropSchemaSQL(ds DropSchema) (string, error) {
	return "", errors.New("DROP SCHEMA is not supported in SQLite")
}

func (s *SQLiteDialect) AddColumnSQL(ac AddColumn, tableName string) ([]string, error) {
	var queries []string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", s.quoteIdentifier(tableName), s.quoteIdentifier(ac.Name)))
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
	tx := []string{"BEGIN;"}
	tx = append(tx, queries...)
	tx = append(tx, "COMMIT;")
	return tx
}

func (s *SQLiteDialect) WrapInTransactionWithConfig(queries []string, trans Transaction) []string {
	return s.WrapInTransaction(queries)
}

// New implementations for views.
func (s *SQLiteDialect) CreateViewSQL(cv CreateView) (string, error) {
	if cv.OrReplace {
		return fmt.Sprintf("CREATE VIEW IF NOT EXISTS %s AS %s;", s.quoteIdentifier(cv.Name), cv.Definition), nil
	}
	return fmt.Sprintf("CREATE VIEW %s AS %s;", s.quoteIdentifier(cv.Name), cv.Definition), nil
}

func (s *SQLiteDialect) DropViewSQL(dv DropView) (string, error) {
	if dv.IfExists {
		return fmt.Sprintf("DROP VIEW IF EXISTS %s;", s.quoteIdentifier(dv.Name)), nil
	}
	return fmt.Sprintf("DROP VIEW %s;", s.quoteIdentifier(dv.Name)), nil
}

func (s *SQLiteDialect) RenameViewSQL(rv RenameView) (string, error) {
	return "", errors.New("RENAME VIEW is not supported in SQLite")
}

// New implementations for functions.
func (s *SQLiteDialect) CreateFunctionSQL(cf CreateFunction) (string, error) {
	return "", errors.New("CREATE FUNCTION is not supported in SQLite")
}

func (s *SQLiteDialect) DropFunctionSQL(df DropFunction) (string, error) {
	return "", errors.New("DROP FUNCTION is not supported in SQLite")
}

func (s *SQLiteDialect) RenameFunctionSQL(rf RenameFunction) (string, error) {
	return "", errors.New("RENAME FUNCTION is not supported in SQLite")
}

// New implementations for procedures.
func (s *SQLiteDialect) CreateProcedureSQL(cp CreateProcedure) (string, error) {
	return "", errors.New("CREATE PROCEDURE is not supported in SQLite")
}

func (s *SQLiteDialect) DropProcedureSQL(dp DropProcedure) (string, error) {
	return "", errors.New("DROP PROCEDURE is not supported in SQLite")
}

func (s *SQLiteDialect) RenameProcedureSQL(rp RenameProcedure) (string, error) {
	return "", errors.New("RENAME PROCEDURE is not supported in SQLite")
}

// New implementations for triggers.
func (s *SQLiteDialect) CreateTriggerSQL(ct CreateTrigger) (string, error) {
	if ct.OrReplace {
		return fmt.Sprintf("DROP TRIGGER IF EXISTS %s; CREATE TRIGGER %s %s;", s.quoteIdentifier(ct.Name), s.quoteIdentifier(ct.Name), ct.Definition), nil
	}
	return fmt.Sprintf("CREATE TRIGGER %s %s;", s.quoteIdentifier(ct.Name), ct.Definition), nil
}

func (s *SQLiteDialect) DropTriggerSQL(dt DropTrigger) (string, error) {
	if dt.IfExists {
		return fmt.Sprintf("DROP TRIGGER IF EXISTS %s;", s.quoteIdentifier(dt.Name)), nil
	}
	return fmt.Sprintf("DROP TRIGGER %s;", s.quoteIdentifier(dt.Name)), nil
}

func (s *SQLiteDialect) RenameTriggerSQL(rt RenameTrigger) (string, error) {
	return "", errors.New("RENAME TRIGGER is not supported in SQLite")
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
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s_backup;", s.quoteIdentifier(tableName), s.quoteIdentifier(tableName)),
	}
	ctSQL, err := newSchema.ToSQL(DialectSQLite, true)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new schema for table %s: %w", tableName, err)
	}
	queries = append(queries, ctSQL)
	queries = append(queries, fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s_backup;", s.quoteIdentifier(tableName), strings.Join(newCols, ", "), strings.Join(selectCols, ", "), s.quoteIdentifier(tableName)))
	queries = append(queries, fmt.Sprintf("DROP TABLE %s_backup;", s.quoteIdentifier(tableName)))
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
	return dialectRegistry[DialectPostgres]
}

package migration

import (
	"errors"
	"fmt"
	"strings"
)

type SQLiteDialect struct{}

func (s *SQLiteDialect) quoteIdentifier(id string) string {
	return fmt.Sprintf("\"%s\"", id)
}

func (s *SQLiteDialect) TableExistsSQL(table string) string {
	return fmt.Sprintf(`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type = 'table' AND name = '%s'`, table)
}

func (s *SQLiteDialect) convertDefault(defVal any, colType string) string {
	var def string
	switch defVal := defVal.(type) {
	case string:
		def = defVal
	case nil:
		def = "NULL"
	default:
		def = fmt.Sprintf("%v", defVal)
	}
	lowerDef := strings.ToLower(def)
	if lowerDef == "now()" {
		return "CURRENT_TIMESTAMP"
	}
	if lowerDef == "null" {
		return "NULL"
	}
	if strings.ToLower(colType) == "string" {
		if !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
			return fmt.Sprintf("'%s'", def)
		}
	}
	return def
}

func (s *SQLiteDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", s.quoteIdentifier(ct.Name)))
		var cols []string
		var pkCols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", s.quoteIdentifier(col.Name), s.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
			if !col.Nullable {
				colDef += " NOT NULL"
			}
			if col.Default != "" {
				def := s.convertDefault(col.Default, col.Type)
				if strings.Contains(colDef, "NOT NULL") {
					if def != "NULL" {
						colDef += fmt.Sprintf(" DEFAULT %s", def)
					}
				} else {
					colDef += fmt.Sprintf(" DEFAULT %s", def)
				}
			}
			if col.Check != "" {
				colDef += fmt.Sprintf(" CHECK (%s)", col.Check)
			}
			cols = append(cols, colDef)
			if len(ct.PrimaryKey) == 0 && col.PrimaryKey {
				pkCols = append(pkCols, s.quoteIdentifier(col.Name))
			}
		}
		if len(ct.PrimaryKey) > 0 {
			var pkQuoted []string
			for _, col := range ct.PrimaryKey {
				pkQuoted = append(pkQuoted, s.quoteIdentifier(col))
			}
			cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkQuoted, ", ")))
		} else if len(pkCols) > 0 {
			cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
		}
		sb.WriteString(strings.Join(cols, ", "))
		sb.WriteString(");")
		var extra []string
		for _, col := range ct.Columns {
			if col.Unique {
				extra = append(extra, fmt.Sprintf("CREATE UNIQUE INDEX uniq_%s_%s ON %s (%s);", ct.Name, col.Name, s.quoteIdentifier(ct.Name), s.quoteIdentifier(col.Name)))
			} else if col.Index {
				extra = append(extra, fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s);", ct.Name, col.Name, s.quoteIdentifier(ct.Name), s.quoteIdentifier(col.Name)))
			}
		}
		if len(extra) > 0 {
			sb.WriteString("\n" + strings.Join(extra, "\n"))
		}
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
		def := s.convertDefault(ac.Default, ac.Type)
		if !ac.Nullable {
			if def != "NULL" {
				sb.WriteString(fmt.Sprintf(" DEFAULT %s", def))
			}
		} else {
			sb.WriteString(fmt.Sprintf(" DEFAULT %s", def))
		}
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
	if dt, ok := sqliteDataTypes[lt]; ok {
		if (lt == "varchar" || lt == "char") && size > 0 {
			return fmt.Sprintf("%s(%d)", dt, size)
		} else if (lt == "decimal" || lt == "numeric") && size > 0 {
			// Assumes a default scale of 2
			return fmt.Sprintf("%s(%d,2)", dt, size)
		}
		return dt
	}
	return genericType
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

func (s *SQLiteDialect) CreateFunctionSQL(cf CreateFunction) (string, error) {
	return "", errors.New("CREATE FUNCTION is not supported in SQLite")
}

func (s *SQLiteDialect) DropFunctionSQL(df DropFunction) (string, error) {
	return "", errors.New("DROP FUNCTION is not supported in SQLite")
}

func (s *SQLiteDialect) RenameFunctionSQL(rf RenameFunction) (string, error) {
	return "", errors.New("RENAME FUNCTION is not supported in SQLite")
}

func (s *SQLiteDialect) CreateProcedureSQL(cp CreateProcedure) (string, error) {
	return "", errors.New("CREATE PROCEDURE is not supported in SQLite")
}

func (s *SQLiteDialect) DropProcedureSQL(dp DropProcedure) (string, error) {
	return "", errors.New("DROP PROCEDURE is not supported in SQLite")
}

func (s *SQLiteDialect) RenameProcedureSQL(rp RenameProcedure) (string, error) {
	return "", errors.New("RENAME PROCEDURE is not supported in SQLite")
}

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

func (s *SQLiteDialect) EOS() string {
	return ";"
}

func (s *SQLiteDialect) InsertSQL(table string, columns []string, values []any) (string, error) {
	quotedCols := []string{}
	for _, col := range columns {
		quotedCols = append(quotedCols, s.quoteIdentifier(col))
	}
	quotedVals := []string{}
	for _, val := range values {
		switch v := val.(type) {
		case string:
			quotedVals = append(quotedVals, fmt.Sprintf("'%s'", v))
		case nil:
			quotedVals = append(quotedVals, "NULL")
		default:
			quotedVals = append(quotedVals, fmt.Sprintf("%v", v))
		}
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		s.quoteIdentifier(table),
		strings.Join(quotedCols, ", "),
		strings.Join(quotedVals, ", "),
	)
	return query, nil
}

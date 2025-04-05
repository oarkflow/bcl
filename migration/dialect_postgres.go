package migration

import (
	"errors"
	"fmt"
	"strings"
)

type PostgresDialect struct{}

func (p *PostgresDialect) quoteIdentifier(id string) string {
	return fmt.Sprintf("\"%s\"", id)
}

func (p *PostgresDialect) TableExistsSQL(table string) string {
	return fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = '%s')`, table)
}

func (p *PostgresDialect) convertDefault(defVal any, colType string) string {
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

func (p *PostgresDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", p.quoteIdentifier(ct.Name)))
		var cols []string
		var pkCols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", p.quoteIdentifier(col.Name), p.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
			if !col.Nullable {
				colDef += " NOT NULL"
			}
			if col.Default != "" {
				def := p.convertDefault(col.Default, col.Type)
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
				pkCols = append(pkCols, p.quoteIdentifier(col.Name))
			}
		}
		if len(ct.PrimaryKey) > 0 {
			var pkQuoted []string
			for _, col := range ct.PrimaryKey {
				pkQuoted = append(pkQuoted, p.quoteIdentifier(col))
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
				extra = append(extra, fmt.Sprintf("CREATE UNIQUE INDEX uniq_%s_%s ON %s (%s);", ct.Name, col.Name, p.quoteIdentifier(ct.Name), p.quoteIdentifier(col.Name)))
			} else if col.Index {
				extra = append(extra, fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s);", ct.Name, col.Name, p.quoteIdentifier(ct.Name), p.quoteIdentifier(col.Name)))
			}
		}
		if len(extra) > 0 {
			sb.WriteString("\n" + strings.Join(extra, "\n"))
		}
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

func (p *PostgresDialect) EOS() string {
	return ";"
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
		def := p.convertDefault(ac.Default, ac.Type)
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
	from := rc.From
	if from == "" && rc.Name != "" {
		from = rc.Name
	}
	if from == "" {
		return "", errors.New("Postgres requires column name for renaming column")
	}
	if rc.To == "" {
		return "", errors.New("Postgres requires new column name for renaming column")
	}
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", p.quoteIdentifier(tableName), p.quoteIdentifier(from), p.quoteIdentifier(rc.To)), nil
}

func (p *PostgresDialect) MapDataType(genericType string, size int, autoIncrement, primaryKey bool) string {
	if autoIncrement {
		return "SERIAL"
	}
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
	case "float":
		return "REAL"
	case "double":
		return "DOUBLE PRECISION"
	default:
		return genericType
	}
}

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

func (p *PostgresDialect) InsertSQL(table string, columns []string, values []any) (string, error) {
	quotedCols := []string{}
	for _, col := range columns {
		quotedCols = append(quotedCols, p.quoteIdentifier(col))
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
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n",
		p.quoteIdentifier(table),
		strings.Join(quotedCols, ", "),
		strings.Join(quotedVals, ", "),
	)
	return query, nil
}

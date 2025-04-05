package migration

import (
	"errors"
	"fmt"
	"strings"
)

type MySQLDialect struct{}

func (m *MySQLDialect) quoteIdentifier(id string) string {
	return fmt.Sprintf("`%s`", id)
}

func (m *MySQLDialect) convertDefault(defVal any, colType string) string {
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

func (m *MySQLDialect) TableExistsSQL(table string) string {
	return fmt.Sprintf(`SELECT COUNT(*) > 0 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = '%s'`, table)
}

func (m *MySQLDialect) CreateTableSQL(ct CreateTable, up bool) (string, error) {
	if up {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TABLE %s (", m.quoteIdentifier(ct.Name)))
		var cols []string
		var pkCols []string
		for _, col := range ct.Columns {
			colDef := fmt.Sprintf("%s %s", m.quoteIdentifier(col.Name), m.MapDataType(col.Type, col.Size, col.AutoIncrement, col.PrimaryKey))
			if col.AutoIncrement {
				colDef += " AUTO_INCREMENT"
			}
			if !col.Nullable {
				colDef += " NOT NULL"
			}
			if col.Default != "" {
				def := m.convertDefault(col.Default, col.Type)
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
				pkCols = append(pkCols, m.quoteIdentifier(col.Name))
			}
		}
		if len(ct.PrimaryKey) > 0 {
			var pkQuoted []string
			for _, col := range ct.PrimaryKey {
				pkQuoted = append(pkQuoted, m.quoteIdentifier(col))
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
				extra = append(extra, fmt.Sprintf("CREATE UNIQUE INDEX uniq_%s_%s ON %s (%s);", ct.Name, col.Name, m.quoteIdentifier(ct.Name), m.quoteIdentifier(col.Name)))
			} else if col.Index {
				extra = append(extra, fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s);", ct.Name, col.Name, m.quoteIdentifier(ct.Name), m.quoteIdentifier(col.Name)))
			}
		}
		if len(extra) > 0 {
			sb.WriteString("\n" + strings.Join(extra, "\n"))
		}
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
		def := m.convertDefault(ac.Default, ac.Type)
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

func (m *MySQLDialect) DropColumnSQL(dc DropColumn, tableName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", m.quoteIdentifier(tableName), m.quoteIdentifier(dc.Name)), nil
}

func (m *MySQLDialect) RenameColumnSQL(rc RenameColumn, tableName string) (string, error) {
	if rc.Type == "" {
		return "", errors.New("MySQL requires column type for renaming column")
	}
	from := rc.From
	if from == "" && rc.Name != "" {
		from = rc.Name
	}
	if from == "" {
		return "", errors.New("MySQL requires column name for renaming column")
	}
	if rc.To == "" {
		return "", errors.New("MySQL requires new column name for renaming column")
	}
	return fmt.Sprintf("ALTER TABLE %s CHANGE %s %s %s;", m.quoteIdentifier(tableName), m.quoteIdentifier(from), m.quoteIdentifier(rc.To), rc.Type), nil
}

func (m *MySQLDialect) MapDataType(genericType string, size int, autoIncrement, primaryKey bool) string {
	lt := strings.ToLower(genericType)
	if dt, ok := mysqlDataTypes[lt]; ok {
		if (lt == "string" || lt == "varchar") && size > 0 {
			return fmt.Sprintf("%s(%d)", dt, size)
		} else if lt == "decimal" && size > 0 {
			// Default scale of 2 is assumed
			return fmt.Sprintf("DECIMAL(%d,2)", size)
		}
		return dt
	}
	return genericType
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

func (m *MySQLDialect) CreateFunctionSQL(cf CreateFunction) (string, error) {
	return "", errors.New("CREATE FUNCTION is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) DropFunctionSQL(df DropFunction) (string, error) {
	return "", errors.New("DROP FUNCTION is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) RenameFunctionSQL(rf RenameFunction) (string, error) {
	return "", errors.New("RENAME FUNCTION is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) CreateProcedureSQL(cp CreateProcedure) (string, error) {
	return "", errors.New("CREATE PROCEDURE is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) DropProcedureSQL(dp DropProcedure) (string, error) {
	return "", errors.New("DROP PROCEDURE is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) RenameProcedureSQL(rp RenameProcedure) (string, error) {
	return "", errors.New("RENAME PROCEDURE is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) CreateTriggerSQL(ct CreateTrigger) (string, error) {
	return "", errors.New("CREATE TRIGGER is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) DropTriggerSQL(dt DropTrigger) (string, error) {
	return "", errors.New("DROP TRIGGER is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) RenameTriggerSQL(rt RenameTrigger) (string, error) {
	return "", errors.New("RENAME TRIGGER is not supported in this MySQL dialect implementation")
}

func (m *MySQLDialect) InsertSQL(table string, columns []string, values []any) (string, error) {
	quotedCols := []string{}
	for _, col := range columns {
		quotedCols = append(quotedCols, m.quoteIdentifier(col))
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
		m.quoteIdentifier(table),
		strings.Join(quotedCols, ", "),
		strings.Join(quotedVals, ", "),
	)
	return query, nil
}
func (m *MySQLDialect) EOS() string {
	return ";"
}

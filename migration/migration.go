package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	DialectPostgres = "postgres"
	DialectMySQL    = "mysql"
	DialectSQLite   = "sqlite"

	lockFileName = "migration.lock"
)

var tableSchemas = make(map[string]*CreateTable)
var schemaMutex sync.Mutex

type Config struct {
	Migrations []Migration `json:"Migration"`
}

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
	RenameTable          []RenameTable          `json:"RenameTable,omitempty"`
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

type RenameTable struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

func (rt RenameTable) ToSQL(dialect string) (string, error) {
	switch dialect {
	case DialectPostgres, DialectSQLite:
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", rt.OldName, rt.NewName), nil
	case DialectMySQL:
		return fmt.Sprintf("RENAME TABLE %s TO %s;", rt.OldName, rt.NewName), nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

type DeleteData struct {
	Name  string `json:"name"`
	Where string `json:"Where"`
}

func (d DeleteData) ToSQL(dialect string) (string, error) {
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", d.Name, d.Where), nil
}

type DropEnumType struct {
	Name     string `json:"name"`
	IfExists bool   `json:"IfExists"`
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

type DropRowPolicy struct {
	Name     string `json:"name"`
	Table    string `json:"Table"`
	IfExists bool   `json:"if_exists,omitempty"`
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

type DropMaterializedView struct {
	Name     string `json:"name"`
	IfExists bool   `json:"if_exists,omitempty"`
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

type DropTable struct {
	Name    string `json:"name"`
	Cascade bool   `json:"cascade,omitempty"`
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

type DropSchema struct {
	Name     string `json:"name"`
	Cascade  bool   `json:"cascade,omitempty"`
	IfExists bool   `json:"if_exists,omitempty"`
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
	if dialect == DialectSQLite {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", tableName, a.Name))
		dataType := mapDataType(dialect, a.Type, a.Size, a.AutoIncrement, a.PrimaryKey)
		sb.WriteString(dataType)
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
	} else {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s ", tableName, a.Name))
		dataType := mapDataType(dialect, a.Type, a.Size, a.AutoIncrement, a.PrimaryKey)
		sb.WriteString(dataType)
		if dialect == DialectMySQL && a.AutoIncrement {
			sb.WriteString(" AUTO_INCREMENT")
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
	}
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
	case DialectPostgres, DialectMySQL:
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
	if dialect == DialectSQLite {
		return "", errors.New("SQLite DROP COLUMN must use table recreation")
	}
	switch dialect {
	case DialectPostgres, DialectMySQL:
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, d.Name), nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

func (r RenameColumn) ToSQL(dialect, tableName string) (string, error) {
	if dialect == DialectSQLite {
		return "", errors.New("SQLite RENAME COLUMN must use table recreation")
	}
	switch dialect {
	case DialectPostgres:
		return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", tableName, r.From, r.To), nil
	case DialectMySQL:
		if r.Type == "" {
			return "", errors.New("MySQL requires column type for renaming column")
		}
		return fmt.Sprintf("ALTER TABLE %s CHANGE %s %s %s;", tableName, r.From, r.To, r.Type), nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

func recreateTableForSQLite(tableName string, newSchema CreateTable, renameMap map[string]string) ([]string, error) {
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

func (at AlterTable) ToSQL(dialect string) ([]string, error) {
	if dialect == DialectSQLite {
		return handleSQLiteAlterTable(at)
	}
	var queries []string
	for _, addCol := range at.AddColumn {
		qList, err := addCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, fmt.Errorf("error in AddColumn: %w", err)
		}
		queries = append(queries, qList...)
	}
	for _, dropCol := range at.DropColumn {
		q, err := dropCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, fmt.Errorf("error in DropColumn: %w", err)
		}
		queries = append(queries, q)
	}
	for _, renameCol := range at.RenameColumn {
		q, err := renameCol.ToSQL(dialect, at.Name)
		if err != nil {
			return nil, fmt.Errorf("error in RenameColumn: %w", err)
		}
		queries = append(queries, q)
	}
	return queries, nil
}

func handleSQLiteAlterTable(at AlterTable) ([]string, error) {
	schemaMutex.Lock()
	defer schemaMutex.Unlock()
	origSchema, ok := tableSchemas[at.Name]
	if !ok {
		return nil, fmt.Errorf("table schema for %s not found; cannot recreate table for alteration", at.Name)
	}
	newSchema := *origSchema
	renameMap := make(map[string]string)
	if len(at.DropColumn) > 0 || len(at.RenameColumn) > 0 {
		for _, dropCol := range at.DropColumn {
			found := false
			newCols := []AddColumn{}
			for _, col := range newSchema.Columns {
				if col.Name == dropCol.Name {
					found = true
					continue
				}
				newCols = append(newCols, col)
			}
			if !found {
				return nil, fmt.Errorf("column %s not found in table %s for dropping", dropCol.Name, at.Name)
			}
			newSchema.Columns = newCols
			newPK := []string{}
			for _, pk := range newSchema.PrimaryKey {
				if pk != dropCol.Name {
					newPK = append(newPK, pk)
				}
			}
			newSchema.PrimaryKey = newPK
		}
		for _, renameCol := range at.RenameColumn {
			found := false
			for i, col := range newSchema.Columns {
				if col.Name == renameCol.From {
					newSchema.Columns[i].Name = renameCol.To
					found = true
					renameMap[renameCol.From] = renameCol.To
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("column %s not found in table %s for renaming", renameCol.From, at.Name)
			}
			for i, pk := range newSchema.PrimaryKey {
				if pk == renameCol.From {
					newSchema.PrimaryKey[i] = renameCol.To
				}
			}
		}
		queries, err := recreateTableForSQLite(at.Name, newSchema, renameMap)
		if err != nil {
			return nil, fmt.Errorf("failed to recreate table for SQLite alteration: %w", err)
		}
		tableSchemas[at.Name] = &newSchema
		return queries, nil
	}
	var queries []string
	for _, addCol := range at.AddColumn {
		qList, err := addCol.ToSQL(DialectSQLite, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
		newSchema.Columns = append(newSchema.Columns, addCol)
		if addCol.PrimaryKey {
			newSchema.PrimaryKey = append(newSchema.PrimaryKey, addCol.Name)
		}
	}
	tableSchemas[at.Name] = &newSchema
	return queries, nil
}

func (op Operation) ToSQL(dialect string) ([]string, error) {
	var queries []string
	for _, ct := range op.CreateTable {
		q, err := ct.ToSQL(dialect, true)
		if err != nil {
			return nil, fmt.Errorf("error in CreateTable: %w", err)
		}
		queries = append(queries, q)
		if dialect == DialectSQLite {
			schemaMutex.Lock()
			cpy := ct
			tableSchemas[ct.Name] = &cpy
			schemaMutex.Unlock()
		}
	}
	for _, at := range op.AlterTable {
		qList, err := at.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in AlterTable: %w", err)
		}
		queries = append(queries, qList...)
	}
	for _, dd := range op.DeleteData {
		q, err := dd.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DeleteData: %w", err)
		}
		queries = append(queries, q)
	}
	for _, de := range op.DropEnumType {
		q, err := de.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropEnumType: %w", err)
		}
		queries = append(queries, q)
	}
	for _, drp := range op.DropRowPolicy {
		q, err := drp.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropRowPolicy: %w", err)
		}
		queries = append(queries, q)
	}
	for _, dmv := range op.DropMaterializedView {
		q, err := dmv.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropMaterializedView: %w", err)
		}
		queries = append(queries, q)
	}
	for _, dt := range op.DropTable {
		q, err := dt.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropTable: %w", err)
		}
		queries = append(queries, q)
	}
	for _, ds := range op.DropSchema {
		q, err := ds.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropSchema: %w", err)
		}
		queries = append(queries, q)
	}
	for _, rt := range op.RenameTable {
		q, err := rt.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in RenameTable: %w", err)
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
			return nil, fmt.Errorf("error in migration operation: %w", err)
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
	case DialectMySQL:
		if trans.IsolationLevel != "" {
			beginStmt = fmt.Sprintf("SET TRANSACTION ISOLATION LEVEL %s; START TRANSACTION;", trans.IsolationLevel)
		} else {
			beginStmt = "START TRANSACTION;"
		}
	default:
		beginStmt = "BEGIN;"
	}
	txQueries := []string{beginStmt}
	txQueries = append(txQueries, queries...)
	txQueries = append(txQueries, "COMMIT;")
	return txQueries
}

func computeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	AlterTable           []AlterTable           `json:"AlterTable,omitempty"`
	CreateTable          []CreateTable          `json:"CreateTable,omitempty"`
	DeleteData           []DeleteData           `json:"DeleteData,omitempty"`
	DropEnumType         []DropEnumType         `json:"DropEnumType,omitempty"`
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
	return getDialect(dialect).CreateTableSQL(ct, up)
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

func (d DropColumn) ToSQL(dialect, tableName string) (string, error) {
	return getDialect(dialect).DropColumnSQL(d, tableName)
}

type RenameColumn struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type,omitempty"`
}

func (r RenameColumn) ToSQL(dialect, tableName string) (string, error) {
	return getDialect(dialect).RenameColumnSQL(r, tableName)
}

type RenameTable struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

func (rt RenameTable) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).RenameTableSQL(rt)
}

type DeleteData struct {
	Name  string `json:"name"`
	Where string `json:"Where"`
}

func (d DeleteData) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DeleteDataSQL(d)
}

type DropEnumType struct {
	Name     string `json:"name"`
	IfExists bool   `json:"IfExists"`
}

func (d DropEnumType) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropEnumTypeSQL(d)
}

type DropRowPolicy struct {
	Name     string `json:"name"`
	Table    string `json:"Table"`
	IfExists bool   `json:"if_exists,omitempty"`
}

func (drp DropRowPolicy) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropRowPolicySQL(drp)
}

type DropMaterializedView struct {
	Name     string `json:"name"`
	IfExists bool   `json:"if_exists,omitempty"`
}

func (dmv DropMaterializedView) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropMaterializedViewSQL(dmv)
}

type DropTable struct {
	Name    string `json:"name"`
	Cascade bool   `json:"cascade,omitempty"`
}

func (dt DropTable) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropTableSQL(dt)
}

type DropSchema struct {
	Name     string `json:"name"`
	Cascade  bool   `json:"cascade,omitempty"`
	IfExists bool   `json:"if_exists,omitempty"`
}

func (ds DropSchema) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropSchemaSQL(ds)
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

func (a AddColumn) ToSQL(dialect, tableName string) ([]string, error) {
	return getDialect(dialect).AddColumnSQL(a, tableName)
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
		sqliteDialect, _ := getDialect(DialectSQLite).(*SQLiteDialect)
		queries, err := sqliteDialect.RecreateTableForAlter(at.Name, newSchema, renameMap)
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

func computeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

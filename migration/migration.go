package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/oarkflow/migration/drivers"
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
	Connection  string        `json:"Connection"`
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
	CreateView           []CreateView           `json:"CreateView,omitempty"`
	DropView             []DropView             `json:"DropView,omitempty"`
	RenameView           []RenameView           `json:"RenameView,omitempty"`
	CreateFunction       []CreateFunction       `json:"CreateFunction,omitempty"`
	DropFunction         []DropFunction         `json:"DropFunction,omitempty"`
	RenameFunction       []RenameFunction       `json:"RenameFunction,omitempty"`
	CreateProcedure      []CreateProcedure      `json:"CreateProcedure,omitempty"`
	DropProcedure        []DropProcedure        `json:"DropProcedure,omitempty"`
	RenameProcedure      []RenameProcedure      `json:"RenameProcedure,omitempty"`
	CreateTrigger        []CreateTrigger        `json:"CreateTrigger,omitempty"`
	DropTrigger          []DropTrigger          `json:"DropTrigger,omitempty"`
	RenameTrigger        []RenameTrigger        `json:"RenameTrigger,omitempty"`
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
	Default       any         `json:"default,omitempty"`
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
	Name string `json:"name"`
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

type CreateView struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	OrReplace  bool   `json:"or_replace,omitempty"`
}

func (cv CreateView) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).CreateViewSQL(cv)
}

type DropView struct {
	Name     string `json:"name"`
	Cascade  bool   `json:"cascade,omitempty"`
	IfExists bool   `json:"if_exists,omitempty"`
}

func (dv DropView) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropViewSQL(dv)
}

type RenameView struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

func (rv RenameView) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).RenameViewSQL(rv)
}

type CreateFunction struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	OrReplace  bool   `json:"or_replace,omitempty"`
}

func (cf CreateFunction) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).CreateFunctionSQL(cf)
}

type DropFunction struct {
	Name     string `json:"name"`
	Cascade  bool   `json:"cascade,omitempty"`
	IfExists bool   `json:"if_exists,omitempty"`
}

func (df DropFunction) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropFunctionSQL(df)
}

type RenameFunction struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

func (rf RenameFunction) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).RenameFunctionSQL(rf)
}

type CreateProcedure struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	OrReplace  bool   `json:"or_replace,omitempty"`
}

func (cp CreateProcedure) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).CreateProcedureSQL(cp)
}

type DropProcedure struct {
	Name     string `json:"name"`
	Cascade  bool   `json:"cascade,omitempty"`
	IfExists bool   `json:"if_exists,omitempty"`
}

func (dp DropProcedure) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropProcedureSQL(dp)
}

type RenameProcedure struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

func (rp RenameProcedure) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).RenameProcedureSQL(rp)
}

type CreateTrigger struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	OrReplace  bool   `json:"or_replace,omitempty"`
}

func (ct CreateTrigger) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).CreateTriggerSQL(ct)
}

type DropTrigger struct {
	Name     string `json:"name"`
	Cascade  bool   `json:"cascade,omitempty"`
	IfExists bool   `json:"if_exists,omitempty"`
}

func (dt DropTrigger) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).DropTriggerSQL(dt)
}

type RenameTrigger struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

func (rt RenameTrigger) ToSQL(dialect string) (string, error) {
	return getDialect(dialect).RenameTriggerSQL(rt)
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
	for _, cv := range op.CreateView {
		q, err := cv.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in CreateView: %w", err)
		}
		queries = append(queries, q)
	}
	for _, dv := range op.DropView {
		q, err := dv.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropView: %w", err)
		}
		queries = append(queries, q)
	}
	for _, rv := range op.RenameView {
		q, err := rv.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in RenameView: %w", err)
		}
		queries = append(queries, q)
	}
	for _, cf := range op.CreateFunction {
		q, err := cf.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in CreateFunction: %w", err)
		}
		queries = append(queries, q)
	}
	for _, df := range op.DropFunction {
		q, err := df.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropFunction: %w", err)
		}
		queries = append(queries, q)
	}
	for _, rf := range op.RenameFunction {
		q, err := rf.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in RenameFunction: %w", err)
		}
		queries = append(queries, q)
	}
	for _, cp := range op.CreateProcedure {
		q, err := cp.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in CreateProcedure: %w", err)
		}
		queries = append(queries, q)
	}
	for _, dp := range op.DropProcedure {
		q, err := dp.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropProcedure: %w", err)
		}
		queries = append(queries, q)
	}
	for _, rp := range op.RenameProcedure {
		q, err := rp.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in RenameProcedure: %w", err)
		}
		queries = append(queries, q)
	}
	for _, ct := range op.CreateTrigger {
		q, err := ct.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in CreateTrigger: %w", err)
		}
		queries = append(queries, q)
	}
	for _, dt := range op.DropTrigger {
		q, err := dt.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in DropTrigger: %w", err)
		}
		queries = append(queries, q)
	}
	for _, rt := range op.RenameTrigger {
		q, err := rt.ToSQL(dialect)
		if err != nil {
			return nil, fmt.Errorf("error in RenameTrigger: %w", err)
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

// RunSeeds executes the seed SQL statements for a given SeedDefinition.
// In a full implementation the queries would be run against the DB.
func RunSeeds(seed SeedDefinition, dialect string) error {
	queries, err := seed.ToSQL(dialect)
	if err != nil {
		return err
	}
	// Execute each query (here we simply print them as a placeholder).
	for _, q := range queries {
		fmt.Println(q) // ...replace with actual DB execution
	}
	return nil
}

func computeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func NewDriver(driver string, dsn string) (IDatabaseDriver, error) {
	switch driver {
	case "mysql":
		return drivers.NewMySQLDriver(dsn)
	case "postgres":
		return drivers.NewPostgresDriver(dsn)
	case "sqlite":
		return drivers.NewSQLiteDriver(dsn)
	}
	return nil, fmt.Errorf("unsupported driver: %s", driver)
}

// NewHistoryDriver returns an implementation of HistoryDriver (file, db, etc.)
func NewHistoryDriver(driver, dialect, config string) (HistoryDriver, error) {
	switch driver {
	case "file":
		return NewFileHistoryDriver(config), nil
	case "db":
		return NewDatabaseHistoryDriver(dialect, config)
	default:
		return nil, fmt.Errorf("unsupported history driver: %s", driver)
	}
}

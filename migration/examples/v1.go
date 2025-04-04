package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	
	"github.com/oarkflow/bcl"
)

var driverTemplates = make(map[string]*DriverTemplate)
var tableSchemas = make(map[string]*CreateTable)
var schemaMutex sync.Mutex

// DriverTemplate defines all stub templates for a specific dialect.
type DriverTemplate struct {
	Name                   string                     `json:"name"`
	CreateTable            string                     `json:"CreateTable"`
	DropTable              string                     `json:"DropTable"`
	AlterTableAddColumn    string                     `json:"AlterTableAddColumn"`
	ColumnDefinition       string                     `json:"ColumnDefinition"`
	PrimaryKey             string                     `json:"PrimaryKey"`
	RenameTable            string                     `json:"RenameTable"`
	RenameColumn           string                     `json:"RenameColumn"`
	DeleteData             string                     `json:"DeleteData"`
	CreateMigrationHistory string                     `json:"CreateMigrationHistory"`
	BeginTransaction       string                     `json:"BeginTransaction"`
	StartTransaction       string                     `json:"StartTransaction"`
	CommitTransaction      string                     `json:"CommitTransaction"`
	DataTypes              map[string]DataTypeMapping `json:"DataTypes"`
	NotNull                string                     `json:"NotNull"`
	Default                string                     `json:"Default"`
	Check                  string                     `json:"Check"`
}

// DataTypeMapping defines how to map a generic type.
type DataTypeMapping struct {
	Template      string `json:"template"`
	Fallback      string `json:"fallback"`
	AutoIncrement string `json:"auto_increment,omitempty"`
}

// LoadDriverTemplates loads all driver templates from the given BCL file.
func LoadDriverTemplates(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read driver config file: %w", err)
	}
	var drivers []DriverTemplate
	if err := json.Unmarshal(data, &drivers); err != nil {
		return fmt.Errorf("failed to unmarshal driver config: %w, file: %s", err, path)
	}
	for i := range drivers {
		driverTemplates[drivers[i].Name] = &drivers[i]
	}
	return nil
}

// ----------------------------
// Migration DSL types
// ----------------------------

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

func (r RenameColumn) ToSQL(driverName, tableName string) (string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return "", fmt.Errorf("driver '%s' not found", driverName)
	}
	// Use the RenameColumn template from the driver stub.
	sql := tmpl.RenameColumn
	sql = strings.ReplaceAll(sql, "{table}", tableName)
	sql = strings.ReplaceAll(sql, "{from}", r.From)
	sql = strings.ReplaceAll(sql, "{to}", r.To)
	return sql, nil
}

type RenameTable struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
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

// ----------------------------
// Helper Functions
// ----------------------------

// generateColumnDefinition creates a column definition using the driver template.
func generateColumnDefinition(driverName string, col AddColumn) (string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return "", fmt.Errorf("driver '%s' not found", driverName)
	}
	// Lookup data type mapping.
	mapping, ok := tmpl.DataTypes[strings.ToLower(col.Type)]
	if !ok {
		return "", fmt.Errorf("no data type mapping for type '%s' in driver '%s'", col.Type, driverName)
	}
	var typeStr string
	if col.AutoIncrement && mapping.AutoIncrement != "" {
		typeStr = mapping.AutoIncrement
	} else {
		if col.Size > 0 && strings.Contains(mapping.Template, "{size}") {
			typeStr = strings.ReplaceAll(mapping.Template, "{size}", fmt.Sprintf("%d", col.Size))
		} else {
			typeStr = mapping.Fallback
		}
	}
	// Process options using the stubs.
	notNull := ""
	if !col.Nullable {
		notNull = tmpl.NotNull
	}
	def := ""
	if col.Default != "" {
		def = strings.ReplaceAll(tmpl.Default, "{value}", col.Default)
	}
	check := ""
	if col.Check != "" {
		check = strings.ReplaceAll(tmpl.Check, "{expression}", col.Check)
	}
	// Replace placeholders in the ColumnDefinition template.
	colDef := tmpl.ColumnDefinition
	colDef = strings.ReplaceAll(colDef, "{name}", col.Name)
	colDef = strings.ReplaceAll(colDef, "{type}", typeStr)
	colDef = strings.ReplaceAll(colDef, "{notnull}", notNull)
	colDef = strings.ReplaceAll(colDef, "{default}", def)
	colDef = strings.ReplaceAll(colDef, "{check}", check)
	return colDef, nil
}

// ----------------------------
// CreateTable SQL Generation
// ----------------------------

func (ct CreateTable) ToSQL(driverName string, up bool) (string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return "", fmt.Errorf("driver '%s' not found", driverName)
	}
	if up {
		var colDefs []string
		for _, col := range ct.Columns {
			def, err := generateColumnDefinition(driverName, col)
			if err != nil {
				return "", err
			}
			colDefs = append(colDefs, def)
		}
		// If primary keys are defined, use the PrimaryKey template.
		if len(ct.PrimaryKey) > 0 {
			pk := tmpl.PrimaryKey
			pk = strings.ReplaceAll(pk, "{columns}", strings.Join(ct.PrimaryKey, ", "))
			colDefs = append(colDefs, pk)
		}
		sql := tmpl.CreateTable
		sql = strings.ReplaceAll(sql, "{table}", ct.Name)
		sql = strings.ReplaceAll(sql, "{columns}", strings.Join(colDefs, ", "))
		return sql, nil
	}
	// For down migrations, use the DropTable template.
	sql := tmpl.DropTable
	sql = strings.ReplaceAll(sql, "{table}", ct.Name)
	return sql, nil
}

// ----------------------------
// AddColumn SQL Generation (for ALTER statements)
// ----------------------------

func (a AddColumn) ToSQL(driverName, tableName string) ([]string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return nil, fmt.Errorf("driver '%s' not found", driverName)
	}
	def, err := generateColumnDefinition(driverName, a)
	if err != nil {
		return nil, err
	}
	sql := tmpl.AlterTableAddColumn
	sql = strings.ReplaceAll(sql, "{table}", tableName)
	sql = strings.ReplaceAll(sql, "{column_definition}", def)
	return []string{sql}, nil
}

// ----------------------------
// DeleteData SQL Generation
// ----------------------------

func (d DeleteData) ToSQL(driverName string) (string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return "", fmt.Errorf("driver '%s' not found", driverName)
	}
	sql := tmpl.DeleteData
	sql = strings.ReplaceAll(sql, "{table}", d.Name)
	sql = strings.ReplaceAll(sql, "{condition}", d.Where)
	return sql, nil
}

// ----------------------------
// RenameTable SQL Generation
// ----------------------------

func (rt RenameTable) ToSQL(driverName string) (string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return "", fmt.Errorf("driver '%s' not found", driverName)
	}
	sql := tmpl.RenameTable
	sql = strings.ReplaceAll(sql, "{old}", rt.OldName)
	sql = strings.ReplaceAll(sql, "{new}", rt.NewName)
	return sql, nil
}

// ----------------------------
// DropTable SQL Generation
// ----------------------------

func (dt DropTable) ToSQL(driverName string) (string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return "", fmt.Errorf("driver '%s' not found", driverName)
	}
	sql := tmpl.DropTable
	sql = strings.ReplaceAll(sql, "{table}", dt.Name)
	return sql, nil
}

// ----------------------------
// Operation and Migration SQL Generation
// ----------------------------

func (at AlterTable) ToSQL(driverName string) ([]string, error) {
	var queries []string
	for _, addCol := range at.AddColumn {
		qList, err := addCol.ToSQL(driverName, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	// Additional processing for DropColumn and RenameColumn would follow a similar pattern
	for _, dropCol := range at.DropColumn {
		// Example: For drop column, you might decide to build a SQL statement via a stub if provided.
		q := fmt.Sprintf("/* DROP COLUMN %s from %s: not supported via stub */", dropCol.Name, at.Name)
		queries = append(queries, q)
	}
	for _, renCol := range at.RenameColumn {
		q, err := renCol.ToSQL(driverName, at.Name)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, nil
}

func (op Operation) ToSQL(driverName string) ([]string, error) {
	var queries []string
	for _, ct := range op.CreateTable {
		q, err := ct.ToSQL(driverName, true)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
		// Optionally save schema for SQLite recreation.
		schemaMutex.Lock()
		tmp := ct
		tableSchemas[ct.Name] = &tmp
		schemaMutex.Unlock()
	}
	for _, at := range op.AlterTable {
		qList, err := at.ToSQL(driverName)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	for _, dd := range op.DeleteData {
		q, err := dd.ToSQL(driverName)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	// Other operations (DropEnumType, DropRowPolicy, etc.) can be implemented similarly.
	return queries, nil
}

func (m Migration) ToSQL(driverName string, up bool) ([]string, error) {
	var queries []string
	var ops []Operation
	if up {
		ops = m.Up
	} else {
		ops = m.Down
	}
	for _, op := range ops {
		qList, err := op.ToSQL(driverName)
		if err != nil {
			return nil, err
		}
		queries = append(queries, qList...)
	}
	return queries, nil
}

// ----------------------------
// Transaction Wrappers Using Driver Templates
// ----------------------------

func WrapInTransaction(queries []string, driverName string) []string {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return queries
	}
	txQueries := []string{tmpl.StartTransaction}
	txQueries = append(txQueries, queries...)
	txQueries = append(txQueries, tmpl.CommitTransaction)
	return txQueries
}

func WrapInTransactionWithConfig(queries []string, trans Transaction, driverName string) []string {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return queries
	}
	var beginStmt string
	if trans.IsolationLevel != "" {
		beginStmt = strings.ReplaceAll(tmpl.BeginTransaction, "{isolation}", trans.IsolationLevel)
	} else {
		beginStmt = tmpl.StartTransaction
	}
	txQueries := []string{beginStmt}
	txQueries = append(txQueries, queries...)
	txQueries = append(txQueries, tmpl.CommitTransaction)
	return txQueries
}

// ----------------------------
// Migration History Table SQL Generation
// ----------------------------

func CreateMigrationHistoryTableSQL(driverName string) (string, error) {
	tmpl, ok := driverTemplates[driverName]
	if !ok {
		return "", fmt.Errorf("driver '%s' not found", driverName)
	}
	return tmpl.CreateMigrationHistory, nil
}

func main() {
	// Parse flags for paths and driver selection.
	configPath := flag.String("config", "migrations/migration.bcl", "Path to migration DSL configuration file (e.g., migrations.bcl)")
	driverPath := flag.String("driver", "drivers.json", "Path to driver configuration BCL file (e.g., drivers.bcl)")
	driverName := flag.String("driver-name", "postgres", "Driver name to use (e.g., postgres, mysql, sqlite)")
	dryRun := flag.Bool("dry-run", true, "Output the generated SQL without executing migrations")
	flag.Parse()
	
	if *configPath == "" || *driverPath == "" || *driverName == "" {
		log.Fatal("Please specify -config, -driver, and -driver-name flags")
	}
	
	// Load the driver templates (all SQL fragments come from the BCL file).
	if err := LoadDriverTemplates(*driverPath); err != nil {
		log.Fatalf("Error loading driver templates: %v", err)
	}
	
	// Read and unmarshal the migration DSL configuration.
	data, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("Error reading migration config file: %v", err)
	}
	var cfg Config
	_, err = bcl.Unmarshal(data, &cfg)
	if err != nil {
		log.Fatalf("Error unmarshalling migration config: %v", err)
	}
	
	// Print the SQL for the migration history table.
	historySQL, err := CreateMigrationHistoryTableSQL(*driverName)
	if err != nil {
		log.Fatalf("Error generating migration history table SQL: %v", err)
	}
	log.Println("Migration History Table SQL:")
	log.Println(historySQL)
	
	// Process each
	for _, m := range cfg.Migrations {
		log.Printf("Starting migration: %s - %s", m.Name, m.Description)
		
		// Generate the UP SQL.
		upQueries, err := m.ToSQL(*driverName, true)
		if err != nil {
			log.Fatalf("Error generating UP SQL for migration '%s': %v", m.Name, err)
		}
		// Wrap the queries in a transaction using driver stubs.
		if len(m.Transaction) > 0 {
			upQueries = WrapInTransactionWithConfig(upQueries, m.Transaction[0], *driverName)
		} else {
			upQueries = WrapInTransaction(upQueries, *driverName)
		}
		log.Printf("Generated UP SQL for migration '%s':", m.Name)
		for _, q := range upQueries {
			log.Println(q)
		}
		
		// Execute the migration (here we just simulate a delay).
		if !*dryRun {
			log.Printf("Executing migration '%s'...", m.Name)
			// Here you would execute each SQL statement against your database.
			time.Sleep(100 * time.Millisecond)
		} else {
			log.Printf("Dry-run mode: Not executing migration '%s'.", m.Name)
		}
		
		// Generate the DOWN SQL.
		downQueries, err := m.ToSQL(*driverName, false)
		if err != nil {
			log.Fatalf("Error generating DOWN SQL for migration '%s': %v", m.Name, err)
		}
		if len(downQueries) > 0 {
			if len(m.Transaction) > 0 {
				downQueries = WrapInTransactionWithConfig(downQueries, m.Transaction[0], *driverName)
			} else {
				downQueries = WrapInTransaction(downQueries, *driverName)
			}
			log.Printf("Generated DOWN SQL for migration '%s':", m.Name)
			for _, q := range downQueries {
				log.Println(q)
			}
		} else {
			log.Printf("Warning: No DOWN SQL generated for migration '%s'.", m.Name)
		}
		
		log.Printf("Completed migration: %s", m.Name)
	}
	
	log.Println("All migrations completed successfully.")
}

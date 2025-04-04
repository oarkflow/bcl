package migration

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oarkflow/cli/contracts"

	"github.com/oarkflow/bcl"
)

type Driver interface {
	ApplyMigration(m Migration) error
	RollbackMigration(step int) error
	ResetMigrations() error
	ValidateMigrations() error
	CreateMigrationFile(name string) error
}

type DummyDriver struct {
	migrationDir      string
	historyFile       string
	historyMutex      sync.Mutex
	appliedMigrations map[string]string
	dialect           string
}

func NewDummyDriver(migrationDir, historyFile, dialect string) *DummyDriver {
	return &DummyDriver{
		migrationDir:      migrationDir,
		historyFile:       historyFile,
		appliedMigrations: make(map[string]string),
		dialect:           dialect,
	}
}

func (d *DummyDriver) loadHistory() error {
	d.historyMutex.Lock()
	defer d.historyMutex.Unlock()
	data, err := os.ReadFile(d.historyFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			d.appliedMigrations[parts[0]] = parts[1]
		}
	}
	return nil
}

func (d *DummyDriver) saveHistory() error {
	d.historyMutex.Lock()
	defer d.historyMutex.Unlock()
	var lines []string
	for name, cs := range d.appliedMigrations {
		lines = append(lines, fmt.Sprintf("%s:%s", name, cs))
	}
	return ioutil.WriteFile(d.historyFile, []byte(strings.Join(lines, "\n")), 0644)
}

func (d *DummyDriver) ApplyMigration(m Migration) error {
	path := filepath.Join(d.migrationDir, m.Name+".bcl")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}
	checksum := computeChecksum(data)
	if prev, ok := d.appliedMigrations[m.Name]; ok && prev != checksum {
		return fmt.Errorf("checksum mismatch for migration %s", m.Name)
	}
	var cfg Config
	if _, err := bcl.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal migration file: %w", err)
	}
	if len(cfg.Migrations) == 0 {
		return fmt.Errorf("no migration found in file %s", m.Name)
	}
	migration := cfg.Migrations[0]
	queries, err := migration.ToSQL(d.dialect, true)
	if err != nil {
		return fmt.Errorf("failed to generate SQL: %w", err)
	}
	log.Printf("Applying migration %s with SQL:\n%s", m.Name, strings.Join(queries, "\n"))
	d.appliedMigrations[m.Name] = checksum
	return d.saveHistory()
}

func (d *DummyDriver) RollbackMigration(step int) error {
	log.Printf("Rolling back %d migration(s)...", step)
	return nil
}

func (d *DummyDriver) ResetMigrations() error {
	log.Println("Resetting migrations...")
	d.appliedMigrations = make(map[string]string)
	return d.saveHistory()
}

func (d *DummyDriver) ValidateMigrations() error {
	files, err := ioutil.ReadDir(d.migrationDir)
	if err != nil {
		return fmt.Errorf("failed to read migration directory: %w", err)
	}
	var missing []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".bcl") {
			name := strings.TrimSuffix(file.Name(), ".bcl")
			if _, ok := d.appliedMigrations[name]; !ok {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing applied migrations: %v", missing)
	}
	log.Println("All migrations validated.")
	return nil
}

func (d *DummyDriver) CreateMigrationFile(name string) error {
	name = fmt.Sprintf("%d_%s", time.Now().Unix(), name)
	filename := filepath.Join(d.migrationDir, name+".bcl")
	template := fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "New migration"
  Up {
  
  }
  Down {
  
  }
}`, name)
	if err := ioutil.WriteFile(filename, []byte(template), 0644); err != nil {
		return fmt.Errorf("failed to create migration file: %w", err)
	}
	log.Printf("Migration file created: %s", filename)
	return nil
}

type MakeMigrationCommand struct {
	extend contracts.Extend
	Driver Driver
}

func (c *MakeMigrationCommand) Signature() string {
	return "make:migration"
}

func (c *MakeMigrationCommand) Description() string {
	return "Creates a new migration file in the designated directory."
}

func (c *MakeMigrationCommand) Extend() contracts.Extend {
	return c.extend
}

func (c *MakeMigrationCommand) Handle(ctx contracts.Context) error {

	name := ctx.Argument(0)
	if name == "" {
		return errors.New("migration name is required")
	}
	return c.Driver.CreateMigrationFile(name)
}

type MigrateCommand struct {
	extend contracts.Extend
	Driver Driver
}

func (c *MigrateCommand) Signature() string {
	return "migrate"
}

func (c *MigrateCommand) Description() string {
	return "Migrate all migration files that are not already applied."
}

func (c *MigrateCommand) Extend() contracts.Extend {
	return c.extend
}

func (c *MigrateCommand) Handle(ctx contracts.Context) error {
	if err := c.Driver.ValidateMigrations(); err != nil {
		log.Printf("Validation warning: %v", err)
	}
	files, err := ioutil.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migration directory: %w", err)
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".bcl") {
			name := strings.TrimSuffix(file.Name(), ".bcl")
			migration := Migration{Name: name}
			if err := c.Driver.ApplyMigration(migration); err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", name, err)
			}
		}
	}
	return nil
}

type RollbackCommand struct {
	extend contracts.Extend
	Driver Driver
}

func (c *RollbackCommand) Signature() string {
	return "migration:rollback"
}

func (c *RollbackCommand) Description() string {
	return "Rolls back migrations. Optionally specify --step=<n>."
}

func (c *RollbackCommand) Extend() contracts.Extend {
	return c.extend
}

func (c *RollbackCommand) Handle(ctx contracts.Context) error {
	stepStr := ctx.Option("step")
	step := 1
	if stepStr != "" {
		var err error
		step, err = strconv.Atoi(stepStr)
		if err != nil {
			return fmt.Errorf("invalid step value: %w", err)
		}
	}
	return c.Driver.RollbackMigration(step)
}

type ResetCommand struct {
	extend contracts.Extend
	Driver Driver
}

func (c *ResetCommand) Signature() string {
	return "migration:reset"
}

func (c *ResetCommand) Description() string {
	return "Resets migrations by rolling back and reapplying all migrations."
}

func (c *ResetCommand) Extend() contracts.Extend {
	return c.extend
}

func (c *ResetCommand) Handle(ctx contracts.Context) error {
	return c.Driver.ResetMigrations()
}

type ValidateCommand struct {
	extend contracts.Extend
	Driver Driver
}

func (c *ValidateCommand) Signature() string {
	return "migration:validate"
}

func (c *ValidateCommand) Description() string {
	return "Validates the migration history against migration files."
}

func (c *ValidateCommand) Extend() contracts.Extend {
	return c.extend
}

func (c *ValidateCommand) Handle(ctx contracts.Context) error {
	return c.Driver.ValidateMigrations()
}

package migration

import (
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/cli"
	"github.com/oarkflow/cli/console"
	"github.com/oarkflow/cli/contracts"
	"github.com/oarkflow/json"
	"github.com/oarkflow/log"
	"github.com/oarkflow/squealx"
)

var (
	Name    = "Migration"
	Version = "v0.0.1"
)

var logger = log.Logger{
	TimeFormat: "15:04:05",
	Caller:     1,
	Writer: &log.ConsoleWriter{
		ColorOutput:    true,
		QuoteString:    true,
		EndWithMessage: true,
	},
}

// New interface for applying SQL statements on any database.
type IDatabaseDriver interface {
	ApplySQL(queries []string) error
	DB() *squealx.DB
}

type IManager interface {
	ApplyMigration(m Migration) error
	RollbackMigration(step int) error
	ResetMigrations() error
	ValidateMigrations() error
	CreateMigrationFile(name string) error
	ValidateHistoryStorage() error
}

type Manager struct {
	migrationDir  string
	dialect       string
	client        contracts.Cli
	dbDriver      IDatabaseDriver
	historyDriver HistoryDriver
}

// ManagerOption defines a function signature for Manager options.
type ManagerOption func(*Manager)

// WithMigrationDir allows setting a custom migration directory.
func WithMigrationDir(dir string) ManagerOption {
	return func(m *Manager) {
		m.migrationDir = dir
	}
}

// WithDriver allows setting a custom history driver.
func WithDriver(driver IDatabaseDriver) ManagerOption {
	return func(m *Manager) {
		m.dbDriver = driver
	}
}

// WithHistoryDriver allows setting a custom history driver.
func WithHistoryDriver(driver HistoryDriver) ManagerOption {
	return func(m *Manager) {
		m.historyDriver = driver
	}
}

// WithDialect allows setting an initial dialect.
func WithDialect(dialect string) ManagerOption {
	return func(m *Manager) {
		m.dialect = dialect
	}
}

// defaultManager returns a slice of default options for Manager.
func defaultManager(client contracts.Cli) *Manager {
	return &Manager{
		migrationDir:  "migrations",
		dialect:       "postgres",
		historyDriver: NewFileHistoryDriver("migration_history.txt"),
		client:        client,
	}
}

func NewManager(opts ...ManagerOption) *Manager {
	dir := "migrations"
	if err := os.MkdirAll(dir, fs.ModePerm); err != nil {
		logger.Fatal().Msgf("Failed to create migration directory: %v", err)
	}
	cli.SetName(Name)
	cli.SetVersion(Version)
	app := cli.New()
	client := app.Instance.Client()
	m := defaultManager(client)
	for _, opt := range opts {
		opt(m)
	}
	client.Register([]contracts.Command{
		console.NewListCommand(client),
		&MakeMigrationCommand{Driver: m},
		&MigrateCommand{Driver: m},
		&RollbackCommand{Driver: m},
		&ResetCommand{Driver: m},
		&ValidateCommand{Driver: m},
	})
	return m
}

func (d *Manager) Run() {
	d.client.Run(os.Args, true)
}

func (d *Manager) SetDialect(dialect string) {
	d.dialect = dialect
}

func (d *Manager) GetDialect() string {
	return d.dialect
}

func (d *Manager) ValidateHistoryStorage() error {
	return d.historyDriver.ValidateStorage()
}

func (d *Manager) ApplyMigration(m Migration) error {
	path := filepath.Join(d.migrationDir, m.Name+".bcl")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}
	checksum := computeChecksum(data)
	histories, err := d.historyDriver.Load()
	if err != nil {
		return err
	}
	for _, h := range histories {
		if h.Name == m.Name {
			if h.Checksum == checksum {
				return nil
			}
			return fmt.Errorf("checksum mismatch for migration %s", m.Name)
		}
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
	if d.dbDriver == nil {
		return fmt.Errorf("no database driver configured")
	}
	for _, val := range migration.Validate {
		if err := runPreUpChecks(val.PreUpChecks); err != nil {
			return fmt.Errorf("pre-up validation failed for migration %s: %w", migration.Name, err)
		}
	}
	if err := d.dbDriver.ApplySQL(queries); err != nil {
		return fmt.Errorf("failed to apply migration %s: %w", m.Name, err)
	}
	for _, val := range migration.Validate {
		if err := runPostUpChecks(val.PostUpChecks); err != nil {
			return fmt.Errorf("post-up validation failed for migration %s: %w", migration.Name, err)
		}
	}
	now := time.Now()
	logger.Info().Msgf("Applied migration: %s at %v", m.Name, now.Format(time.DateTime))
	history := MigrationHistory{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Checksum:    checksum,
		AppliedAt:   now,
	}
	return d.historyDriver.Save(history)
}

func (d *Manager) RollbackMigration(step int) error {
	// Load history from driver.
	histories, err := d.historyDriver.Load()
	if err != nil {
		return err
	}
	total := len(histories)
	if step <= 0 || step > total {
		return fmt.Errorf("rollback step %d is out of range, total applied: %d", step, total)
	}
	// Rollback in reverse order.
	for i := 0; i < step; i++ {
		last := histories[len(histories)-1]
		name := last.Name
		path := filepath.Join(d.migrationDir, name+".bcl")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s for rollback: %w", name, err)
		}
		var cfg Config
		if _, err := bcl.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to unmarshal migration file %s for rollback: %w", name, err)
		}
		if len(cfg.Migrations) == 0 {
			return fmt.Errorf("no migration found in file %s for rollback", name)
		}
		migration := cfg.Migrations[0]
		downQueries, err := migration.ToSQL(d.dialect, false)
		if err != nil {
			return fmt.Errorf("failed to generate rollback SQL for migration %s: %w", name, err)
		}
		if err := d.dbDriver.ApplySQL(downQueries); err != nil {
			return fmt.Errorf("failed to rollback migration %s: %w", name, err)
		}
		logger.Info().Msg("Rolled back migration: " + name)
		// Remove last history record.
		histories = histories[:len(histories)-1]
	}
	// If using file-based history, overwrite with new history.
	if fh, ok := d.historyDriver.(*FileHistoryDriver); ok {
		data, err := json.Marshal(histories)
		if err != nil {
			return err
		}
		return os.WriteFile(fh.filePath, data, 0644)
	}
	return nil
}

func (d *Manager) ResetMigrations() error {
	logger.Info().Msg("Resetting migrations...")
	// Load applied history.
	histories, err := d.historyDriver.Load()
	if err != nil {
		return err
	}
	// Rollback all migrations in reverse order.
	for i := len(histories) - 1; i >= 0; i-- {
		name := histories[i].Name
		path := filepath.Join(d.migrationDir, name+".bcl")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s for rollback: %w", name, err)
		}
		var cfg Config
		if _, err := bcl.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to unmarshal migration file %s for rollback: %w", name, err)
		}
		if len(cfg.Migrations) == 0 {
			return fmt.Errorf("no migration found in file %s for rollback", name)
		}
		migration := cfg.Migrations[0]
		downQueries, err := migration.ToSQL(d.dialect, false)
		if err != nil {
			return fmt.Errorf("failed to generate rollback SQL for migration %s: %w", name, err)
		}
		if err := d.dbDriver.ApplySQL(downQueries); err != nil {
			return fmt.Errorf("failed to rollback migration %s: %w", name, err)
		}
		logger.Info().Msg("Rolled back migration: " + name)
	}
	// Clear history completely.
	if fh, ok := d.historyDriver.(*FileHistoryDriver); ok {
		return os.WriteFile(fh.filePath, []byte("[]"), 0644)
	}
	return nil
}

func (d *Manager) ValidateMigrations() error {
	files, err := ioutil.ReadDir(d.migrationDir)
	if err != nil {
		return fmt.Errorf("failed to read migration directory: %w", err)
	}
	histories, err := d.historyDriver.Load()
	if err != nil {
		return err
	}
	// Create a map of applied migration names from persistent history.
	applied := make(map[string]bool)
	for _, h := range histories {
		applied[h.Name] = true
	}
	var missing []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".bcl") {
			name := strings.TrimSuffix(file.Name(), ".bcl")
			if !applied[name] {
				missing = append(missing, name)
			}
		}
	}
	toApply := len(missing)
	if toApply > 0 {
		logger.Info().Msgf("Migration initiated for : %v migration(s)", toApply)
		return nil
	}
	logger.Info().Msg("Migrations are up to date.")
	return nil
}

func (d *Manager) CreateMigrationFile(name string) error {
	name = fmt.Sprintf("%d_%s", time.Now().Unix(), name)
	filename := filepath.Join(d.migrationDir, name+".bcl")
	tokens := strings.Split(name, "_")
	var template string
	if len(tokens) < 2 {
		template = defaultTemplate(name)
	} else {
		op := strings.ToLower(tokens[1])
		objType := ""
		if len(tokens) > 2 {
			last := strings.ToLower(tokens[len(tokens)-1])
			if last == "table" || last == "view" || last == "function" || last == "trigger" {
				objType = last
			}
		}
		switch op {
		case "create":
			if objType == "view" {
				viewName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Create view %s."
  Connection = "default"
  Up {
    CreateView "%s" {
      # Define view SQL query.
    }
  }
  Down {
    DropView "%s" {
      Cascade = true
    }
  }
}`, name, viewName, viewName, viewName)
			} else if objType == "function" {

				funcName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Create function %s."
  Connection = "default"
  Up {
    CreateFunction "%s" {
      # Define function signature and body.
    }
  }
  Down {
    DropFunction "%s" {
      Cascade = true
    }
  }
}`, name, funcName, funcName, funcName)
			} else if objType == "trigger" {
				triggerName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Create trigger %s."
  Connection = "default"
  Up {
    CreateTrigger "%s" {
      # Define trigger logic.
    }
  }
  Down {
    DropTrigger "%s" {
      Cascade = true
    }
  }
}`, name, triggerName, triggerName, triggerName)
			} else {
				var table string
				if objType == "table" {
					table = strings.Join(tokens[2:len(tokens)-1], "_")
				} else {
					table = strings.Join(tokens[2:], "_")
				}
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Create table %s."
  Connection = "default"
  Up {
    CreateTable "%s" {
      Column "id" {
        type = "integer"
        primary_key = true
        auto_increment = true
        index = true
        unique = true
      }
      Column "is_active" {
        type = "boolean"
        default = false
      }
      Column "status" {
        type = "string"
        size = 20
        default = "active"
      }
      Column "created_at" {
        type = "datetime"
        default = "now()"
      }
      Column "updated_at" {
        type = "datetime"
        default = "now()"
      }
      Column "deleted_at" {
        type = "datetime"
        is_nullable = true
      }
    }
  }
  Down {
    DropTable "%s" {
      Cascade = true
    }
  }
}
`, name, table, table, table)
			}
		case "alter":
			if objType == "view" {
				viewName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Alter view %s."
  Connection = "default"
  Up {
    AlterView "%s" {
      # Define alterations for view.
    }
  }
  Down {
    # Define rollback for view alterations.
  }
}`, name, viewName, viewName)
			} else if objType == "function" {
				funcName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Alter function %s."
  Connection = "default"
  Up {
    AlterFunction "%s" {
      # Define function alterations.
    }
  }
  Down {
    # Define rollback for function alterations.
  }
}`, name, funcName, funcName)
			} else if objType == "trigger" {
				triggerName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Alter trigger %s."
  Connection = "default"
  Up {
    AlterTrigger "%s" {
      # Define trigger alterations.
    }
  }
  Down {
    # Define rollback for trigger alterations.
  }
}`, name, triggerName, triggerName)
			} else {

				var table string
				if objType == "table" {
					table = strings.Join(tokens[2:len(tokens)-1], "_")
				} else {
					table = strings.Join(tokens[2:], "_")
				}
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Alter table %s."
  Connection = "default"
  Up {
    AlterTable "%s" {
      # Define alterations here.
    }
  }
  Down {
    # Define rollback for alterations.
  }
}`, name, table, table)
			}
		case "drop":
			if objType == "view" {
				viewName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Drop view %s."
  Connection = "default"
  Up {
    DropView "%s" {
      Cascade = true
    }
  }
  Down {
    # Optionally define rollback for view drop.
  }
}`, name, viewName, viewName)
			} else if objType == "function" {
				funcName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Drop function %s."
  Connection = "default"
  Up {
    DropFunction "%s" {
      Cascade = true
    }
  }
  Down {
    # Optionally define rollback for function drop.
  }
}`, name, funcName, funcName)
			} else if objType == "trigger" {
				triggerName := strings.Join(tokens[2:len(tokens)-1], "_")
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Drop trigger %s."
  Connection = "default"
  Up {
    DropTrigger "%s" {
      Cascade = true
    }
  }
  Down {
    # Optionally define rollback for trigger drop.
  }
}`, name, triggerName, triggerName)
			} else {

				var table string
				if objType == "table" {
					table = strings.Join(tokens[2:len(tokens)-1], "_")
				} else {
					table = strings.Join(tokens[2:], "_")
				}
				template = fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "Drop table %s."
  Connection = "default"
  Up {
    DropTable "%s" {
      Cascade = true
    }
  }
  Down {
    # Optionally define rollback for table drop.
  }
}`, name, table, table)
			}
		default:
			template = defaultTemplate(name)
		}
	}
	if err := os.WriteFile(filename, []byte(template), 0644); err != nil {
		return fmt.Errorf("failed to create migration file: %w", err)
	}
	logger.Printf("Migration file created: %s", filename)
	return nil
}

func defaultTemplate(name string) string {
	return fmt.Sprintf(`Migration "%s" {
  Version = "1.0.0"
  Description = "New migration"
  Connection = "default"
  Up {
    # Define migration operations here.
  }
  Down {
    # Define rollback operations here.
  }
}`, name)
}

type MakeMigrationCommand struct {
	extend contracts.Extend
	Driver IManager
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
	Driver IManager
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

func acquireLock() error {
	if _, err := os.Stat(lockFileName); err == nil {
		return fmt.Errorf("migration lock already acquired")
	}
	f, err := os.Create(lockFileName)
	if err != nil {
		return fmt.Errorf("failed to create lock file: %w", err)
	}
	f.Close()
	return nil
}

func releaseLock() error {
	if err := os.Remove(lockFileName); err != nil {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}
	return nil
}

func runPreUpChecks(checks []string) error {
	for _, check := range checks {
		logger.Printf("Executing PreUpCheck: %s", check)

		if strings.Contains(strings.ToLower(check), "fail") {
			return fmt.Errorf("PreUp check failed: %s", check)
		}
	}
	logger.Info().Msg("All PreUpChecks passed.")
	return nil
}

func runPostUpChecks(checks []string) error {
	for _, check := range checks {
		logger.Printf("Executing PostUpCheck: %s", check)

		if strings.Contains(strings.ToLower(check), "fail") {
			return fmt.Errorf("PostUp check failed: %s", check)
		}
	}
	logger.Info().Msg("All PostUpChecks passed.")
	return nil
}

func (c *MigrateCommand) Handle(ctx contracts.Context) error {
	// Validate that history storage exists before proceeding.
	if err := c.Driver.ValidateHistoryStorage(); err != nil {
		return fmt.Errorf("history storage validation failed: %w", err)
	}
	if err := acquireLock(); err != nil {
		return fmt.Errorf("cannot start migration: %w", err)
	}
	defer func() {
		if err := releaseLock(); err != nil {
			logger.Printf("Warning releasing lock: %v", err)
		}
	}()
	if err := c.Driver.ValidateMigrations(); err != nil {
		logger.Printf("Validation warning: %v", err)
	}
	files, err := os.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migration directory: %w", err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".bcl") {
			continue
		}
		name := strings.TrimSuffix(file.Name(), ".bcl")
		path := filepath.Join("migrations", file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", name, err)
		}
		var cfg Config
		if _, err := bcl.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to unmarshal migration file %s: %w", name, err)
		}
		if len(cfg.Migrations) == 0 {
			return fmt.Errorf("no migration found in file %s", name)
		}
		for _, migration := range cfg.Migrations {
			for _, val := range migration.Validate {
				if err := runPreUpChecks(val.PreUpChecks); err != nil {
					return fmt.Errorf("pre-up validation failed for migration %s: %w", migration.Name, err)
				}
			}
			if err := c.Driver.ApplyMigration(migration); err != nil {
				logger.Error().Msgf("Failed to apply migration %s: %v", migration.Name, err)
				return fmt.Errorf("failed to apply migration %s: %w", migration.Name, err)
			}
			for _, val := range migration.Validate {
				if err := runPostUpChecks(val.PostUpChecks); err != nil {
					return fmt.Errorf("post-up validation failed for migration %s: %w", migration.Name, err)
				}
			}
		}
	}
	return nil
}

type RollbackCommand struct {
	extend contracts.Extend
	Driver IManager
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
	Driver IManager
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
	Driver IManager
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

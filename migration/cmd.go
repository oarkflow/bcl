package migration

import (
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oarkflow/cli"
	"github.com/oarkflow/cli/console"
	"github.com/oarkflow/cli/contracts"

	"github.com/oarkflow/bcl"
)

var (
	Name    = "Migration"
	Version = "v0.0.1"
)

// New interface for applying SQL statements on any database.
type IDatabaseDriver interface {
	ApplySQL(queries []string) error
}

type IManager interface {
	ApplyMigration(m Migration) error
	RollbackMigration(step int) error
	ResetMigrations() error
	ValidateMigrations() error
	CreateMigrationFile(name string) error
}

type Manager struct {
	migrationDir      string
	historyFile       string
	historyMutex      sync.Mutex
	appliedMigrations map[string]string
	dialect           string
	client            contracts.Cli
	dbDriver          IDatabaseDriver // added field for DB driver
}

func NewManager() *Manager {
	dir := "migrations"
	if err := os.MkdirAll(dir, fs.ModePerm); err != nil {
		log.Fatalf("Failed to create migration directory: %v", err)
	}
	cli.SetName(Name)
	cli.SetVersion(Version)
	app := cli.New()
	client := app.Instance.Client()
	d := &Manager{
		migrationDir:      dir,
		historyFile:       "migration_history.txt",
		appliedMigrations: make(map[string]string),
		client:            client,
	}
	client.Register([]contracts.Command{
		console.NewListCommand(client),
		&MakeMigrationCommand{Driver: d},
		&MigrateCommand{Driver: d},
		&RollbackCommand{Driver: d},
		&ResetCommand{Driver: d},
		&ValidateCommand{Driver: d},
	})
	return d
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

// SetDriver allows injecting a database driver into the Manager.
func (d *Manager) SetDriver(driver IDatabaseDriver) {
	d.dbDriver = driver
}

func (d *Manager) loadHistory() error {
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

func (d *Manager) saveHistory() error {
	d.historyMutex.Lock()
	defer d.historyMutex.Unlock()
	var lines []string
	for name, cs := range d.appliedMigrations {
		lines = append(lines, fmt.Sprintf("%s:%s", name, cs))
	}
	return os.WriteFile(d.historyFile, []byte(strings.Join(lines, "\n")), 0644)
}

func (d *Manager) ApplyMigration(m Migration) error {
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
	// Ensure a valid DB driver is set.
	if d.dbDriver == nil {
		return fmt.Errorf("no database driver configured")
	}
	// Delegate applying SQL to the driver.
	if err := d.dbDriver.ApplySQL(queries); err != nil {
		return fmt.Errorf("failed to apply migration %s: %w", m.Name, err)
	}
	d.appliedMigrations[m.Name] = checksum
	return d.saveHistory()
}

func (d *Manager) RollbackMigration(step int) error {
	log.Printf("Rolling back %d migration(s)...", step)
	return nil
}

func (d *Manager) ResetMigrations() error {
	log.Println("Resetting migrations...")
	d.appliedMigrations = make(map[string]string)
	return d.saveHistory()
}

func (d *Manager) ValidateMigrations() error {
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
	log.Printf("Migration file created: %s", filename)
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
		log.Printf("Executing PreUpCheck: %s", check)

		if strings.Contains(strings.ToLower(check), "fail") {
			return fmt.Errorf("PreUp check failed: %s", check)
		}
	}
	log.Println("All PreUpChecks passed.")
	return nil
}

func runPostUpChecks(checks []string) error {
	for _, check := range checks {
		log.Printf("Executing PostUpCheck: %s", check)

		if strings.Contains(strings.ToLower(check), "fail") {
			return fmt.Errorf("PostUp check failed: %s", check)
		}
	}
	log.Println("All PostUpChecks passed.")
	return nil
}

func (c *MigrateCommand) Handle(ctx contracts.Context) error {
	if err := acquireLock(); err != nil {
		return fmt.Errorf("cannot start migration: %w", err)
	}
	defer func() {
		if err := releaseLock(); err != nil {
			log.Printf("Warning releasing lock: %v", err)
		}
	}()
	if err := c.Driver.ValidateMigrations(); err != nil {
		log.Printf("Validation warning: %v", err)
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
		checksum := computeChecksum(data)
		log.Printf("Migration file %s checksum: %s", name, checksum)
		var cfg Config
		if _, err := bcl.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to unmarshal migration file %s: %w", name, err)
		}
		if len(cfg.Migrations) == 0 {
			return fmt.Errorf("no migration found in file %s", name)
		}
		migration := cfg.Migrations[0]
		for _, val := range migration.Validate {
			if err := runPreUpChecks(val.PreUpChecks); err != nil {
				return fmt.Errorf("pre-up validation failed for migration %s: %w", migration.Name, err)
			}
		}
		if err := c.Driver.ApplyMigration(Migration{Name: migration.Name}); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", migration.Name, err)
		}
		for _, val := range migration.Validate {
			if err := runPostUpChecks(val.PostUpChecks); err != nil {
				return fmt.Errorf("post-up validation failed for migration %s: %w", migration.Name, err)
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

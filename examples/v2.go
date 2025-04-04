package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oarkflow/bcl"
)

type MigrationConfig struct {
	Name        string
	Version     string
	Description string
	Up          *MigrationStep
	Down        *MigrationStep
	Validate    *Validation
	Transaction *Transaction
}

type MigrationStep struct {
	Operations []interface{}
}

type Validation struct {
	PreUpChecks  []string
	PostUpChecks []string
}

type Transaction struct {
	Mode           string
	IsolationLevel string
}

// Schema Operations
type CreateSchema struct {
	Name    string
	Comment string
}

type DropSchema struct {
	Name     string
	Cascade  bool
	IfExists bool
}

// Enum Operations
type CreateEnumType struct {
	Name   string
	Values []string
}

type DropEnumType struct {
	Name     string
	IfExists bool
}

// Table Operations
type CreateTable struct {
	Name    string
	Columns map[string]ColumnDefinition
	Indexes []*CreateIndex
}

type ColumnDefinition struct {
	Type       string
	PrimaryKey bool
	Unique     bool
	Nullable   bool
	Default    string
	Size       int
	Check      string
	References string
}

type CreateIndex struct {
	Name    string
	Columns []string
	Where   string
}

type DropTable struct {
	Name    string
	Cascade bool
}

// Table Modification Operations
type AlterTable struct {
	Name         string
	AddColumn    *AddColumn
	DropColumn   *DropColumn
	RenameColumn *RenameColumn
}

type AddColumn struct {
	Name string
	Spec ColumnDefinition
}

type DropColumn struct {
	Name string
}

type RenameColumn struct {
	From string
	To   string
}

// Data Operations
type InsertData struct {
	Table      string
	Columns    []string
	Values     [][]string
	OnConflict *ConflictAction
}

type ConflictAction struct {
	Constraint string
	Do         string
}

type DeleteData struct {
	Table string
	Where string
}

// View Operations
type CreateMaterializedView struct {
	Name    string
	Query   string
	Refresh string
}

type DropMaterializedView struct {
	Name     string
	IfExists bool
}

// Security Operations
type CreateRowPolicy struct {
	Name   string
	Table  string
	Select string
	Using  string
}

type DropRowPolicy struct {
	Name     string
	Table    string
	IfExists bool
}

func main() {
	bcl.RegisterFunction("upper", func(params ...any) (any, error) {
		if len(params) == 0 {
			return nil, errors.New("At least one param required")
		}
		str, ok := params[0].(string)
		if !ok {
			str = fmt.Sprint(params[0])
		}
		return strings.ToUpper(str), nil
	})
	var input = `
Migration "explicit_operations" {
  Version = "1.0.0-beta"
  Description = "Migration with explicit operation labeling"

  Up {

    CreateSchema "core" {
      Comment = "Main application schema"
    }


    CreateEnumType "core.user_role" {
      Values = ["admin", "editor", "guest"]
    }


    CreateTable "core.users" {
      Columns = {
        id = {
          type = "uuid",
          primary_key = true,
          default = "gen_uuid()"
        }
        username = {
          type = "string",
          size = 75,
          unique = true,
          nullable = false
        }
        role = {
          type = "core.user_role",
          default = "guest"
        }
        created_at = {
          type = "timestamp",
          default = "now()"
        }
      }

      CreateIndex "idx_active_users" {
        columns = ["username", "created_at"]
        where = "deleted_at IS NULL"
      }
    }

    CreateTable "core.profiles" {
      Columns = {
        id = {
          type = "uuid",
          primary_key = true
        }
        user_id = {
          type = "uuid",
          references = "core.users(id)",
          unique = true
        }
        bio = {
          type = "text",
          nullable = true
        }
      }
    }


    AlterTable "core.users" {
      AddColumn "email" {
        type = "string"
        size = 255
        check = "email ~* '@'"
      }

      DropColumn "temporary_flag" {}

      RenameColumn {
        from = "signup_date"
        to = "created_at"
      }
    }


    InsertData "core.users" {
      Columns = ["username", "role", "email"]
      Values = [
        ["admin1", "admin", "admin@example.com"],
        ["reviewer1", "editor", "review@org"]
      ]
      OnConflict = {
        constraint = "username_unique"
        do = "NOTHING"
      }
    }


    CreateMaterializedView "core.active_users" {
      Query = "SELECT * FROM core.users WHERE last_login > NOW() - INTERVAL '90 days'"
      Refresh = "CONCURRENTLY"
    }


    CreateRowPolicy "user_access_policy" {
      Table = "core.users"
      Select = "role IN ('admin', 'editor')"
      Using = "active = true"
    }
  }

  Down {

    DropRowPolicy "user_access_policy" {
      Table = "core.users"
      IfExists = true
    }

    DropMaterializedView "core.active_users" {
      IfExists = true
    }

    AlterTable "core.users" {
      AddColumn "temporary_flag" {
        type = "boolean"
        nullable = true
      }

      DropColumn "email" {}

      RenameColumn {
        from = "created_at"
        to = "signup_date"
      }
    }

    DeleteData "core.users" {
      Where = "username LIKE 'admin%'"
    }

    DropTable "core.profiles" {
      Cascade = true
    }

    DropTable "core.users" {
      Cascade = true
    }

    DropEnumType "core.user_role" {
      IfExists = true
    }

    DropSchema "core" {
      Cascade = true
      IfExists = true
    }
  }


  Validate {
    PreUpChecks = [
      "schema_not_exists('core')",
      "table_empty('legacy.users')"
    ]

    PostUpChecks = [
      "index_exists('core.idx_active_users')",
      "fk_exists('core.profiles_user_id_fkey')"
    ]
  }

  Transaction {
    Mode = "atomic"
    IsolationLevel = "read_committed"
  }
}
	`

	var cfg MigrationConfig
	_, err := bcl.Unmarshal([]byte(input), &cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshalled Config:")
	fmt.Printf("%+v\n\n", cfg)

	// str := bcl.MarshalAST(nodes)
	// fmt.Println("Marshaled AST:")
	// fmt.Println(str)

}

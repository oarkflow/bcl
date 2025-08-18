# BCL (Block Configuration Language) API Documentation

BCL is a high-performance configuration language designed as a robust JSON replacement with advanced features like expressions, functions, includes, and concurrent parsing.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Core API](#core-api)
- [Advanced Features](#advanced-features)
- [Performance](#performance)
- [Migration Guide](#migration-guide)

## Installation

```bash
go get github.com/oarkflow/bcl
```

## Quick Start

### Basic Usage

```go
package main

import (
    "fmt"
    "log"
    "github.com/oarkflow/bcl"
)

func main() {
    config := `
    name = "MyApp"
    version = 1.0
    debug = true

    server {
        host = "localhost"
        port = 8080
    }
    `

    var result map[string]any
    _, err := bcl.Unmarshal([]byte(config), &result)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("%+v\n", result)
}
```

### JSON Compatibility

BCL can seamlessly convert to and from JSON:

```go
// Convert BCL to JSON
jsonData, err := bcl.MarshalJSON(result)

// Convert JSON to BCL
var config map[string]any
err = bcl.UnmarshalJSON(jsonData, &config)
```

## Core API

### Parsing Functions

#### `Unmarshal(data []byte, v any) ([]Node, error)`

Parses BCL data and unmarshals it into the provided interface.

```go
var config map[string]any
nodes, err := bcl.Unmarshal([]byte(bclData), &config)
```

#### `Marshal(v any) (string, error)`

Converts a Go value to BCL format.

```go
bclString, err := bcl.Marshal(config)
```

#### `NewParser(input string) *Parser`

Creates a new parser instance for advanced parsing scenarios.

```go
parser := bcl.NewParser(bclString)
nodes, err := parser.Parse()
```

### JSON Functions

#### `MarshalJSON(v any) ([]byte, error)`

Converts BCL data to JSON format, removing BCL-specific metadata.

```go
jsonData, err := bcl.MarshalJSON(config)
```

#### `MarshalJSONIndent(v any, prefix, indent string) ([]byte, error)`

Converts BCL data to indented JSON format.

```go
jsonData, err := bcl.MarshalJSONIndent(config, "", "  ")
```

#### `UnmarshalJSON(data []byte, v any) error`

Parses JSON data into BCL structures.

```go
var config map[string]any
err := bcl.UnmarshalJSON(jsonData, &config)
```

### Environment and Evaluation

#### `NewEnv(parent *Environment) *Environment`

Creates a new environment for variable storage and evaluation.

```go
env := bcl.NewEnv(nil)
env.vars["myVar"] = "value"
```

### Function Registry

#### `RegisterFunction(name string, fn Function) error`

Registers a custom function for use in BCL expressions.

```go
err := bcl.RegisterFunction("double", func(args ...any) (any, error) {
    if len(args) != 1 {
        return nil, errors.New("double requires exactly one argument")
    }

    switch v := args[0].(type) {
    case int:
        return v * 2, nil
    case float64:
        return v * 2, nil
    default:
        return nil, errors.New("double requires a numeric argument")
    }
})
```

#### `LookupFunction(name string) (Function, bool)`

Retrieves a registered function.

```go
fn, exists := bcl.LookupFunction("double")
```

## Advanced Features

### Concurrent Parsing

Parse multiple BCL files concurrently for improved performance:

```go
parser := bcl.NewConcurrentParser(4) // 4 workers
ctx := context.Background()

files := []string{"config1.bcl", "config2.bcl", "config3.bcl"}
results, err := parser.ParseFiles(ctx, files)

// Or parse and merge all results
env, err := parser.ParseAndMerge(ctx, files)
```

### Validation

BCL provides comprehensive validation capabilities:

```go
// Create a validation schema
schema := bcl.NewSchema()
schema.AddRule("server.port", bcl.Required(), bcl.Type("int"), bcl.Range(1, 65535))
schema.AddRule("server.host", bcl.Required(), bcl.Type("string"))
schema.AddRule("debug", bcl.Type("bool"))

// Validate data
err := schema.Validate(config)
if err != nil {
    // Handle validation errors
}
```

#### Built-in Validators

- `Required()` - Ensures value is not nil or empty
- `Type(string)` - Validates value type (string, int, float, bool, array, object)
- `Min(float64)` - Minimum numeric value
- `Max(float64)` - Maximum numeric value
- `Range(min, max float64)` - Numeric range
- `MinLength(int)` - Minimum string/array length
- `MaxLength(int)` - Maximum string/array length
- `Pattern(string)` - Regex pattern matching
- `Enum(...any)` - Value must be one of specified values
- `Custom(func)` - Custom validation function

### Error Handling

BCL provides structured error types for better error handling:

```go
switch err := err.(type) {
case *bcl.ParseError:
    fmt.Printf("Parse error at %s:%d:%d - %s\n",
        err.File, err.Line, err.Column, err.Message)
case *bcl.EvalError:
    fmt.Printf("Evaluation error: %s\n", err.Message)
case *bcl.ValidationError:
    fmt.Printf("Validation error for field %s: %s\n",
        err.Field, err.Message)
case *bcl.MultiError:
    for _, e := range err.Errors {
        fmt.Printf("Error: %v\n", e)
    }
}
```

## BCL Language Features

### Variables and Expressions

```bcl
# Variables
name = "MyApp"
version = 1.0
port = 8080

# Expressions
debug_port = port + 1000
app_info = name + " v" + version

# Environment variables with defaults
db_host = "${env.DB_HOST:localhost}"
db_port = ${env.DB_PORT:5432}
```

### Blocks and Objects

```bcl
# Named block
server "main" {
    host = "localhost"
    port = 8080
    ssl = true
}

# Nested blocks
database {
    primary {
        host = "db1.example.com"
        port = 5432
    }
    replica {
        host = "db2.example.com"
        port = 5432
    }
}

# Objects (maps)
features = {
    auth = true
    api = true
    websocket = false
}
```

### Arrays and Complex Types

```bcl
# Simple array
ports = [8080, 8081, 8082]

# Array of objects
users = [
    { name = "alice", role = "admin" }
    { name = "bob", role = "user" }
]

# Mixed types
config = {
    name = "app"
    ports = [8080, 8081]
    features = {
        ssl = true
        compression = true
    }
}
```

### Functions

```bcl
# Built-in functions
upper_name = upper(name)
lower_env = lower(environment)
full_length = length(users)

# String functions
trimmed = trim("  hello  ")
replaced = replace("hello world", "world", "BCL")
parts = split("a,b,c", ",")
joined = join(parts, "-")

# Date/time functions
current_time = now()
formatted = date_format(current_time, "2006-01-02")

# Conditional expressions
port = debug ? 8081 : 8080
level = env == "prod" ? "error" : "debug"
```

### Control Structures

```bcl
# Conditional blocks
IF (environment == "production") {
    debug = false
    log_level = "error"
} ELSEIF (environment == "staging") {
    debug = true
    log_level = "info"
} ELSE {
    debug = true
    log_level = "debug"
}
```

### Includes

```bcl
# Include local file
@include "database.bcl"

# Include from URL
@include "https://config.example.com/base.bcl"

# Include with variable
db_config = @include "database.bcl"
```

### Special Directives

```bcl
# Execute commands
hostname = @exec(cmd="hostname")
git_hash = @exec(cmd="git", args=["rev-parse", "HEAD"])

# Pipeline for data transformation
@pipeline {
    raw = @exec(cmd="cat", args=["data.json"])
    parsed = parse_json(raw)
    filtered = filter(parsed, "active == true")
}
```

## Performance

BCL is designed for high performance with several optimizations:

### String Builder Pool

BCL uses a pool of string builders to reduce allocations:

```go
// Automatically managed by BCL
sb := getBuilder(capacity)
defer putBuilder(sb)
```

### Concurrent Parsing

Parse multiple files in parallel:

```go
parser := bcl.NewConcurrentParser(runtime.NumCPU())
results, err := parser.ParseFiles(ctx, files)
```

### Include Caching

Files included with `@include` are automatically cached:

```go
// Clear cache if needed
bcl.ClearIncludeCache()
```

### Benchmarks

Run benchmarks to test performance:

```bash
go test -bench=. -benchmem
```

## Migration Guide

### From JSON

```go
// Before (JSON)
var config Config
err := json.Unmarshal(jsonData, &config)

// After (BCL)
var config Config
_, err := bcl.Unmarshal(bclData, &config)

// Or use JSON compatibility
err := bcl.UnmarshalJSON(jsonData, &config)
```

### From HCL

BCL syntax is similar to HCL but with enhancements:

```hcl
# HCL
resource "aws_instance" "example" {
  ami = "ami-123456"
  instance_type = "t2.micro"
}

# BCL equivalent
aws_instance "example" {
  ami = "ami-123456"
  instance_type = "t2.micro"
}
```

### From YAML

```yaml
# YAML
server:
  host: localhost
  port: 8080

# BCL equivalent
server {
  host = "localhost"
  port = 8080
}
```

## Best Practices

1. **Use Type Validation**: Always validate configuration data
   ```go
   schema := bcl.NewSchema()
   schema.AddRule("port", bcl.Type("int"), bcl.Range(1, 65535))
   ```

2. **Handle Errors Properly**: Use structured error types
   ```go
   if err != nil {
       switch e := err.(type) {
       case *bcl.ParseError:
           // Handle parse error
       case *bcl.ValidationError:
           // Handle validation error
       }
   }
   ```

3. **Use Functions for Reusability**: Register custom functions
   ```go
   bcl.RegisterFunction("env_or_default", func(args ...any) (any, error) {
       // Implementation
   })
   ```

4. **Leverage Concurrent Parsing**: For multiple files
   ```go
   parser := bcl.NewConcurrentParser(4)
   env, err := parser.ParseAndMerge(ctx, files)
   ```

5. **Cache Include Files**: Use the built-in caching
   ```bcl
   # This will be cached after first load
   @include "common.bcl"
   ```

## Examples

### Complete Configuration Example

```bcl
# Application configuration
app_name = "ProductionApp"
environment = "${env.APP_ENV:production}"
version = "3.2.1"

# Server configuration
server "api" {
    host = "0.0.0.0"
    port = ${env.PORT:8080}

    ssl {
        enabled = true
        cert = "/etc/ssl/cert.pem"
        key = "/etc/ssl/key.pem"
    }
}

# Database configuration
database "primary" {
    driver = "postgresql"
    host = "${env.DB_HOST:localhost}"
    port = ${env.DB_PORT:5432}

    credentials = @include "credentials.bcl"

    pool {
        min = 5
        max = 20
        idle_timeout = 300
    }
}

# Feature flags
features = {
    new_ui = true
    beta_api = environment != "production"
    debug_mode = environment == "development"
}

# Conditional configuration
IF (environment == "production") {
    log_level = "error"
    cache_ttl = 3600
} ELSE {
    log_level = "debug"
    cache_ttl = 60
}
```

### Go Integration Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/oarkflow/bcl"
)

type Config struct {
    AppName     string `json:"app_name"`
    Environment string `json:"environment"`
    Version     string `json:"version"`
    Server      struct {
        API struct {
            Host string `json:"host"`
            Port int    `json:"port"`
            SSL  struct {
                Enabled bool   `json:"enabled"`
                Cert    string `json:"cert"`
                Key     string `json:"key"`
            } `json:"ssl"`
        } `json:"api"`
    } `json:"server"`
    Features map[string]bool `json:"features"`
}

func main() {
    // Register custom functions
    bcl.RegisterFunction("is_prod", func(args ...any) (any, error) {
        if len(args) != 1 {
            return false, nil
        }
        env, ok := args[0].(string)
        return ok && env == "production", nil
    })

    // Parse configuration
    data, err := os.ReadFile("config.bcl")
    if err != nil {
        log.Fatal(err)
    }

    var config Config
    _, err = bcl.Unmarshal(data, &config)
    if err != nil {
        log.Fatal(err)
    }

    // Validate configuration
    schema := bcl.NewSchema()
    schema.AddRule("app_name", bcl.Required(), bcl.Type("string"))
    schema.AddRule("server.api.port", bcl.Required(), bcl.Type("int"), bcl.Range(1, 65535))

    if err := schema.Validate(config); err != nil {
        log.Fatal("Validation failed:", err)
    }

    // Convert to JSON if needed
    jsonData, err := bcl.MarshalJSON(config)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Configuration loaded: %s\n", jsonData)
}
```

## Contributing

Contributions are welcome! Please see the [GitHub repository](https://github.com/oarkflow/bcl) for more information.

## License

BCL is released under the MIT License. See LICENSE file for details.

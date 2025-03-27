# BCL

BCL is a lightweight configuration language parser designed for ease-of-use and flexibility in configuration management.


## Usage Example

Create a configuration file (e.g., `config.bcl`) with content like:
```bcl
appName = "Boilerplate"
version = 1.2

@include "credentials.bcl"

server main {
    host   = "localhost"
    port   = 8080
    secure = false
}

greeting = "Welcome to ${upper(appName)}"
calc     = 10 + 5
negValue = -calc

IF (settings.debug) {
    logLevel = "verbose"
} ELSE {
    logLevel = "normal"
}
```

Then run an example provided in the repository:
```bash
go run examples/main.go
```
The tool will read the configuration, evaluate expressions, perform dynamic interpolation, and output the processed configuration.

### Complete Usage Example

Create a configuration file (e.g., `config.bcl`) with:
```bcl
appName = "Boilerplate"
version = 1.2
@include "credentials.bcl"

server main {
    host   = "localhost"
    port   = 8080
    secure = false
}

greeting = "Welcome to ${upper(appName)}"
calc     = 10 + 5

IF (settings.debug) {
    logLevel = "verbose"
} ELSE {
    logLevel = "normal"
}
```
Then run:
```bash
go run examples/main.go
```
Expected output includes the unmarshaled configuration and the AST.

## Features

- **Dynamic Expression Evaluation:** Supports inline expressions with interpolation syntax `${...}`.
- **Function Support:** Register custom functions (e.g., `upper`) that can be used in expressions.
- **Unary and Binary Operators:** Handles arithmetic, relational, and unary operators (like `-` and `!`).
- **Block and Map Structures:** Easily define groups of configuration parameters using blocks or maps.
- **Environment Variable Lookup:** Lookup system environment variables with syntax like `${env.VAR_NAME}`.
- **Include Directive:** Incorporates external configuration files or remote resources using the `@include` keyword.
- **Control Structures:** Basic support for control statements like `IF`, `ELSEIF`, and `ELSE` to drive conditional configuration.

### Complete Feature Examples

- **Dynamic Expression Evaluation:**
```bcl
text = "Current version: ${version}"
```
- **Function Support:**
```go
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
```
- **Unary and Binary Operators:**
```bcl
negNumber = -10
notTrue   = !false
calc      = 20 + 5 * 2
```
- **Block Structures:**
```bcl
database db1 {
    host     = "127.0.0.1"
    port     = 5432
    username = "user"
    password = "pass"
}
```
- **Map Structures:**
```bcl
settings = {
    debug     = true
    timeout   = 30
    rateLimit = 100
}
```
- **Slice Examples (Primitive Types):**
```bcl
fruits = ["apple", "banana", "cherry"]
numbers = [1, 2, 3, 4, 5]
```
- **Slice Examples (Objects):**
```bcl
users = [
    {
        name = "Alice"
        age  = 30
    }
    {
        name = "Bob"
        age  = 25
    }
]
```
- **Environment Variable Lookup:**
```bcl
defaultShell = "${env.SHELL:/bin/bash}"
```
- **Include Directive:**
```bcl
@include "other_config.bcl"
```
- **Control Structures:**
```bcl
IF (settings.debug) {
    logLevel = "verbose"
} ELSE {
    logLevel = "normal"
}
```

## Advanced Examples

### External File Inclusion via URL

Include configuration from a remote URL:
```bcl
@include "https://raw.githubusercontent.com/oarkflow/proj/config.bcl"
```

### Multi-line String using Heredoc

Define a multi-line string with preserved line breaks:
```bcl
description = <<EOF
This is a multi-line string.
It can include several lines,
and even special characters like # without being treated as a comment.
EOF
```

### Comment Usage

Comments are supported and can be added using the hash symbol:
```bcl
# This is a comment and will be ignored by the parser.
appName = "SampleApp"
```

## All-In-One Example

Below is a comprehensive configuration example that demonstrates maps, slices, blocks, external file inclusion, multi-line strings, dynamic expressions, environment variable lookup, function calls, and control structures:

```bcl
appName = "Boilerplate"
version = 1.2
@include "credentials.bcl"
@include "https://raw.githubusercontent.com/oarkflow/proj/config.bcl"

# Block example for server configuration
server main {
    host   = "localhost"
    port   = 8080
    secure = false
}

# Block example for database configuration
database db1 {
    host     = "127.0.0.1"
    port     = 5432
    username = "user"
    password = "pass"
}

# Map example for settings (note: maps are different from blocks)
settings = {
    debug     = true
    timeout   = 30
    rateLimit = 100
}

# Slice example for users list
users = ["alice", "bob", "charlie"]

# Multi-line string using heredoc
description = <<EOF
This configuration demonstrates:
- External file inclusion via URL
- Blocks vs maps vs slices
- Dynamic expression evaluation
- Function calls and env variable lookup
EOF

# Dynamic expression using a registered function and env lookup
greeting = "Welcome to ${upper(appName)}"
envInfo  = "Home directory: ${env.HOME}"

# Control structure example based on settings
IF (settings.debug) {
    logLevel = "verbose"
} ELSE {
    logLevel = "normal"
}
```

## Unmarshal & MarshalAST Examples

### Unmarshal from a String
```go
import (
    "fmt"
    "github.com/oarkflow/bcl"
)

func exampleUnmarshalString() {
    configStr := `
appName = "Boilerplate"
version = 1.2
greeting = "Welcome to ${upper(appName)}"
`
    var cfg map[string]any
    nodes, err := bcl.Unmarshal([]byte(configStr), &cfg)
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Println("Unmarshaled Config:", cfg)
    fmt.Println("Marshaled AST:")
    fmt.Println(bcl.MarshalAST(nodes))
}
```

### Unmarshal from a File
```go
import (
    "fmt"
    "os"
    "github.com/oarkflow/bcl"
)

func exampleUnmarshalFile() {
    data, err := os.ReadFile("config.bcl")
    if err != nil {
        fmt.Println("Error reading file:", err)
        return
    }
    var cfg map[string]any
    nodes, err := bcl.Unmarshal(data, &cfg)
    if err != nil {
        fmt.Println("Error unmarshaling:", err)
        return
    }
    fmt.Println("Unmarshaled Config from file:", cfg)
    fmt.Println("Marshaled AST:")
    fmt.Println(bcl.MarshalAST(nodes))
}
```

For more details, please review the source code and usage examples provided in the repository.

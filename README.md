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
    // ...existing function implementation...
})
```
- **Unary and Binary Operators:**
```bcl
negNumber = -10
notTrue   = !false
calc      = 20 + 5 * 2
```
- **Block and Map Structures:**
```bcl
database db1 {
    host     = "127.0.0.1"
    port     = 5432
    username = "user"
    password = "pass"
}
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

For more details, please review the source code and usage examples provided in the repository.

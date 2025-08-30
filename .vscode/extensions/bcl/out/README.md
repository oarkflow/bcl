# BCL Language Support

This extension provides comprehensive language support for BCL (Block Configuration Language) files in Visual Studio Code.

## Features

- Syntax highlighting for BCL files
- Error checking and diagnostics
- Code completion for keywords and built-in constructs
- Hover information for BCL constructs
- Go to definition support
- Find all references support

## Syntax Highlighting

The extension provides syntax highlighting for all BCL language constructs:
- Comments (`#`)
- Keywords (`true`, `false`, `IF`, `ELSE`, `@include`)
- Strings, numbers, and identifiers
- Blocks and maps
- Operators and assignments

## Error Checking

The extension provides real-time error checking for BCL files, highlighting potential issues in your configuration.

## Code Completion

Intelligent code completion is available for BCL keywords and constructs:
- `true` and `false` boolean values
- Control structures (`IF`, `ELSE`)
- Include directives (`@include`)

## Hover Information

Hover over BCL constructs to get contextual information about the language features.

## Go to Definition

Navigate to the definition of identifiers with the "Go to Definition" command.
Supports navigation through dot notation paths - for example, clicking on `myservice-prod2` in `tunnel.myservice-prod2.mapping.user_id` will navigate to the `tunnel "myservice-prod2"` block definition.

## Find All References

Find all references to an identifier with the "Find All References" command.
References are found even when the identifier appears as part of a dot notation path.

## Requirements

- Visual Studio Code 1.98.0 or later

## Extension Settings

This extension contributes the following settings:

- `bcl.maxNumberOfProblems`: Controls the maximum number of problems produced by the server (default: 100)
- `bcl.trace.server`: Traces the communication between VS Code and the language server (default: "off")

## Known Issues

This is an initial implementation. More advanced features will be added in future releases.

## Release Notes

### 0.0.6

- **CRITICAL FIX**: Added nested property parsing to symbol table
- Fixed "Go to Definition" for nested block properties like `config` in `tunnel."test-service".config.api_key`
- Enhanced symbol table to include nested properties within blocks
- Added recursive parsing of nested blocks and their properties
- Now supports navigation to any level of nesting in dot notation paths

### 0.0.5

- **CRITICAL FIX**: Fixed word pattern in language configuration to properly separate dot notation
- Fixed dot notation navigation - each component separated by dots is now treated as individual words
- Fixed issue where clicking on dot notation went to first character instead of definition
- Enhanced word boundary detection for proper navigation in expressions like `tunnel.myservice-prod2.mapping.user_id`
- Each token in dot notation (`tunnel`, `myservice-prod2`, `mapping`, `user_id`) is now individually navigable

### 0.0.4

- Fixed dot notation navigation to properly resolve individual components
- Improved word separation for dot notation (mapping.user_id now treated as mapping and user_id)
- Enhanced definition provider to navigate to correct definitions in dot notation paths
- Fixed issue where navigation was going to first character of same line instead of definition

### 0.0.3

- Fixed hyphenated identifier navigation in dot notation
- Improved word pattern handling for identifiers with hyphens
- Enhanced syntax highlighting for hyphenated identifiers
- Fixed word separation issues with hyphens in identifiers

### 0.0.2

- Enhanced dot notation support
- Improved Go to definition for dot notation paths
- Better handling of quoted block names in navigation
- Enhanced Find all references to include dot notation occurrences

### 0.0.1

Initial release of BCL Language Support with basic language server features.

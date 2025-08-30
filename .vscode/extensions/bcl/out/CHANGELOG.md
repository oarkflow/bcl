# Change Log

All notable changes to the "bcl" extension will be documented in this file.

## [0.0.6] - 2025-08-29

- CRITICAL FIX: Added nested property parsing to symbol table
- Fixed "Go to Definition" for nested block properties like `config` in `tunnel."test-service".config.api_key`
- Enhanced symbol table to include nested properties within blocks
- Added recursive parsing of nested blocks and their properties
- Now supports navigation to any level of nesting in dot notation paths

## [0.0.5] - 2025-08-29

- CRITICAL FIX: Fixed word pattern in language configuration to properly separate dot notation
- Fixed dot notation navigation - each component separated by dots is now treated as individual words
- Fixed issue where clicking on dot notation went to first character instead of definition
- Enhanced word boundary detection for proper navigation in expressions like tunnel.myservice-prod2.mapping.user_id
- Each token in dot notation (tunnel, myservice-prod2, mapping, user_id) is now individually navigable

## [0.0.4] - 2025-08-29

- Fixed dot notation navigation to properly resolve individual components
- Improved word separation for dot notation (mapping.user_id now treated as mapping and user_id)
- Enhanced definition provider to navigate to correct definitions in dot notation paths
- Fixed issue where navigation was going to first character of same line instead of definition

## [0.0.3] - 2025-08-29

- Fixed hyphenated identifier navigation in dot notation
- Improved word pattern handling for identifiers with hyphens
- Enhanced syntax highlighting for hyphenated identifiers
- Fixed word separation issues with hyphens in identifiers

## [0.0.2] - 2025-08-29

- Enhanced dot notation support
- Improved Go to definition for dot notation paths
- Better handling of quoted block names in navigation
- Enhanced Find all references to include dot notation occurrences

## [0.0.1] - 2025-08-29

- Initial release
- Syntax highlighting for BCL files
- Basic language server implementation
- Error checking and diagnostics
- Code completion for keywords
- Hover information
- Go to definition support
- Find all references support

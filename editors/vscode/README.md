# BCL Language Support

VS Code extension for `.bcl` files. It provides TextMate highlighting, snippets, formatting, diagnostics, IntelliSense, hover, goto definition, references, rename, symbols, semantic tokens, and recent symbol history through `bcl-lsp`.

During development, run `npm install` and `npm run compile` in this directory. The extension looks for a bundled `bin/<platform>-<arch>/bcl-lsp` binary, then `bcl.languageServer.path`, then falls back to `go run ./cmd/bcl-lsp` from the workspace.

# BCL Language Support

VS Code extension for `.bcl` and `.schema` files. It provides TextMate highlighting, snippets, diagnostics, navigation, IntelliSense, rich hover, goto definition, references, rename, symbols, semantic tokens, and recent symbol history through `bcl-lsp`.

## Formatting

`Format Document` (`shift+alt+F`), `Format Selection`, and `editor.formatOnSave` all run the BCL formatter in `bcl-lsp`:

- Blocks are re-indented by nesting depth, spacing between tokens is normalized, runs of blank lines collapse to one, and trailing whitespace goes away.
- Comments, declaration order, and every token's original text are preserved, so formatting never changes what a file means.
- Files the validator still rejects (a duplicate block, an unknown field) format anyway; only a lexical error such as an unterminated string stops it.
- The editor's own `editor.tabSize` and `editor.insertSpaces` settings decide the indentation, and CRLF files stay CRLF.

A `.bclfmt` file at the project root fixes the style for everyone working in it
(`indent`, `tabs`, `keep_blank_lines`) and takes precedence over your own editor
settings.

Typing follows the same structure: a line left open by `{`, `[`, or `(` indents the next line, and a closing bracket outdents itself.

The same formatter is available on the command line as `bcl fmt` (`-w` to write, `-l` to list files that differ, `-indent`/`-tabs` for the indentation style, `-canonical` for the AST-driven rewrite that sorts declarations and drops comments).

## Diagnostics and quick fixes

Diagnostics carry a stable code (`BCL0100`), shown in the Problems panel and used
to offer fixes: remove an unused declaration, rename a duplicate, insert a missing
required field or module input, create a missing reference, add a `bcl` version
block, add a catch-all `case`, or add `replaced_by` to a deprecated declaration.

Blocks fold by real structure rather than indentation, and `import` paths are
clickable.

Condition authoring is supported directly:

- Snippets and highlighting for `routes`, `route`, `lifecycle`, `phase`, `chain`, `watch`, `step`, `policy_package`, `policy_overlay`, `action_catalog`, `output_contract`, `standard_facts`, `response_classifier`, and `lifecycle_test`, including request/response headers, body, and format envelopes.
- Rich hover for route normalization, lifecycle phase flow, chain/watch behavior, overlays, and action delivery safety.
- Commands for `Condition: Route Coverage`, `Condition: Lifecycle Playground`, and `Condition: Compact State`.
- Example openers for the request lifecycle and HTTP auth guard examples.

Condition commands call a running Condition HTTP server. Configure:

- `bcl.conditionServer.url`, default `http://127.0.0.1:8080`
- `bcl.conditionServer.tenant`, default `default`
- `bcl.condition.defaultLifecycle`, default `http_request`

During development, run `npm install` and `npm run compile` in this directory. The extension looks for a bundled `bin/<platform>-<arch>/bcl-lsp` binary, then `bcl.languageServer.path`, then falls back to `go run ./cmd/bcl-lsp` from the workspace.

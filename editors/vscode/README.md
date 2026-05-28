# BCL Language Support

VS Code extension for `.bcl` and `.schema` files. It provides TextMate highlighting, snippets, diagnostics, navigation, IntelliSense, rich hover, goto definition, references, rename, symbols, semantic tokens, and recent symbol history through `bcl-lsp`.

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

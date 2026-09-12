# BCL

A configuration language for Go, and a decision engine that runs on it.

BCL reads like a config file and behaves like a small typed language: blocks and
assignments, schemas that validate them, expressions and pattern matching,
modules with a lockfile, and profiles that layer environment-specific overrides.
On top of that sits a decision engine — decision tables, rule sets, bundles,
datasets and gates — for policy that belongs in a reviewable file rather than in
code.

It has one third-party dependency, no cgo, and ships a language server and a
VS Code extension.

```bcl
# A comment, preserved by the formatter.
server {
  address ":8080"
  workers 4
}

schema server {
  required address string
  optional workers int default 1 min 1 max 64
}
```

## Install

```sh
go get github.com/oarkflow/bcl          # library
go install github.com/oarkflow/bcl/cmd/bcl@latest      # CLI
go install github.com/oarkflow/bcl/cmd/bcl-lsp@latest  # language server
```

## Using it from Go

Decode into a struct, the way you would with `encoding/json`:

```go
type Server struct {
    Address string `bcl:"address"`
    Workers int    `bcl:"workers"`
}

var cfg struct {
    Server Server `bcl:"server"`
}
if err := bcl.Unmarshal(src, &cfg); err != nil {
    return err
}
```

Or compile to a normalized document when you want the diagnostics, the block
index, and the schema results:

```go
normalized, err := bcl.CompileBytes(src, &bcl.Options{
    Strict:         true,
    ResolveImports: true,
    BaseDir:        dir,
})
```

`Options` is also the capability model: env access, wall-clock time, hashing,
encoding, filesystem and network reach are each off or bounded by default. See
[SECURITY.md](SECURITY.md) before compiling a document you did not write.

## The CLI

| Command | What it does |
| --- | --- |
| `bcl fmt [-w] [-l] [-indent n] [-tabs] [path…]` | Format files or a whole directory. `-l` lists what differs; `-canonical` rewrites from the AST instead |
| `bcl lint <file>` | Diagnostics only |
| `bcl validate [-strict] <file\|dir>` | Validation, including schemas and cross-references |
| `bcl compile [-out x.json] <file\|dir>` | Normalized JSON |
| `bcl explain [-input in.json] <file>` | Why the document resolves the way it does |
| `bcl simulate -input in.json <file>` | Evaluate against sample input |
| `bcl test [-json] <file>` | Run `test` blocks |
| `bcl export [-format json\|yaml] [-fields …] [-redact …] <file>` | Export with field selection and redaction |
| `bcl codegen [-package p] <file>` | Generate Go types |
| `bcl docs <file>` | Generate documentation |
| `bcl domain <dir>` | Compile a directory as one domain |
| `bcl migrate [-version 1.0] <file>` | Rewrite a document for a target BCL version |
| `bcl modules lock\|fetch\|verify` | Module lockfile operations |

## Editor support

The VS Code extension lives in [`editors/vscode`](editors/vscode): diagnostics,
completion, hover, go-to-definition, references, rename, symbols, semantic
tokens, formatting (document, selection, and whole folders), and code actions.

```sh
make install-extension     # build the server and CLI, link the extension, reload
```

It works in any window, not only a checkout of this repository: the language
server and CLI binaries are bundled with the extension. Any editor that speaks
LSP can run `bcl-lsp` directly.

## Formatting

`bcl fmt` re-indents by real bracket depth, normalizes spacing, and collapses
runs of blank lines. It preserves every comment, every declaration's order, and
every token's text, so formatting never changes what a document means — a
property checked by a fuzz target and by a repository-wide test that compares the
canonical form before and after. It also formats files the validator still
rejects, which is what an editor needs.

A project can fix its own style in a `.bclfmt` file, written in BCL and found by
walking up from the file being formatted (stopping at the repository root):

```bcl
indent 4
tabs false
keep_blank_lines false
```

It beats the editor's own tab settings, because formatting is shared output. A
`-indent` or `-tabs` flag you type beats both.

## Diagnostics

Every diagnostic carries a stable code — `error[BCL0100]: duplicate block a.x` —
so tooling can suppress, explain, or fix a class of problem without matching on
message text. Codes are grouped by the stage that reports them (parsing,
references, schemas, expressions, capabilities, modules, decisions, hygiene) and
listed in [`codes.go`](codes.go).

## Limits

Parsing recurses once per nesting level, so documents nested deeper than
`bcl.MaxNestingDepth` (512) are refused with a diagnostic. Without that bound a
large enough file exhausts the goroutine stack and ends the process.

## Development

```sh
go test ./...                                                   # suite
go test -race ./...                                             # concurrency
go test -run XXX -fuzz FuzzParseFormatCompile -fuzztime 60s .    # fuzzing
go test . ./cmd/bcl-lsp -run XXX -bench . -benchtime 100x        # benchmarks
go run ./cmd/bcl fmt -w .                                        # format BCL sources
```

CI runs all of this on Linux, macOS and Windows; see
[`.github/workflows/ci.yml`](.github/workflows/ci.yml).

# BCL Benchmark Notes

Benchmarks live in [benchmark_test.go](benchmark_test.go).

Command used:

```bash
GOCACHE=/private/tmp/bcl-gocache GOMODCACHE=/private/tmp/bcl-gomodcache go test -run '^$' -bench . -benchmem -count=3
```

Or run the repository helper:

```bash
make bench
```

Machine:

```text
goos: darwin
goarch: arm64
cpu: Apple M2 Pro
```

## Current Highlights

### Text codec comparison

`BenchmarkCodecComparison` encodes the same three-field struct with BCL and
`encoding/json`. On Apple M2 Pro with Go 1.27.0 (`-benchtime=750ms`,
two runs, representative result):

| Codec | Wire bytes | Marshal ns/op | Marshal B/op | Unmarshal ns/op | Unmarshal B/op |
|---|---:|---:|---:|---:|---:|
| BCL `Marshal` / `Unmarshal` | 42 | 135 | 96 | 150 | 48 |
| BCL `Marshal` / `UnmarshalSimple` | 42 | 135 | 96 | 140 | 16 |
| JSON | 49 | 211 | 128 | 282 | 32 |

This is one small payload, not a general ranking. The fast marshal path writes
plain scalar structs directly; plain `Unmarshal` handles simple top-level
literal assignments to built-in struct fields and falls back to the compiler.
`UnmarshalSimple` accepts only that narrow data subset and returns
`ErrSimpleUnmarshalUnsupported` before changing the destination otherwise.
That strict API uses less decode memory than JSON here (16 vs 32 B/op), while
general `Unmarshal` uses more (48 B/op). `UnmarshalWithOptions` always uses the
compiler. gRPC is a transport; a comparison with it must specify the message
codec (usually Protocol Buffers), framing, and compression. A compact data codec
would need its own schema and wire format while the existing
BCL syntax remains useful for human-authored configuration and policy.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Parse small | ~2,774 | 2,129 | 25 |
| Parse policy | ~23,858 | 27,842 | 144 |
| Scan small | ~1,073 | 0 | 0 |
| Scan policy | ~9,930 | 0 | 0 |
| Compile policy | ~36,271 | 41,772 | 322 |
| Compile already-parsed policy | ~9,868 | 13,545 | 178 |
| Validate policy | ~11,413 | 11,358 | 115 |
| Format policy | ~31,969 | 31,178 | 259 |
| Eval expression | ~261 | 104 | 5 |
| Eval condition | ~453 | 16 | 1 |
| Marshal struct | ~190 | 80 | 1 |
| Unmarshal struct | ~156 | 48 | 2 |
| Evaluate decision table | ~1,445 | 1,544 | 15 |
| Evaluate decision table (into) | ~1,330 | 984 | 13 |
| Compile `example/main.bcl` | ~487,393 | 336 KB | 2,775 |
| Compile detailed example | ~494,324 | 341 KB | 2,818 |
| Normalized JSON export | ~17,406 | 11,611 | 139 |

## Optimization Notes

- All scalar type conversions use `github.com/oarkflow/convert` for allocation-free paths.
- `writeScalar` uses `convert.AppendString` with a stack-allocated buffer, replacing `fmt.Fprintf`.
- `assignGoValue` uses `convert.ToString`/`ToInt64`/`ToFloat64`/`ToBool`/`ToUint64` instead of `fmt.Sprint` and manual type switches.
- Expression builtins (`toInt`, `toFloat`, `toBool`) delegate to `convert` package.
- `parseInlineNumber` uses `convert.ToInt64`/`ToFloat64` instead of `fmt.Sscan`.
- `parseCSVScalar` and `parseYAMLScalar` use `convert` for int/float/bool parsing.
- Numeric literal parsing in parser uses `convert` instead of `strconv.ParseInt`/`ParseFloat`.
- `env.int`/`env.float`/`env.bool` compiler builtins use `convert`.
- `numericInt`/`numericFloat` wrappers delegate to `convert.ToInt64`/`ToFloat64`.
- Lexer slices identifiers/numbers/plain strings from the source string instead of building them rune by rune.
- Token storage uses adaptive preallocation and a cleared scratch pool so parse/compile do not allocate a fresh token backing array on every call.
- Expression nodes preserve raw source slices instead of first building temporary token-text slices.
- `Scan`/`ScanFile`/`ScanString` provide a zero-allocation syntax gate for hot paths that do not need a materialized AST.
- Expressions compile into cached bytecode programs and run through a small stack VM.
- Common boolean predicates use a typed fast path and avoid the VM stack entirely.
- Expression references are compiled into path parts and evaluated without allocating joined path strings for normal `subject.roles` style paths.
- `has_any` and `has_all` avoid reflect slice conversion allocations and simple string equality avoids `reflect.DeepEqual`/`fmt.Sprint`.
- Validation compiles expressions without evaluating them.
- `Unmarshal` decodes simple literal structs directly and uses the compiler for richer BCL. The compiler path assigns from normalized maps without a JSON round trip.
- Normalization uses sized maps for known block/object shapes.
- Formatter grows its output buffer from input size.
- `isEmpty` uses typed type switches instead of `fmt.Sprint(v) == ""`.
- `compare` converts both sides to string via type assertion before falling back to `fmt.Sprint`.
- `equalLoose` uses typed type switches for string/int/int64/float64/float32/bool/nil instead of `reflect.DeepEqual`/`fmt.Sprint` fallback.
- `setNormalized` replaces `strings.Split` with a single-pass `strings.IndexByte` loop to avoid the `[]string` allocation.
- `collectRef`, `parseSpreadTarget`, `parseDottedAssignment`, `parseSchemaFieldName`, `parseSchemaType` replace `[]string`+`strings.Join` with `strings.Builder`.
- `contains`/`containsValue` add fast string-vs-string paths to avoid `fmt.Sprint` allocation.
- `sliceValues` uses index-assignment instead of `append` for pre-allocated slices.
- `unique` uses a custom `typeAndString` key builder with `map[string]struct{}` instead of `fmt.Sprintf("%T:%v")` with `map[string]bool`.
- Expression VM stack is pre-allocated with known program length.
- `EvalOptions` is reused across `valueWithRedact`, `interpolate`, and decision `call` evaluation by embedding it in the `compiler` struct.

## Remaining Performance Work

- Full parse/compile will continue to allocate while it returns a rich AST and normalized map model; pushing it further needs arena-like reuse or a lower-level streaming API.
- Expression VM `sliceValues` still allocates when converting `[]string` to `[]any` for runtime use — a typed VM stack would eliminate this.
- `evalVars` creates a new `map[string]any` on every call; caching or reuse would reduce compile-time allocations.
- `EvalOptions` is still heap-allocated via `&c.evalOpts` escape; a value receiver or passing by value could eliminate the pointer allocation.
- Lexer token pooling could be refined for zero-alloc scan paths in hot parse loops.
- AST node pooling would reduce GC pressure for repeated parse/compile cycles.

# BCL Benchmark Notes

Benchmarks live in [benchmark_test.go](/Users/sujit/Sites/bcl-v2/benchmark_test.go).

Command used:

```bash
GOCACHE=/private/tmp/bcl-gocache GOMODCACHE=/private/tmp/bcl-gomodcache go test -run '^$' -bench . -benchmem
```

Machine:

```text
goos: darwin
goarch: arm64
cpu: Apple M2 Pro
```

## Current Highlights

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Parse small | ~2,347 | 2,096 | 24 |
| Parse policy | ~18,458 | 14,768 | 146 |
| Scan small | ~1,038 | 0 | 0 |
| Scan policy | ~9,795 | 0 | 0 |
| Compile policy | ~26,405 | 27,158 | 315 |
| Compile already-parsed policy | ~5,991 | 12,152 | 165 |
| Validate policy | ~2,368 | 539 | 12 |
| Format policy | ~29,309 | 40,284 | 266 |
| Eval expression | ~45.70 | 0 | 0 |
| Eval condition | ~196.4 | 0 | 0 |
| Marshal struct | ~912.9 | 416 | 13 |
| Unmarshal struct | ~2,269 | 4,483 | 34 |
| Compile `example/main.bcl` | ~376,063 | 399 KB | 2,500 |
| Compile detailed example | ~365,449 | 403 KB | 2,543 |
| Normalized JSON export | ~16,587 | 11,612 | 139 |

## Improvements From Baseline

| Benchmark | Baseline | Current |
|---|---:|---:|
| Parse small allocs | 49 | 24 |
| Parse policy bytes | 65,624 | 14,785 |
| Parse policy allocs | 312 | 150 |
| Compile policy bytes | 78,430 | 27,158 |
| Compile policy allocs | 477 | 315 |
| Validate policy bytes | 10,141 | 539 |
| Validate policy allocs | 99 | 12 |
| Format policy bytes | 69,171 | 40,283 |
| Format policy allocs | 398 | 266 |
| Eval expression | ~1,720 ns / 3,313 B / 25 allocs | ~45.66 ns / 0 B / 0 allocs |
| Eval condition | ~3,445 ns / 4,882 B / 48 allocs | ~195.9 ns / 0 B / 0 allocs |
| Unmarshal struct | ~3,560 ns / 5,703 B / 55 allocs | ~2,269 ns / 4,483 B / 34 allocs |
| Compile `example/main.bcl` | ~429,000 ns / 580 KB / 3,553 allocs | ~376,063 ns / 399 KB / 2,500 allocs |

## Optimization Notes

- Lexer now slices identifiers/numbers/plain strings from the source string instead of building them rune by rune.
- Token storage uses adaptive preallocation and a cleared scratch pool so parse/compile do not allocate a fresh token backing array on every call.
- Expression nodes preserve raw source slices instead of first building temporary token-text slices.
- `Scan`/`ScanFile`/`ScanString` provide a zero-allocation syntax gate for hot paths that do not need a materialized AST.
- Expressions compile into cached bytecode programs and run through a small stack VM.
- Common boolean predicates use a typed fast path and avoid the VM stack entirely.
- Expression references are compiled into path parts and evaluated without allocating joined path strings for normal `subject.roles` style paths.
- `has_any` and `has_all` avoid reflect slice conversion allocations and simple string equality avoids `reflect.DeepEqual`/`fmt.Sprint`.
- Validation compiles expressions without evaluating them.
- `Unmarshal` decodes directly from normalized maps into Go values instead of JSON round-tripping.
- Normalization uses sized maps for known block/object shapes.
- Formatter grows its output buffer from input size.

## Remaining Performance Work

- Repeated `Eval` and condition evaluation are zero-allocation for the benchmarked common predicate shapes.
- Full parse/compile will continue to allocate while it returns a rich AST and normalized map model; pushing it further needs arena-like reuse or a lower-level streaming API.

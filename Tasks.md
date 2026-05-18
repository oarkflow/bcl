# BCL Implementation Tasks

Last updated: 2026-05-17

## Completed

- [x] Generic HCL-like parser for assignments, maps, lists, nested blocks, block IDs, compact syntax, comments, strings, multiline strings, and heredocs.
- [x] Primitive value support for strings, ints, floats, booleans, null, durations, bytes, dates, datetimes, identifiers, and typed constructor calls such as `regex`, `cidr`, `url`, `email`, `ip`, and `time`.
- [x] Inline lists, multiline block lists, maps, nested maps, object arrays, and repeated-field normalization.
- [x] Generic block model for `tenant`, `policy`, `role`, `pipeline`, `route`, `rule`, `http`, `connector`, `source`, `action`, `file`, `command`, and arbitrary future block types.
- [x] Environment functions: `env`, `env.required`, `env.int`, `env.bool`, `env.float`, `env.duration`, `env.bytes`, and `env.list`.
- [x] Constants, reusable sets, references, set calls, and basic reference validation.
- [x] `when` condition parsing with `all`, `any`, `not`, `none`, expression leaves, and condition evaluation.
- [x] Expression evaluator for comparison, membership, string matching, `between`, `exists`, `empty`, boolean composition, regex, CIDR membership, string functions, length, contains, JSON/base64/sha256 capability-gated functions, and time capability checks.
- [x] String and heredoc interpolation using `${...}` expressions, with an option to disable interpolation.
- [x] Import support for local files and deterministic local globs, with named import namespaces.
- [x] Module support for local files/directories, module namespaces, module inputs, and lockfile entries.
- [x] Lockfile read/write plus checksum enforcement for local imports/modules.
- [x] Remote Git/registry sources are represented and require lockfile entries; network fetching is intentionally not performed.
- [x] Profiles, `active_profile`, top-level overrides, block overrides, and profile-specific overrides.
- [x] Schema definitions with required/optional fields, defaults, enums, lists, maps, objects, tuples, and basic alias-aware validation.
- [x] Type aliases with `type Name = target`.
- [x] Strict mode from `bcl { strict true }` and CLI `--strict`, including unknown schema fields and stricter module checks.
- [x] Validation for duplicate IDs, duplicate constants/sets/schemas, unknown references, reference cycles, unused constants/sets, deprecated blocks, workflow entrypoints, invalid workflow connections, unreachable workflow steps, and integration block shape.
- [x] Integration modeling for `runtime`, `capabilities`, `phase`, `http`, `connector`, `source`, `action`, `file`, `command`, and `output` without executing side effects.
- [x] HTTP modeling validation for base URLs, denied localhost/metadata hosts, auth types, required auth fields, request methods, request body types, and response status shape.
- [x] Sensitive value redaction through `sensitive(...)`, sensitive path collection, and redacted normalized output.
- [x] Normalized JSON-compatible AST with source spans, diagnostics, dependencies, capabilities, sensitive paths, lockfile metadata, and explain trace via `CompileResult`.
- [x] Go APIs: `Parse`, `ParseFile`, `ParsePath`, `LoadFile`, `Compile`, `CompileFile`, `CompileDetailed`, `CompileFileWithLock`, `Validate`, `ValidateFile`, `Lint`, `Format`, `Explain`, `ExplainFile`, `Simulate`, `SimulateFile`, `Watch`, `WatchFiles`, `Marshal`, `Unmarshal`, `NewEncoder`, and `NewDecoder`.
- [x] CLI commands: `fmt`, `lint`, `validate --strict`, `compile --profile --allow-env --lockfile`, `explain --input`, `simulate --input`, and `modules lock`.
- [x] Example suite for app config, policies, workflows, routing modules, risk modules, integrations, profiles, and a runnable Go example.
- [x] Tests for parser/compiler basics, advanced conditions, imports/modules/profiles/overrides, schema/reference/cycle checks, examples, lockfile enforcement, integration validation, simulation, and CLI smoke coverage.

## Completed From Original Remaining List

- [x] Remote Git module fetch modeling with pinned revisions, cache directory management, checksum verification, and no-network/offline mode.
- [x] Registry module fetch modeling with registry metadata, version constraints, checksums, and provenance.
- [x] Full semantic validation for evaluation strategies, including conflicting allow/deny priorities across namespaces and shadowed policies/rules.
- [x] Rich policy/rule simulation reports with per-expression values, short-circuit details, final strategy reasoning, and matched priority ordering.
- [x] Full HTTP response mapping semantics for `capture body.id as id`, `map {}`, `on_status`, retry decisions, default values, and error mapping.
- [x] Conditional header blocks such as `headers { when request.id exists { ... } }`.
- [x] OAuth2 client credentials modeling and validation.
- [x] Proxy config and redirect policy validation for HTTP connectors.
- [x] Strong validation that sensitive headers/auth fields are wrapped in `sensitive(...)`.
- [x] Structured output block semantics for filtering fields and redaction paths at export time.
- [x] Complete formatter support for every newer construct, including typed blocks and quoted keys, with golden output tests.
- [x] YAML export.
- [x] Code generation from schemas.
- [x] Migration tooling for BCL version upgrades.
- [x] A richer hot-reload dependency watcher that follows imported files/modules and emits dependency-specific events.
- [x] Performance pass for lexer/parser allocation reduction and benchmark suite.
- [x] Public documentation generated from examples and `Requirements.md`.

## Remaining Hardening

- [x] Improve stdlib YAML scalar quoting/null/bool handling while keeping dependency-free export.
- [ ] Expand remote registry fetching from reachability/provenance validation to archive download/extraction once a registry package format is finalized.
- [ ] Deepen formatter golden tests for every example file and preserve comments during formatting.
- [x] Continue allocation reduction beyond the expression cache, especially parser token construction and normalized map generation.
- [x] Add a compiled expression bytecode program cache and VM to push repeated `Eval` calls closer to zero allocations.
- [x] Add typed VM fast paths for common boolean predicates to reduce the remaining expression interface allocations.
- [ ] Add parser/formatter comment-preserving trivia if comments must round-trip through `fmt`.

## Safety Boundaries

- [x] Compile, validate, explain, and simulate do not execute commands, write arbitrary files, or perform HTTP requests.
- [x] Command/file/http blocks are modeled and validated as configuration only.
- [x] Remote module network access is opt-in through explicit fetch APIs/CLI paths, never during normal compile, validate, explain, or simulate.

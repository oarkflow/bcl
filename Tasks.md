# Decision/Rules/Pattern Matching Roadmap

## Summary
Create a root-level `Tasks.md` and implement the roadmap one feature at a time. The first implementation slice is pattern matching because it is the smallest high-value change and has a clear test surface. After that, continue through decision authoring, diagnostics, outcomes, phases, tests, and explanation quality.

## P0: Pattern Matching
- [x] Add `ANY(pattern)` and `EXISTS(pattern)` collection patterns that pass when at least one list item matches `pattern`.
- [x] Bind named object rest patterns like `{ kind: "loan", ...rest }`.
- [x] Bind named list rest patterns like `["prime", ...tail]`.
- [x] Add negative patterns using `not PATTERN`.
- [x] Add validation warning for `match` expressions without a catch-all case.
- [x] Add language examples and tests for all pattern additions.

## P1: Decision Tables
- [x] Add `decision_table "id" { ... }` as a first-class decision authoring block.
- [x] Compile table rows into normal decision rules so existing evaluation/explain behavior still works.
- [x] Support `default`, `strategy`, row `when`, row `then`, row `priority`, and row `reason`.
- [x] Add validation for duplicate row IDs and undeclared effects.
- [x] Add loan eligibility example using a decision table.

## P1: Conflict And Coverage Diagnostics
- [x] Warn on duplicate equivalent conditions with different effects in the same decision.
- [x] Warn on catch-all rules that make later rules unreachable under `first_match`.
- [x] Warn when a decision has no explicit default.
- [x] Warn when a `match` over literal/enum-like cases has no fallback.
- [x] Add tests for warnings without breaking valid existing examples.

## P2: First-Class Outcomes
- [x] Normalize policy/rule/table results into a consistent outcome object.
- [x] Preserve existing `effect`, `score`, `action`, `event`, and `reason` compatibility.
- [x] Add `attributes`/`metadata` to decision results for downstream APIs.
- [x] Add scenario expectations for outcome attributes.

## P2: Rule Groups / Phases
- [x] Add optional phase metadata for decision rules: `phase validate|guard|score|decide|notify`.
- [x] Evaluate phases in deterministic order when present.
- [x] Preserve current behavior when no phases are declared.
- [x] Include phase in explain traces.

## P2: Test Matrices
- [x] Add `test_matrix` support for multiple decision cases in one block.
- [x] Support per-case `input` and `expect`.
- [x] Reuse existing scenario evaluator internally.
- [x] Add CLI/reporting coverage if current test runner exposes scenarios.

## P3: Explain "Why Not"
- [x] Extend condition evaluation to capture failed subconditions.
- [x] Include actual value, operator, expected value, and path where possible.
- [x] Keep existing explain JSON shape backward compatible.
- [x] Add tests for false conditions, missing fields, and type mismatches.

## P4: Decision Essentials Completion
- [x] Add explicit `MISSING` and `NULL` pattern sentinels.
- [x] Add `PATTERN as name` alias bindings.
- [x] Warn on invalid patterns, duplicate literal cases, unreachable cases after catch-all, and inconsistent alternative bindings.
- [x] Add schema-aware pattern diagnostics for decision conditions.
- [x] Add rule lifecycle metadata: `version`, `status`, `effective_from`, `effective_until`, `owner`, and `rationale`.
- [x] Skip inactive and out-of-window rules during evaluation.
- [x] Add first-class `obligation` and `advice` outputs.
- [x] Add conservative same-priority, numeric-overlap, and enum-coverage diagnostics.
- [x] Add batch and dataset decision evaluation helpers.
- [x] Add a complete example covering patterns, lifecycle metadata, obligations/advice, phases, test matrices, and batch reports.

## Implementation Order
1. Create `Tasks.md` and keep it as the living checklist.
2. Implement P0 pattern matching first.
3. Add P0 validation and docs.
4. Implement P1 decision tables.
5. Implement P1 diagnostics.
6. Continue P2/P3 features one at a time.
7. Keep P4 complete-decision features covered by tests and examples.

## Public Interfaces / Compatibility
- Existing `match`, `ANY`, `ALL`, `SOME`, `NONE`, policies, rule sets, rankings, scenarios, and decision results remain backward compatible.
- `ANY(pattern)` and `EXISTS(pattern)` mean at least one collection item matches.
- `ALL(pattern)` keeps its current all-items behavior.
- `not PATTERN` negates a pattern.
- `...rest` binds unmatched object fields or remaining list items.
- `decision_table "id" { ... }` compiles rows into ordinary decision rules.
- `MISSING` matches absent object fields only; `NULL` matches explicit null values.
- `PATTERN as name` binds the value that matched `PATTERN`.
- Rule lifecycle metadata is advisory except `status` and effective windows, which control rule eligibility.
- `obligation` and `advice` are emitted alongside actions/events without changing existing outputs.

## Test Plan
- Run `go test ./...` after each completed feature slice.
- Add focused unit tests for `ANY(pattern)` / `EXISTS(pattern)`, rest binding, negative patterns, match fallback validation, decision tables, and conflict/coverage diagnostics.
- Add or update examples so `examples_test.go` continues to cover the public syntax.
- Run the complete decision essentials package and its test matrix as a public syntax fixture.

## Assumptions
- `Tasks.md` lives at `/Users/sujit/Sites/bcl/Tasks.md`.
- `Requirements.md` is not currently present at repo root, so the roadmap is based on the current code and examples.
- Implementation proceeds sequentially, completing and testing one feature slice before starting the next.
- The first code change after `Tasks.md` is P0 pattern matching.

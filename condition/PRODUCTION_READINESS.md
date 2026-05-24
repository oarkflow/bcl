# Condition Production Readiness Plan

## Milestone 1: Publish-Time Safety

- Compile and validate decision definitions in strict mode for production services.
- Require a BCL version declaration for strict production validation.
- Require every strict module to declare a resolvable source.
- Optionally require tests or decision gates before publish.
- Keep permissive mode available for local prototypes and legacy examples.

## Milestone 2: Lifecycle Controls

- Support draft, validate, publish, activate, rollback, and reload with last-known-good behavior.
- Store definition digest, source path, version, environment, and metadata for every publish.
- Add approval metadata hooks before activation in controlled environments.
- Add diff and simulation reports to promotion workflows.

## Milestone 3: Runtime Determinism

- Make time, environment, external datasets, and HTTP adapters explicit in runtime options.
- Record dataset source metadata and ranking inputs in explain/audit output.
- Provide strict evaluation mode that fails on input validation diagnostics.
- Add reproducibility tests for repeated evaluations.

## Milestone 4: Observability And Audit

- Emit matched, skipped, and selected rules for every explainable decision.
- Include decision version, definition digest, reason code, latency, and trace summary in audits.
- Verify audit chains for memory, file, and SQLite stores.
- Add metrics for publish, evaluate, test, gate, rollback, and failure paths.

## Milestone 5: Scale And Operations

- Benchmark large rule bundles, large rankings, and concurrent evaluations.
- Add canary and shadow-evaluation workflows.
- Add emergency disable and rollback operations.
- Run all example use cases in strict validation with tests or gates.

## Current Slice

- Added service-level strict production validation switches.
- Added service-level test/gate requirement switches.
- Added validation tests for strict missing version/source failures and strict tested publish success.
- Added service-level strict evaluation switches.
- Added strict evaluation tests that reject input/schema diagnostics.
- Updated all condition decision-platform examples to pass strict validation.
- Added lifecycle test coverage for failed publishes not activating definitions.
- Added activation approval enforcement and approval audit metadata.
- Added emergency disable/enable controls that block evaluation while disabled.
- Added shadow evaluation support on evaluate requests for candidate comparisons.
- Added production readiness self-reporting for safety switches, store availability, and audit-chain health.
- Added persistent-store lifecycle coverage for file and SQLite stores.
- Added service benchmark harnesses for strict publish, strict evaluate, and shadow evaluate paths.

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
- Added fail-closed audit writes for publish, evaluate, lifecycle, workflow, simulation, reload, tests, and gates.
- Added CLI HTTP server read/header/write/idle timeouts, graceful shutdown, optional TLS, and max header sizing.
- Added trusted proxy CIDR handling so forwarded client IP headers are ignored unless the peer is trusted.
- Added SQLite operational tuning: ping on startup, foreign keys, busy timeout, WAL mode for file databases, synchronous NORMAL, and bounded connection pools.
- Added injectable BCL runtime clocks and service runtime policy for fixed time, env access, external adapter allowlists, HTTP host/method allowlists, and external timeouts.
- Added tenant partitioning for definitions, active versions, audits, reports, workflows, HTTP requests, CLI commands, and store queries. Legacy records map to tenant `default`.
- Added deny-by-default external dataset policy checks before evaluation.
- Added richer audit metadata for runtime policy fingerprints, input hashes, decision outcome, rule counts, diagnostics counts, latency, and dataset source fingerprints.
- Added tenant-aware metrics snapshots.
- Added strict validation coverage for all `condition/examples/*/decision.bcl`, tenant isolation tests, deterministic runtime tests, external policy tests, race-safe concurrent store tests, and additional scale benchmarks.
- Added SQLite table rebuild migration for legacy primary keys so existing databases can support duplicate definition name/version/environment across tenants.
- Added root BCL `Options` policy fields for direct callers that need adapter, HTTP host, HTTP method, and external timeout enforcement.
- Added a first-class canary service/API/CLI flow backed by compare reports.
- Added a complete `examples/enterprise-hardening` walkthrough for tenants, deterministic time, external dataset policy, and canary checks.

## Remaining Known Limits

- No known production-hardening gaps remain from this slice. Future work should focus on deployment-specific controls such as enterprise identity provider integration, long-term audit export, and operator runbooks.

# Condition

Condition is a BCL-backed decision automation platform for policy evaluation, ranking, workflow assignment, simulation, comparison, live reload, authorization, and tamper-evident audit records.

This module is intentionally separate from the root BCL module:

```go
module github.com/oarkflow/condition

replace github.com/oarkflow/bcl => ..
```

## Quick Start

```sh
go test ./...
go run ./examples/condition_decision_platform
go run ./cmd/condition serve --config ./examples/condition_decision_platform/condition.yaml
```

HTTP endpoints are protected with `github.com/oarkflow/authz`. For local testing, send `X-Roles: condition-admin`.

```sh
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions
```

Metrics are available to `condition-admin` and `condition-auditor` roles:

```sh
curl -H 'X-Roles: condition-auditor' http://localhost:8080/v1/metrics
```

The default CLI store is file-backed at `.condition`; SQLite is available with `--store sqlite --store-path condition.db`.

The HTTP server honors the service `Config` request timeout and max body size. Optional in-memory rate limiting can be enabled from Go with `server.WithRateLimit(limit, window)` or in `condition.yaml`.

## Production Surface

Condition now stores versioned definitions by `name + version + environment`, keeps an active pointer per environment, and evaluates the active version. Publish creates a version and activates it; `activate` and `rollback` promote existing versions without republishing.

Core CLI commands:

```sh
go run ./cmd/condition validate --name fraud-aml ./examples/condition_decision_platform/use_cases/fraud-aml/decision.bcl
go run ./cmd/condition publish --name fraud-aml --version 1 ./examples/condition_decision_platform/use_cases/fraud-aml/decision.bcl
go run ./cmd/condition versions fraud-aml
go run ./cmd/condition activate fraud-aml 1
go run ./cmd/condition evaluate fraud-aml --decision fraud_aml --input ./examples/condition_decision_platform/use_cases/fraud-aml/inputs/review.json --compact
go run ./cmd/condition gates fraud-aml --bundle risk_release
go run ./cmd/condition rollback fraud-aml 1
go run ./cmd/condition audits --definition fraud-aml --operation evaluate --limit 50
go run ./cmd/condition reports --kind simulation
go run ./cmd/condition audit verify
```

Every command accepts `--config`; explicit command flags override config values. A config file can set the address, environment, store kind/path, request timeout, body limit, rate limit, and `.authz` policy path.

## HTTP API

Use `X-Roles: condition-admin` for local demos, or configure roles in `.authz`.

```sh
curl -H 'X-Roles: condition-admin' http://localhost:8080/healthz
curl -H 'X-Roles: condition-admin' http://localhost:8080/readyz

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"name":"fraud-aml","version":"1","path":"./examples/condition_decision_platform/use_cases/fraud-aml/decision.bcl","run_tests":true}' \
  http://localhost:8080/v1/definitions/validate

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"name":"fraud-aml","version":"1","path":"./examples/condition_decision_platform/use_cases/fraud-aml/decision.bcl"}' \
  http://localhost:8080/v1/definitions

curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions/fraud-aml
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions/fraud-aml/versions
curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/definitions/fraud-aml/versions/1/activate
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"version":"1"}' \
  http://localhost:8080/v1/definitions/fraud-aml/rollback

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"decision":"fraud_aml","input":{"customer":{"id":"c1","blacklisted":true},"transaction":{"amount":500,"channel":"card"}}}' \
  http://localhost:8080/v1/definitions/fraud-aml/evaluate

curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/definitions/fraud-aml/tests
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"bundle":"risk_release"}' \
  http://localhost:8080/v1/definitions/fraud-aml/gates
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"candidate_path":"./candidate.bcl","cases":[]}' \
  http://localhost:8080/v1/definitions/fraud-aml/simulate
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"candidate_path":"./candidate.bcl","cases":[]}' \
  http://localhost:8080/v1/definitions/fraud-aml/compare

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"input":{"transaction":{"amount":100000}}}' \
  http://localhost:8080/v1/definitions/case-review-workflow/workflows/manual_review/start
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"input":{"reviewer":{"approved":true}}}' \
  http://localhost:8080/v1/workflows/{run_id}/advance
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/workflows/{run_id}
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/workflows

curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/audits?definition=fraud-aml&operation=evaluate&limit=50'
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/audits/{audit_id}
curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/audits/verify
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/reports?kind=simulation'
curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/reload
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/metrics
```

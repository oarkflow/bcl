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
go run ./examples/payment-risk
go run ./examples/access-control
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
go run ./cmd/condition validate --name payment-risk ./examples/payment-risk/decision.bcl
go run ./cmd/condition publish --name payment-risk --version 1 ./examples/payment-risk/decision.bcl
go run ./cmd/condition versions payment-risk
go run ./cmd/condition activate payment-risk 1
go run ./cmd/condition evaluate payment-risk --decision payment_risk --input ./examples/payment-risk/inputs/authorize.json --compact
go run ./cmd/condition rollback payment-risk 1
go run ./cmd/condition audits --definition payment-risk --operation evaluate --limit 50
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
  -d '{"name":"payment-risk","version":"1","path":"./examples/payment-risk/decision.bcl","run_tests":true}' \
  http://localhost:8080/v1/definitions/validate

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"name":"payment-risk","version":"1","path":"./examples/payment-risk/decision.bcl"}' \
  http://localhost:8080/v1/definitions

curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions/payment-risk
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions/payment-risk/versions
curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/definitions/payment-risk/versions/1/activate
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"version":"1"}' \
  http://localhost:8080/v1/definitions/payment-risk/rollback

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"decision":"payment_risk","input":{"card":{"stolen":false},"payment":{"risk_score":12,"cross_border":false},"customer":{"trusted":true}}}' \
  http://localhost:8080/v1/definitions/payment-risk/evaluate

curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/definitions/payment-risk/tests
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"candidate_path":"./candidate.bcl","cases":[]}' \
  http://localhost:8080/v1/definitions/payment-risk/simulate
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"candidate_path":"./candidate.bcl","cases":[]}' \
  http://localhost:8080/v1/definitions/payment-risk/compare

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"input":{"transaction":{"amount":100000}}}' \
  http://localhost:8080/v1/definitions/case-review-workflow/workflows/manual_review/start
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"input":{"reviewer":{"approved":true}}}' \
  http://localhost:8080/v1/workflows/{run_id}/advance
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/workflows/{run_id}
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/workflows

curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/audits?definition=payment-risk&operation=evaluate&limit=50'
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/audits/{audit_id}
curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/audits/verify
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/reports?kind=simulation'
curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/reload
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/metrics
```

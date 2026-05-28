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

The HTTP server honors the service `Config` request timeout and max body size. Optional in-memory rate limiting can be enabled from Go with `server.WithRateLimit(limit, window)` or in `condition.yaml`. The CLI server also sets HTTP read/header/write/idle timeouts, supports graceful shutdown, and can serve TLS with `--tls-cert` and `--tls-key`.

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
go run ./cmd/condition canary payment-risk --candidate ./candidate.bcl --dataset payment_risk_batch --max-changed 0
go run ./cmd/condition audits --definition payment-risk --operation evaluate --limit 50
go run ./cmd/condition reports --kind simulation
go run ./cmd/condition audit verify
```

Every command accepts `--config`; explicit command flags override config values. BCL is the native config format for Condition, and JSON/YAML remain supported for compatibility. A config file can set the address, environment, default tenant, store kind/path, request timeout, body limit, runtime policy, rate limit, HTTP server timeouts, TLS files, trusted proxy CIDRs, and `.authz` policy path. CLI commands also accept `--tenant`, defaulting to `default`.

Production config example:

```bcl
address ":8443"
authz_path "./condition.authz"
trusted_proxies ["10.0.0.0/24"]

tls {
  cert_file "/etc/condition/tls.crt"
  key_file "/etc/condition/tls.key"
}

http {
  read_header_timeout "5s"
  read_timeout "15s"
  write_timeout "30s"
  idle_timeout "60s"
  shutdown_timeout "10s"
  max_header_bytes 1048576
}

store {
  kind "sqlite"
  path "/var/lib/condition/condition.db"
}

service {
  environment "production"
  default_tenant "default"
  request_timeout "5s"
  max_request_bytes 1048576
  strict_validation true
  strict_evaluation true
  require_tests true
  require_activation_approval true

  runtime {
    fixed_time "2026-05-26T00:00:00Z"
    allow_env false
    allowed_dataset_adapters ["file"]
    allowed_http_hosts []
    allowed_http_methods ["GET"]
    external_timeout "2s"
  }
}

rate_limit {
  limit 120
  window "1m"
}
```

Audit writes are fail-closed: if the audit sink cannot read the previous hash or append the new sealed record, publish/evaluate/lifecycle operations return an error instead of continuing silently. Definitions, active versions, audits, reports, and workflows are tenant-partitioned; HTTP requests use `X-Tenant-ID`. Runtime policy is deny-by-default for external datasets, and file/HTTP access must be explicitly allowed. SQLite stores enable foreign keys, busy timeout, WAL mode for file databases, and bounded connection pools. `X-Forwarded-For` and `X-Real-IP` are ignored unless the immediate peer is listed in `trusted_proxies`.

A complete runnable BCL server config is in `examples/production-server`.

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

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"event":"failed_login","input":{"principal":{"id":"user-1"}}}' \
  http://localhost:8080/v1/definitions/http-auth-guard/chains/http_request_chain/evaluate

curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"phase":"post","method":"GET","path":"/endpoint-error","input":{"request":{"actor_key":"/endpoint-error","application_key":"request-lifecycle-demo"}},"response":{"status":500}}' \
  http://localhost:8080/v1/definitions/request-lifecycle/lifecycles/http_request/evaluate

curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions/request-lifecycle/route-coverage
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/actions?action=notify&limit=20'
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/incidents?status=open&limit=20'
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"definition":"request-lifecycle","before":"2026-05-28T00:00:00Z"}' \
  http://localhost:8080/v1/state/compact

curl -H 'X-Roles: condition-admin' -X POST http://localhost:8080/v1/definitions/payment-risk/tests
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"candidate_path":"./candidate.bcl","cases":[]}' \
  http://localhost:8080/v1/definitions/payment-risk/simulate
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"candidate_path":"./candidate.bcl","cases":[]}' \
  http://localhost:8080/v1/definitions/payment-risk/compare
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' -d '{"candidate_path":"./candidate.bcl","dataset":"payment_risk_batch","max_changed_cases":0,"require_no_errors":true,"promote":true,"promote_version":"2"}' \
  http://localhost:8080/v1/definitions/payment-risk/canary

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

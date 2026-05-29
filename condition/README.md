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

## Platform Guide And Complete Examples

For the native Condition policy platform features, read:

- `docs/platform-guide.md`
- `examples/request-lifecycle`
- `examples/http-auth-guard`
- `examples/complete-platform`
- `examples/anomaly-detection`

These cover policy packages, contracts, action catalogs, route catalogs, overlays, lifecycle phases, request/response envelopes, response classification, stateful chains, composite watches, decay/cooldown/reset behavior, action delivery, incidents, route coverage, and lifecycle tests.

## Adding New Conditions, Rules, Actions, And Lifecycles

Use this flow when adding a new policy domain, endpoint rule, anomaly, action, or stateful escalation. The goal is to keep policy behavior in BCL while the host application only supplies trusted facts, calls Condition, and applies the returned result.

### 1. Decide The Fact Boundary

Start by listing which facts are trusted application facts and which facts are user/request facts.

- Use `request.*` for payload, route, method, headers, response, and other request-owned or transport-owned values.
- Use `context.*` for middleware-derived facts such as device posture, network reputation, geo lookup, risk score, feature flags, and upstream service signals.
- Use `session.*` for authenticated session/account facts such as logged-in state, session ID, token reuse, MFA status, account mutation signals, and server-side session risk.
- Use `response.*` for post-request status, response body, response format, duration, and error facts.

Do not allow a client body to assert security-sensitive facts such as `session.risk.impossible_travel`, `context.device.trusted`, `context.network.ip_reputation`, or `session.token.reuse_detected`. Populate those from Go middleware or `context.Context`:

```go
ctx := condition.WithContextFacts(ctx, map[string]any{
	"device": map[string]any{
		"trusted": true,
		"changed": false,
	},
	"network": map[string]any{
		"ip_reputation": 80,
	},
})

ctx = condition.WithSession(ctx, map[string]any{
	"risk": map[string]any{
		"impossible_travel": true,
	},
	"token": map[string]any{
		"reuse_detected": false,
	},
})
```

### 2. Add Or Update Route Catalogs

Every lifecycle policy should know which route it is evaluating. Add routes under a catalog file such as `rules/routes.bcl`.

```bcl
routes "http" {
  route "payments_authorize" {
    method "POST"
    pattern "/payments/authorize"
    metadata {
      category "payments"
      sensitivity "high"
      policy_type "commerce-risk"
    }
  }
}
```

Use route metadata for stable policy grouping. Rules can then depend on `route.id`, `route.category`, `route.sensitivity`, and any custom metadata you attach.

### 3. Declare Actions And Output Contracts

Add action names to an action catalog or output contract before decisions emit them. This keeps emitted behavior explicit and validation-friendly.

```bcl
action_catalog "platform_actions" {
  action "allow" {
    sink "event"
  }

  action "challenge" {
    sink "event"
    severity "medium"
  }

  action "hold" {
    sink "event"
    severity "high"
  }

  action "notify" {
    sink "log"
    severity "medium"
  }
}
```

Condition always persists/audits emitted actions. External sinks remain safe by default and require runtime infrastructure allowlists before network delivery.

### 4. Add A Decision Table

A decision table detects a condition and emits an action/event. Keep detection in BCL and keep host code free of policy-specific `if action == ...` logic.

```bcl
decision_table "payment_risk_detection" {
  contract "platform_actions"
  default allow
  hit_policy first

  row "large-payment-from-risky-session" {
    priority 100
    when {
      all {
        route.id == "payments_authorize"
        request.payment.amount_vs_avg >= 5
        any {
          context.device.trusted == false
          context.network.ip_reputation >= 70
          session.risk.impossible_travel == true
        }
      }
    }
    then {
      outcome {
        decision require_review
        reason "payment has elevated session and amount risk"
        attributes {
          action "payment_or_order_anomaly"
          threat "payment_spike"
        }
        metadata {
          severity "high"
          category "commerce"
        }
      }
    }
    reason "payment has elevated session and amount risk"
    reason_code "PAYMENT_SPIKE_RISK"
  }
}
```

Use `attributes` for event/action details that downstream chains should see. Use `metadata` for severity, category, ownership, and reporting context.

### 5. Add Stateful Chains And Watches

Use a `chain` when behavior depends on history: repeated failures, velocity, spikes, ratios, cooldowns, or escalation ladders. Steps can define reusable result envelopes once, then decisions and pre-request guards can reference them with `result`.

```bcl
chain "commerce_risk_chain" {
  entity "request.actor_key"

  watch "payment_risk" {
    event "payment_or_order_anomaly"
    window "30m"
    cooldown "2m"

    step "challenge_payment" {
      id "commerce.challenge"
      threshold 1
      action "challenge"
      severity "medium"
      ttl "10m"

      response {
        blocking true
        status 402
        retry_after_seconds 600
        body {
          error "payment_challenge"
          message "transaction requires customer verification"
        }
      }
    }

    step "hold_payment" {
      id "commerce.hold"
      threshold 3
      action "hold"
      severity "high"
      ttl "1h"

      response {
        blocking true
        status 409
        retry_after_seconds 3600
        body {
          error "payment_held"
          message "transaction held after repeated risk signals"
        }
      }
    }
  }
}
```

For aggregation anomalies, add metrics and grouping:

```bcl
watch "registration_velocity" {
  event "registration_submitted"
  window "10m"
  group_by "context.network.ip"
  distinct "attributes.request.account.email_domain"
  metrics [
    "count"
    "rate"
  ]

  step "notify_registration_velocity" {
    id "identity.registration_velocity"
    metric "distinct_count"
    op ">="
    value 3
    action "notify"
    severity "high"
    ttl "30m"
  }
}
```

Supported watch patterns include count thresholds, rate, baseline comparison, ratio changes, distinct counts, grouped aggregation, and consecutive failures. `400`, `401`, and `403` are treated as expected client outcomes; other `4xx` and all `5xx` are unhealthy unless policy overrides the status sets.

### 6. Wire The Lifecycle

A lifecycle decides which decisions and chains run during each phase.

```bcl
lifecycle "http_request" {
  entity "request.actor_key"
  routes "http"

  phase "pre" {
    decision "pre_request_guard"
  }

  phase "post" {
    decision "payment_risk_detection"
    decision "response_observability"
    chain "commerce_risk_chain"
  }
}
```

Use `pre` for active enforcement before the handler runs. Use `post` for observing handler outcomes, response status, logs, errors, and emitted events. You can add more phases, but keep `pre` and `post` as the standard HTTP lifecycle phases.

### 7. Add Pre-Request Enforcement

Pre-request guards read active `chain_state` and return a reusable result envelope. This is how active block/rate-limit/challenge state stops a request before the business handler runs.

```bcl
decision_table "pre_request_guard" {
  default allow
  hit_policy first

  row "payment-hold-active" {
    priority 100
    when {
      chain_state.payment_risk.step == "hold_payment"
    }
    then {
      outcome {
        decision deny
        reason "payment hold is active"
        attributes {
          result "commerce.hold"
        }
      }
    }
    reason "payment hold is active"
    reason_code "PAYMENT_HOLD_ACTIVE"
  }
}
```

The host should apply the returned enforcement envelope generically: status, headers, retry time, body, action, reason, and severity come from BCL.

### 8. Add Tests Before Publishing

Add BCL lifecycle tests beside the policy:

```bcl
lifecycle_test "large risky payment is challenged" {
  lifecycle "http_request"
  phase "post"
  method "POST"
  path "/payments/authorize"
  input {
    request.actor_key "buyer-1"
    context.device.trusted false
    context.network.ip_reputation 80
  }
  request {
    payment {
      amount_vs_avg 6
    }
    format "json"
  }
  response {
    status 200
    format "json"
  }
  expect {
    final_action "challenge"
    route "payments_authorize"
  }
}
```

For Go tests, evaluate through `Service.EvaluateLifecycle` or the example server and assert the returned action, status, retry header, state, and body. Include negative tests proving user-controlled request bodies cannot trigger trusted `context.*` or `session.*` facts.

### 9. Validate, Simulate, Publish, And Reload

Run the policy locally:

```sh
go run ./cmd/condition validate --name my-policy ./decision.bcl
go run ./cmd/condition publish --name my-policy --version 1 ./decision.bcl
go run ./cmd/condition evaluate my-policy --decision payment_risk_detection --input ./input.json
```

For lifecycle policies, use the lifecycle endpoint or service API:

```sh
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"phase":"post","method":"POST","path":"/payments/authorize","input":{"request":{"actor_key":"buyer-1"}},"request":{"payment":{"amount_vs_avg":6}},"response":{"status":200}}' \
  http://localhost:8080/v1/definitions/my-policy/lifecycles/http_request/evaluate
```

If the server runs with reload watching, edit any imported `*.bcl` file and Condition reloads from the root definition file. Valid policy is activated; invalid policy keeps the last known good definition active.

### 10. Inspect Runtime Effects

After evaluation, inspect what happened:

- Chain state: current step/action, counters, TTL, analytics, and expiry.
- Events: emitted policy events and their attributes.
- Actions: delivery records, sink, status, retries, and dead-letter state.
- Incidents: grouped repeated events by tenant, entity, route, action, severity, and reason.
- Route coverage: declared routes, matched routes, and unmatched traffic.
- Audits: tamper-evident publish/evaluate/reload/test records.

Use these endpoints in development:

```sh
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/actions?limit=20'
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/incidents?limit=20'
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/definitions/my-policy/route-coverage'
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/audits?definition=my-policy&limit=20'
```

### Checklist

- Define trusted fact sources: `request.*`, `context.*`, `session.*`, and `response.*`.
- Add or update route catalog metadata.
- Declare actions and output contracts.
- Add decision tables for detection.
- Add chains/watches for history, aggregation, cooldowns, and escalation.
- Add structured response envelopes on steps.
- Reference step results with `attributes { result "..." }`.
- Wire decisions and chains into lifecycle phases.
- Add pre-request guard rows for active state enforcement.
- Add BCL lifecycle tests and focused Go tests.
- Validate, simulate, publish, and inspect events/actions/incidents/audits.

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

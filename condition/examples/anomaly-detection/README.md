# Anomaly Detection Platform

This example is a broad anomaly detection reference app built on native Condition lifecycle policy. The Fiber server is intentionally thin: it parses request facts, calls `EvaluateLifecycle`, and applies the returned enforcement envelope. The BCL rules own anomaly detection, escalation, action names, sinks, response status, retry headers, response bodies, action delivery, incidents, state, and the threat model.

Run it from the `condition` module:

```sh
go run ./examples/anomaly-detection
go run ./examples/anomaly-detection --serve --addr :8083
go run ./examples/anomaly-detection --serve --watch --addr :8083
```

Validate the policy:

```sh
go run ./cmd/condition validate --name anomaly-detection ./examples/anomaly-detection/decision.bcl
```

## What It Demonstrates

- Session hijacking detection: impossible travel, device change, token reuse, MFA bypass, ASN drift, and IP reputation.
- Account takeover detection: MFA disable, recovery/profile mutation, new device, and profile country drift.
- Fraud claim detection: mule indicators, duplicate identity, synthetic identity, identity velocity, chargeback, and suspicious claim amount.
- Insider-risk detection: privileged export, disabled audit trail, sensitive dataset access, and off-hours export.
- Bot defense: credential stuffing, headless clients, captcha failures, request entropy, and scraping rate.
- Supply-chain detection: vendor bank changes, new payees, approval bypass, risky regions, and unusual purchase order size.
- Compliance detection: sanctions, PEP, watchlist, adverse media, missing KYC/KYB, and beneficial-owner mismatch.
- Geo and country controls: blocked countries, restricted admin regions, regional drift, and tenant data-residency mismatch.
- Business anomalies: off-hours admin operations, policy-change windows, ticket spam, reopen loops, SLA manipulation, coupon/refund abuse, and logistics policy violations.
- Commerce anomalies: payment amount spikes, velocity, new payment methods, and shipping/billing mismatch.
- Data governance anomalies: PII export, bulk downloads, restricted datasets, and cross-region queries.
- API health and behavior anomalies: unexpected `4xx`, repeated `5xx`, unusual method mix, endpoint bursts, high error ratios, notification, and escalation.
- Native aggregation anomalies: traffic spikes, success-rate drops, unexpected error ratios, consecutive failures, registration velocity, and generic log/event/trigger bursts.

## Policy Layout

```text
decision.bcl
rules/catalogs.bcl
rules/routes.bcl
rules/lifecycle.bcl
rules/decisions/*.bcl
rules/chains/*.bcl
tests/lifecycle_tests.bcl
```

The chains define reusable result envelopes with structured `response { body { ... } }` blocks. Pre-request enforcement uses result IDs such as `session.step_up`, `geo.block_region`, `commerce.challenge`, and `data.quarantine`.

The Go host does not define action allowlists, sink lists, chain IDs, watch IDs, or threat-model content. Those are declared in BCL and surfaced through Condition service APIs.

Session, device, network, token, login, and account-risk signals are application-owned facts. In this demo they come from a small server-side session/context lookup keyed by actor, then flow into Condition through `condition.WithContextFacts` and `condition.WithSession`. They are not accepted from request bodies or demo headers. Production apps should populate the same `context.*` and `session.*` facts from auth middleware, session stores, device fingerprinting, IP reputation services, geo middleware, and account-risk services.

When `--watch` is enabled, the server watches `decision.bcl` and every imported `*.bcl` file under the policy tree. Valid edits are reloaded and activated automatically; invalid edits keep the last known good policy active.

## Complete Flow: Add A New Anomaly

This is the flow to add a new condition, rule, action, chain, lifecycle behavior, and curlable example. The example below adds a refund-abuse anomaly. The same pattern works for session risk, payments, compliance, bot defense, data access, logs, events, triggers, or any other domain.

### 1. Define The Signal And Trust Boundary

First decide where each fact comes from.

- `request.refund.*`: user or business payload for the refund request.
- `context.risk.*`: trusted middleware or service-derived risk, such as historical refund risk.
- `session.*`: authenticated account/session facts.
- `response.*`: handler result observed after the request.

For refund abuse, the user can submit the refund amount and reason, but the app should derive account age, historical refund count, device trust, IP reputation, and other security signals from middleware or services.

```go
ctx = condition.WithContextFacts(ctx, map[string]any{
	"risk": map[string]any{
		"refund_count_7d": 5,
		"refund_amount_7d": 1200,
	},
	"device": map[string]any{
		"trusted": false,
	},
})
```

Do not read those trusted values from request JSON. The host only passes them through `context.Context`; BCL owns the policy decision.

### 2. Add The Route

Add a route entry in `rules/routes.bcl`.

```bcl
routes "http" {
  route "refund_request" {
    method "POST"
    pattern "/refunds/request"
    metadata {
      category "commerce"
      sensitivity "high"
      policy_type "refund-risk"
    }
  }
}
```

Then register the HTTP endpoint in the Fiber app. The handler stays generic and does not know about actions, thresholds, or escalation.

```go
app.Post("/refunds/request", acceptHandler("refund", "requested"))
```

### 3. Declare The Actions

Add event and enforcement actions in `rules/catalogs.bcl`. If the action already exists, reuse it.

```bcl
action_catalog "anomaly_actions" {
  action "refund_anomaly" {
    sink "event"
    severity "medium"
  }

  action "challenge" {
    sink "event"
    severity "medium"
  }

  action "hold" {
    sink "event"
    severity "high"
  }

  action "open_case" {
    sink "event"
    severity "critical"
  }
}
```

Condition persists action delivery records for declared actions. External delivery remains disabled unless runtime infrastructure explicitly allows it.

### 4. Add The Detection Decision

Create `rules/decisions/refund_risk.bcl`.

```bcl
decision_table "refund_risk_detection" {
  contract "anomaly_actions"
  default allow
  hit_policy first

  row "high-refund-velocity" {
    priority 100
    when {
      all {
        route.id == "refund_request"
        any {
          context.risk.refund_count_7d >= 3
          context.risk.refund_amount_7d >= 1000
          request.refund.amount >= 500
        }
        any {
          context.device.trusted == false
          context.network.ip_reputation >= 70
        }
      }
    }
    then {
      outcome {
        decision require_review
        reason "refund request has velocity and trust anomalies"
        attributes {
          action "refund_anomaly"
          threat "refund_abuse"
        }
        metadata {
          severity "high"
          category "commerce"
        }
      }
    }
    reason "refund request has velocity and trust anomalies"
    reason_code "REFUND_ABUSE_RISK"
  }
}
```

The decision emits `refund_anomaly`. It does not decide cooldowns, repeated escalation, response body, or pre-request blocking. That belongs in a chain.

### 5. Add The Stateful Chain

Create `rules/chains/refund_risk.bcl`.

```bcl
chain "refund_risk_chain" {
  entity "request.actor_key"

  watch "refund_abuse" {
    event "refund_anomaly"
    window "24h"
    cooldown "5m"

    step "challenge_refund" {
      id "refund.challenge"
      threshold 1
      action "challenge"
      severity "medium"
      ttl "10m"

      response {
        blocking true
        status 402
        retry_after_seconds 600
        body {
          error "refund_challenge"
          message "refund request requires additional verification"
        }
      }
    }

    step "hold_refund" {
      id "refund.hold"
      threshold 2
      action "hold"
      severity "high"
      ttl "1h"

      response {
        blocking true
        status 409
        retry_after_seconds 3600
        body {
          error "refund_held"
          message "refund request held after repeated refund-risk signals"
        }
      }
    }

    step "open_refund_case" {
      id "refund.open_case"
      threshold 4
      action "open_case"
      severity "critical"
      ttl "24h"

      response {
        blocking true
        status 423
        retry_after_seconds 86400
        body {
          error "refund_case_opened"
          message "refund activity opened a risk case"
        }
      }
    }
  }
}
```

Each `step` defines a reusable result envelope. That envelope is what the host writes back to HTTP clients when the step is blocking.

### 6. Add Pre-Request Enforcement

Update `rules/decisions/pre_request_guard.bcl` so active refund state can block before the handler runs.

```bcl
row "refund-hold-active" {
  priority 80
  when {
    chain_state.refund_abuse.step == "hold_refund"
  }
  then {
    outcome {
      decision deny
      reason "refund hold is active"
      attributes {
        result "refund.hold"
      }
    }
  }
  reason "refund hold is active"
  reason_code "REFUND_HOLD_ACTIVE"
}
```

Use `result "refund.hold"` instead of duplicating action, status, retry, and body fields. If the chain step changes, pre-request enforcement stays consistent.

### 7. Wire The Lifecycle

Update `rules/lifecycle.bcl`.

```bcl
lifecycle "http_request" {
  entity "request.actor_key"
  routes "http"

  phase "pre" {
    decision "pre_request_guard"
  }

  phase "post" {
    decision "refund_risk_detection"
    chain "refund_risk_chain"
  }
}
```

For most business/anomaly domains, detection runs in `post` so the handler response is available. Active state enforcement runs in `pre`.

### 8. Import The New Files

Add imports to `decision.bcl`.

```bcl
import "rules/decisions/refund_risk.bcl"
import "rules/chains/refund_risk.bcl"
```

When running with `--watch`, editing either imported file triggers a safe reload from the root `decision.bcl`.

### 9. Add Lifecycle Tests

Add a lifecycle test to `tests/lifecycle_tests.bcl`.

```bcl
lifecycle_test "refund velocity is challenged" {
  lifecycle "http_request"
  phase "post"
  method "POST"
  path "/refunds/request"
  input {
    request.actor_key "refund-user-1"
    context.risk.refund_count_7d 5
    context.risk.refund_amount_7d 1200
    context.device.trusted false
    context.network.ip_reputation 75
  }
  request {
    refund {
      amount 600
      reason "not_as_described"
    }
    format "json"
  }
  response {
    status 200
    body {
      refund "requested"
    }
    format "json"
  }
  expect {
    final_action "challenge"
    route "refund_request"
  }
}
```

Also add a negative test when needed: same request body without trusted `context.*` risk should not trigger. This guards the fact boundary.

### 10. Run And Inspect It

Validate first:

```sh
go run ./cmd/condition validate --name anomaly-detection ./examples/anomaly-detection/decision.bcl
```

Run the server:

```sh
go run ./examples/anomaly-detection --serve --addr :8083
```

Call the endpoint:

```sh
curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"refund-user-1"},"refund":{"amount":600,"reason":"not_as_described"}}' \
  http://127.0.0.1:8083/refunds/request
```

Expected first effect:

```text
HTTP/1.1 402 Payment Required
X-Condition-Action: challenge
X-Condition-State: refund_risk_chain/refund_abuse/challenge_refund
Retry-After: 600
```

Inspect state, events, actions, and incidents:

```sh
curl -s 'http://127.0.0.1:8083/_state?actor=refund-user-1'
curl -s 'http://127.0.0.1:8083/_events?actor=refund-user-1'
curl -s http://127.0.0.1:8083/_actions
curl -s http://127.0.0.1:8083/_incidents
```

### What The Runtime Flow Looks Like

For a request to `/refunds/request`, the full path is:

1. Fiber parses body into ordinary request facts.
2. Host middleware/session lookup adds trusted `context.*` and `session.*` facts.
3. Condition matches the route from `rules/routes.bcl`.
4. `pre` lifecycle reads existing `chain_state`; if a hold/case is active, it returns the referenced result envelope and the handler does not run.
5. Handler runs only when pre-request policy allows it.
6. `post` lifecycle receives request and response facts.
7. `refund_risk_detection` emits `refund_anomaly` when the BCL condition matches.
8. `refund_risk_chain` updates counters/window state and selects the highest matching step.
9. Condition persists events, state, action delivery records, incidents, and audit.
10. The host writes the returned BCL-owned enforcement envelope as HTTP status, headers, and JSON body.

The application never hardcodes `refund_anomaly`, `challenge`, `hold`, thresholds, status codes, retry values, or response bodies. Those live in BCL.

### New Policy Checklist

- Add route in `rules/routes.bcl`.
- Add or reuse actions in `rules/catalogs.bcl`.
- Add one decision file under `rules/decisions`.
- Add one chain file under `rules/chains` if history, escalation, cooldown, aggregation, or blocking is needed.
- Add pre-request guard rows for active blocking states.
- Wire decision and chain into `rules/lifecycle.bcl`.
- Import new files from `decision.bcl`.
- Add BCL lifecycle tests.
- Add Go/example tests if the host fact boundary or middleware mapping changed.
- Run `go run ./cmd/condition validate --name anomaly-detection ./examples/anomaly-detection/decision.bcl`.
- Run `go test ./examples/anomaly-detection ./pkg/condition`.
- Use `/_state`, `/_events`, `/_actions`, `/_incidents`, `/_coverage`, and `/_readiness` to inspect behavior.

## Curl Flows

```sh
curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"alice"}}' \
  http://127.0.0.1:8083/session/continue

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"acct-1"}}' \
  http://127.0.0.1:8083/accounts/update-profile

for i in {1..4}; do curl -i -H 'Content-Type: application/json' \
  -d "{\"actor\":{\"id\":\"reg-$i\"},\"account\":{\"email_domain\":\"example$i.com\"}}" \
  http://127.0.0.1:8083/accounts/register; done

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"claimant-1"},"fraud":{"mule_indicator":true,"duplicate_identity":true}}' \
  http://127.0.0.1:8083/fraud/claim

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"employee-1"},"insider":{"privileged_export":true,"sensitive_dataset":true,"records_exported":15000}}' \
  http://127.0.0.1:8083/insider/export

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"bot-client"},"bot":{"credential_stuffing":true,"captcha_failures":4,"request_entropy":92}}' \
  http://127.0.0.1:8083/bots/challenge

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"buyer-ops"},"vendor":{"bank_changed":true,"new_payee":true,"purchase_order_vs_avg":6}}' \
  http://127.0.0.1:8083/supply-chain/vendor-change

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"screening-1"},"compliance":{"sanctions_hit":true,"pep_hit":true}}' \
  http://127.0.0.1:8083/compliance/screen

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"buyer-1"},"geo":{"country":"IR","region":"blocked"},"payment":{"amount_vs_avg":1,"velocity_10m":1,"new_method":false,"shipping_country":"IR","billing_country":"IR"}}' \
  http://127.0.0.1:8083/payments/authorize

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"buyer-2"},"geo":{"country":"US","region":"us"},"payment":{"amount_vs_avg":6,"velocity_10m":1,"new_method":false,"shipping_country":"US","billing_country":"US"}}' \
  http://127.0.0.1:8083/payments/authorize

curl -i -H 'X-Actor: analyst-eu' -H 'X-Role: analyst' -H 'X-Region: eu' -H 'X-Country: DE' \
  http://127.0.0.1:8083/admin/risk-console

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"support-loop-1"},"tenant":{"verified":true},"support":{"reopen_count":4,"severity":3}}' \
  http://127.0.0.1:8083/support/tickets

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"shipper-1"},"logistics":{"route_country_mismatch":true,"restricted_destination":false,"hazmat":false,"carrier_certified":true,"reroute_attempts":0}}' \
  http://127.0.0.1:8083/logistics/shipments

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"analyst-1"},"tenant":{"region":"us","verified":true},"geo":{"country":"DE","region":"eu"},"data":{"pii_export":true,"bulk_rows":25000,"restricted_dataset":true,"cross_region":true}}' \
  http://127.0.0.1:8083/data/query

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"api-client"},"api":{"unusual_method_mix":true,"endpoint_burst":25,"error_ratio":0.1}}' \
  http://127.0.0.1:8083/api/resource

for i in {1..5}; do curl -i 'http://127.0.0.1:8083/traffic/ok'; done
for i in {1..3}; do curl -i 'http://127.0.0.1:8083/traffic/missing'; done
for i in {1..3}; do curl -i 'http://127.0.0.1:8083/traffic/fail'; done
for i in {1..5}; do curl -i -H 'Content-Type: application/json' \
  -d "{\"actor\":{\"id\":\"signal-$i\"},\"signal\":{\"source\":\"worker-$i\"}}" \
  http://127.0.0.1:8083/signals/logs; done

for i in {1..5}; do curl -i 'http://127.0.0.1:8083/api/resource?mode=fail'; done
```

Inspect runtime effects:

```sh
curl -s http://127.0.0.1:8083/_threat-model
curl -s 'http://127.0.0.1:8083/_state?actor=alice'
curl -s 'http://127.0.0.1:8083/_events?actor=alice'
curl -s http://127.0.0.1:8083/_actions
curl -s http://127.0.0.1:8083/_incidents
curl -s http://127.0.0.1:8083/_coverage
curl -s http://127.0.0.1:8083/_readiness
```

## Threat Model

Assets include sessions, account profiles, identity graph, claims, payments, orders, support workflows, risk console controls, privileged exports, bot-protected surfaces, traffic baselines, logs/events/triggers, vendor payment data, compliance screening, shipments, PII datasets, and API availability. Threat actors include compromised accounts, fraud operators, malicious insiders, and automation clients. Controls include step-up authentication, transaction challenge, order hold, regional block, data quarantine, session termination, account suspension, notifications, escalation, and risk case creation.

The live `/_threat-model` endpoint returns the `threat_model "anomaly_detection"` block from `decision.bcl` as JSON for automation and demos.

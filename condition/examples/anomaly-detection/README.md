# Anomaly Detection Platform

This example is a broad anomaly detection reference app built on native Condition lifecycle policy. The Fiber server is intentionally thin: it parses request facts, calls `EvaluateLifecycle`, and applies the returned enforcement envelope. The BCL rules own anomaly detection, escalation, response status, retry headers, response bodies, action delivery, incidents, and state.

Run it from the `condition` module:

```sh
go run ./examples/anomaly-detection
go run ./examples/anomaly-detection --serve --addr :8083
```

Validate the policy:

```sh
go run ./cmd/condition validate --name anomaly-detection ./examples/anomaly-detection/decision.bcl
```

## What It Demonstrates

- Session hijacking detection: impossible travel, token reuse, MFA bypass, ASN drift, and IP reputation.
- Geo and country controls: blocked countries, regional drift, and tenant data-residency mismatch.
- Business anomalies: off-hours admin operations, ticket spam, SLA manipulation, coupon/refund abuse, and logistics policy violations.
- Commerce anomalies: payment amount spikes, velocity, new payment methods, and shipping/billing mismatch.
- Data governance anomalies: PII export, bulk downloads, restricted datasets, and cross-region queries.
- API health anomalies: unexpected `4xx`, repeated `5xx`, notification, and escalation.

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

## Curl Flows

```sh
curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"alice"},"session":{"impossible_travel":true},"device":{"trusted":true},"network":{"ip_reputation":10}}' \
  http://127.0.0.1:8083/session/continue

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"buyer-1"},"geo":{"country":"IR","region":"blocked"},"payment":{"amount_vs_avg":1,"velocity_10m":1,"new_method":false,"shipping_country":"IR","billing_country":"IR"}}' \
  http://127.0.0.1:8083/payments/authorize

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"buyer-2"},"geo":{"country":"US","region":"us"},"payment":{"amount_vs_avg":6,"velocity_10m":1,"new_method":false,"shipping_country":"US","billing_country":"US"}}' \
  http://127.0.0.1:8083/payments/authorize

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"analyst-1"},"tenant":{"region":"us","verified":true},"geo":{"country":"DE","region":"eu"},"data":{"pii_export":true,"bulk_rows":25000,"restricted_dataset":true,"cross_region":true}}' \
  http://127.0.0.1:8083/data/query

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

Assets include sessions, payments, orders, support workflows, shipments, PII datasets, and API availability. Threat actors include compromised accounts, fraud operators, malicious insiders, and automation clients. Controls include step-up authentication, transaction challenge, order hold, regional block, data quarantine, session termination, account suspension, notifications, escalation, and risk case creation.

The live `/_threat-model` endpoint returns the same model as JSON for automation and demos.

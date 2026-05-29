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

When `--watch` is enabled, the server watches `decision.bcl` and every imported `*.bcl` file under the policy tree. Valid edits are reloaded and activated automatically; invalid edits keep the last known good policy active.

## Curl Flows

```sh
curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"alice"},"session":{"impossible_travel":true},"device":{"trusted":true},"network":{"ip_reputation":10}}' \
  http://127.0.0.1:8083/session/continue

curl -i -H 'Content-Type: application/json' \
  -d '{"actor":{"id":"acct-1"},"account":{"mfa_disabled":true},"device":{"changed":true,"trusted":false},"network":{"ip_reputation":75}}' \
  http://127.0.0.1:8083/accounts/update-profile

for i in {1..4}; do curl -i -H 'Content-Type: application/json' \
  -d "{\"actor\":{\"id\":\"reg-$i\"},\"account\":{\"email_domain\":\"example$i.com\"},\"network\":{\"ip\":\"198.51.100.7\"}}" \
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

# Condition Examples

These examples are intentionally self-contained. Each directory is a small domain application, not a shared runner with renamed inputs. Each one has:

* `main.go` with typed Go domain objects, use-case-specific enrichment/scoring/planning logic, Condition service setup, and business follow-up behavior.
* `decision.bcl` with the policy, tests, reason codes, and decision table for that use case.

Run any example from the `condition` module:

```sh
go run ./examples/access-control
go run ./examples/payment-risk
go run ./examples/kyc-onboarding
go run ./examples/ai-safety
go run ./examples/logistics-routing
go run ./examples/healthcare-prior-auth
go run ./examples/dynamic-pricing
go run ./examples/procurement-approval
go run ./examples/support-routing
go run ./examples/api-traffic-control
go run ./examples/http-auth-guard
go run ./examples/manufacturing-iot
go run ./examples/hr-leave-approval
go run ./examples/education-scholarship
go run ./examples/data-governance
go run ./examples/content-moderation
go run ./examples/loan-eligibility
go run ./examples/cybersecurity-session-risk
go run ./examples/government-benefits
go run ./examples/marketplace-escrow
go run ./examples/devops-deployment-gate
go run ./examples/telecom-notification-routing
go run ./examples/aml-transaction-monitoring
go run ./examples/insurance-claims
go run ./examples/subscription-entitlements
go run ./examples/crm-churn-upsell
go run ./examples/investment-trade-compliance
go run ./examples/smart-city-traffic
go run ./examples/supply-chain-replenishment
go run ./examples/temporal-behavior-risk
go run ./examples/fraud-ring-graph
go run ./examples/geo-territory-assignment
go run ./examples/event-correlation
go run ./examples/product-recommendation
go run ./examples/autonomous-remediation
```

Production server configuration examples live in `production-server`. They are not Go use-case runners; they show BCL-native runtime config for SQLite, strict validation/evaluation, fail-closed audit behavior, HTTP timeouts, trusted proxies, TLS, rate limits, and route-scoped authz.

## Use Case Coverage

* `access-control`: RBAC/ABAC, session risk, conditional MFA, and break-glass access.
* `payment-risk`: payment authorization, fraud blocking, and step-up authentication.
* `kyc-onboarding`: identity verification, document confidence, biometrics, and sanctions screening.
* `ai-safety`: prompt intent governance, safe routing, refusal, and manual safety review.
* `logistics-routing`: carrier eligibility, hazmat controls, capacity review, and shipment booking.
* `healthcare-prior-auth`: treatment eligibility, clinical safety, documentation, and prior authorization routing.
* `dynamic-pricing`: margin floors, surge review, inventory signals, and loyalty discount approval.
* `procurement-approval`: vendor risk, budget policy, purchase order automation, and approval chains.
* `support-routing`: SLA handling, security escalation, enterprise support, and self-service routing.
* `api-traffic-control`: bot blocking, tenant quotas, throttling, and gateway routing.
* `http-auth-guard`: global HTTP middleware, authentication guardrails, and RBAC/ABAC/ACL endpoint authorization.
* `manufacturing-iot`: sensor thresholds, machine shutdown, maintenance review, and production continuity.
* `hr-leave-approval`: leave balances, staffing coverage, blackout periods, and manager review.
* `education-scholarship`: student eligibility, committee review, and merit/need scholarship awards.
* `data-governance`: legal hold, data residency, PII masking, and governed warehouse access.
* `content-moderation`: classifier scores, copyright matches, creator reputation, and publish/remove routing.
* `loan-eligibility`: credit policy, underwriting review, collateral checks, and loan approval.
* `cybersecurity-session-risk`: impossible travel, device trust, Tor signals, MFA step-up, and session termination.
* `government-benefits`: residency, income thresholds, missing documents, and public benefits eligibility.
* `marketplace-escrow`: dispute handling, seller trust, chargebacks, and payout release decisions.
* `devops-deployment-gate`: tests, security scans, canary health, freeze windows, and production promotion.
* `telecom-notification-routing`: consent, blocked countries, provider quality, fallback routing, and SMS sends.
* `aml-transaction-monitoring`: wire velocity, structuring, sanctions hits, country risk, and EDD/SAR routing.
* `insurance-claims`: claim estimation, duplicate checks, missing evidence, provider risk, and SIU/settlement routing.
* `subscription-entitlements`: plan catalogs, feature flags, quota math, entitlement tokens, and upgrade walls.
* `crm-churn-upsell`: product health, NPS, seat utilization, churn scoring, retention and expansion motions.
* `investment-trade-compliance`: pre-trade restricted lists, suitability, concentration limits, and supervision review.
* `smart-city-traffic`: sensor-derived congestion, emergency preemption, crowd events, and signal plan changes.
* `supply-chain-replenishment`: demand forecasting, days-to-stockout, vendor scorecards, auto-POs, and planner review.
* `temporal-behavior-risk`: rolling-window failed logins, reset/new-payee sequences, spend baselines, and velocity review.
* `fraud-ring-graph`: shared devices, payout instruments, known-bad neighbors, chargeback clusters, and seller payout release.
* `geo-territory-assignment`: geofencing, territory capacity, licensed coverage, data residency, and sales assignment.
* `event-correlation`: login/reset/address-change/purchase sequence detection, order holds, and account locks.
* `product-recommendation`: ranked next-best offers, eligibility gates, inventory, margin policy, and compliance review.
* `autonomous-remediation`: SRE runbook guardrails, blast-radius checks, confidence thresholds, and human-in-the-loop approval.

The examples duplicate their small amount of Condition service bootstrapping on purpose so each one can be copied into a real application without also copying a demo runner. The domain logic is deliberately different across examples: KYC builds evidence packages, support computes queue/SLA routing, payments enrich authorizations, logistics scores carrier lanes, data governance rewrites query access, temporal risk computes rolling windows, graph risk links related entities, recommendations rank offers before applying policy, and autonomous remediation turns observability signals into guarded runbooks.

`http-auth-guard` can also run as a curlable server:

```sh
go run ./examples/http-auth-guard --serve --addr :8080
```

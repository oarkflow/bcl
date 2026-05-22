# Condition Decision Platform Use Cases

These examples are owned by the standalone `condition` module. Each use case publishes a local BCL decision definition into the Condition service, evaluates representative inputs, and records tamper-evident audit envelopes.

Run one use case:

```sh
go run ./examples/condition_decision_platform/use_cases/fraud-aml
```

Run the HTTP service:

```sh
go run ./cmd/condition serve --config ./examples/condition_decision_platform/condition.yaml
```

Publish and evaluate with the CLI:

```sh
go run ./cmd/condition validate --name fraud-aml ./examples/condition_decision_platform/use_cases/fraud-aml/decision.bcl
go run ./cmd/condition publish --name fraud-aml --version 1 ./examples/condition_decision_platform/use_cases/fraud-aml/decision.bcl
go run ./cmd/condition versions fraud-aml
go run ./cmd/condition evaluate fraud-aml --decision fraud_aml --input ./examples/condition_decision_platform/use_cases/fraud-aml/inputs/review.json --compact
go run ./cmd/condition rollback fraud-aml 1
go run ./cmd/condition audits --definition fraud-aml --operation evaluate --limit 10
go run ./cmd/condition audit verify
```

The service also exposes protected metrics at `GET /v1/metrics`; use `X-Roles: condition-auditor` or `condition-admin` in local demos.

The included `condition.yaml` demonstrates SQLite-backed storage, request limits, rate limits, and loading the companion `condition.authz` policy file. Omit `--config` to use the built-in demo roles and the default `.condition` file store.

Workflow use cases can be driven through the service API or HTTP:

```sh
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"input":{"case":{"severity":"critical"},"transaction":{"amount":150000}}}' \
  http://localhost:8080/v1/definitions/case-review-workflow/workflows/manual_review/start
```

Use cases included:

| Use case | Decision ID | Focus |
| --- | --- | --- |
| Fraud AML | `fraud_aml` | Deny blocked customers, review high-risk payments |
| Credit eligibility | `credit_eligibility` | Affordability and credit policy |
| Provider routing | `provider_routing` | Rank active providers |
| Case review workflow | `case_review_workflow` | Queue/stage assignment |
| Offer ranking | `offer_ranking` | Select best eligible offer |
| Compliance approval | `compliance_approval` | Regulated approval controls |
| Feature gating | `feature_gating` | Tenant/user entitlement checks |
| SLA escalation | `sla_escalation` | Escalate operational work |

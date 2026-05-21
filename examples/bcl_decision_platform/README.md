# BCL Decision Platform Catalog

This catalog contains executable decision-platform examples for common production domains. Each use case is a standalone mini-application with its own BCL decision package, schema file, runnable JSON inputs, local usage notes, datasets, release gate, and Go `main.go`.

## Run The Catalog

Compile one package:

```sh
go run ./cmd/bcl compile examples/bcl_decision_platform/use_cases/fraud-aml/decision.bcl
```

Run an individual use-case application:

```sh
go run ./examples/bcl_decision_platform/use_cases/fraud-aml
```

Run the aggregate Go demo across representative use cases:

```sh
go run ./examples/bcl_decision_platform
```

## Go API Usage

The examples are intended to map directly to the public API:

```go
program, err := bcl.CompileDecisionFile("examples/bcl_decision_platform/use_cases/fraud-aml/decision.bcl", &bcl.Options{AllowTime: true})
engine := bcl.NewDecisionEngine(program, nil)
result, err := engine.Evaluate("fraud_aml", input)
batch, err := bcl.EvaluateDecisionDataset(program, "fraud_aml", "fraud_aml_batch", nil)
gates, err := bcl.EvaluateDecisionGates(program, "fraud_aml_bundle", nil)
result, err = bcl.CounterfactualDecision(program, "fraud_aml", input, nil)
observation := bcl.DecisionResultObservation(result, input, nil)
```

## Use Cases

| Use case | Decision ID | Focus |
| --- | --- | --- |
| Fraud and AML review | `fraud_aml` | blacklist, high-value transactions, AML region review |
| Loan eligibility | `loan_eligibility` | affordability, credit score, blacklist controls |
| KYC onboarding | `kyc_onboarding` | identity proofing, sanctions, document quality |
| Support routing | `support_routing` | enterprise escalation, SLA risk, normal routing |
| AI safety | `ai_safety` | blocked intents, high-risk review, safe responses |
| Procurement approval | `procurement_approval` | budget checks, spend thresholds, finance review |
| Communications provider routing | `communications_provider_routing` | SMS/email provider selection, quality, cost, country support |
| Telecom routing | `telecom_routing` | compliance checks, fallback routing, premium route |
| Healthcare prior auth | `healthcare_prior_auth` | coverage, urgent review, medical necessity |
| Insurance claims | `insurance_claims` | fraud indicators, auto-settlement, adjuster review |
| Dynamic pricing | `dynamic_pricing` | margin protection, loyalty discounts, manual review |
| Logistics routing | `logistics_routing` | hazmat restrictions, express routing, capacity review |
| IAM access | `iam_access` | blocked identities, privileged access, normal access |
| Sanctions compliance | `sanctions_compliance` | sanctions hits, export controls, screening review |
| Payment risk | `payment_risk` | stolen cards, 3DS step-up, low-risk approval |

## What Each Package Demonstrates

- Separate `decision.schema` files plus `decision_schema` effect contracts.
- `decision_table` rows with lifecycle metadata, reason codes, tags, outcomes, obligations, and advice.
- `dataset` records for batch evaluation and coverage reporting.
- `gate` and `decision_bundle` metadata for release checks.
- Domain-specific Go `main.go` programs for allow, deny, review, scoring, matching, and ranked routing scenarios.

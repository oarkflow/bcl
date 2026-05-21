# Fraud And AML Review

Evaluates transaction risk before payment execution.

- Decision: `fraud_aml`
- Effects: `allow`, `deny`, `require_review`
- Inputs: `customer.blacklisted`, `customer.country`, `transaction.amount`, `transaction.channel`
- Coverage: blacklist denial, AML review for high-value wires, low-risk straight-through approval, risk scoring, and review-queue ranking
- Run: `go run ./examples/bcl_decision_platform/use_cases/fraud-aml`

Expected highlights: blacklisted customers are denied with `BLACKLISTED_CUSTOMER`, review-market wires above 100000 require review, low-value card transactions are allowed, and AML queues are ranked for review readiness.

# Payment Risk And 3DS Step-Up

Evaluates card payments for decline, step-up authentication, or authorization.

- Decision: `payment_risk`
- Effects: `allow`, `deny`, `require_review`
- Inputs: card stolen/country, payment amount/risk/cross-border flag, trusted customer flag
- Coverage: stolen-card decline, 3DS step-up, low-risk authorization, and authorization-risk scoring
- Run: `go run ./examples/bcl_decision_platform/use_cases/payment-risk`

Expected highlights: stolen cards are denied, risky payments require 3DS, and trusted low-risk payments are authorized.

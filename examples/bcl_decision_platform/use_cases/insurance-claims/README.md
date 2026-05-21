# Insurance Claims Triage

Routes claims using policy status, fraud score, documentation, and amount.

- Decision: `insurance_claims`
- Effects: `allow`, `deny`, `require_review`
- Inputs: policy active/type and claim amount/fraud/documentation/loss-type fields
- Coverage: inactive policy denial, fraud review, auto-settlement, and adjuster ranking
- Run: `go run ./examples/bcl_decision_platform/use_cases/insurance-claims`

Expected highlights: inactive policies are denied, suspicious claims go to an adjuster, small complete low-risk claims auto-settle, and adjusters are ranked by specialty/load.

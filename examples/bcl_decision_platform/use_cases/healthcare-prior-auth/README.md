# Healthcare Prior Authorization

Evaluates care authorization requests using coverage and medical-necessity signals.

- Decision: `healthcare_prior_auth`
- Effects: `allow`, `deny`, `require_review`
- Inputs: member coverage/plan, procedure, urgency, medical necessity score, and prior denial signal
- Coverage: inactive coverage denial, clinical review, authorization, and reviewer ranking
- Run: `go run ./examples/bcl_decision_platform/use_cases/healthcare-prior-auth`

Expected highlights: inactive coverage is denied, urgent or weakly supported care goes to clinical review, medically necessary routine care is authorized, and clinical reviewers are ranked by fit/load.

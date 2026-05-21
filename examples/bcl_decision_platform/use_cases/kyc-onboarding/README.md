# KYC Customer Onboarding

Screens applicants for sanctions, document quality, and identity assurance.

- Decision: `kyc_onboarding`
- Effects: `allow`, `deny`, `require_review`
- Inputs: sanctions match, identity score, document quality, country, and PEP signal
- Coverage: sanctions denial, manual document review, verified onboarding, and assurance scoring
- Run: `go run ./examples/bcl_decision_platform/use_cases/kyc-onboarding`

Expected highlights: sanctions matches are blocked and audited, weak evidence is routed to KYC review, and high-confidence identities are approved.

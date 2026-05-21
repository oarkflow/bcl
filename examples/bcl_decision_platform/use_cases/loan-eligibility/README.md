# Loan Eligibility And Credit Policy

Evaluates consumer loan requests using blacklist, credit-score, and affordability rules.

- Decision: `loan_eligibility`
- Effects: `allow`, `deny`, `require_review`
- Inputs: applicant age, credit score, monthly income, blacklist flag, loan EMI and amount
- Coverage: blocked applicant, borderline manual review, prime automatic approval, and underwriting score bands
- Run: `go run ./examples/bcl_decision_platform/use_cases/loan-eligibility`

Expected highlights: blacklist denial takes priority, borderline credit asks for income proof, and prime affordable applications are routed straight through.

# IAM Access Authorization

Authorizes resource access based on identity status, role, MFA, and resource sensitivity.

- Decision: `iam_access`
- Effects: `allow`, `deny`, `require_review`
- Inputs: subject blocked/role/MFA, resource sensitivity/required role, request action
- Coverage: blocked denial, privileged review, role-based grant, and access-risk scoring
- Run: `go run ./examples/bcl_decision_platform/use_cases/iam-access`

Expected highlights: blocked identities are denied and audited, privileged access requires review, and normal matching-role access is granted.

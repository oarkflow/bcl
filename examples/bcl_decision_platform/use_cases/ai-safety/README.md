# AI Safety And Content Moderation

Applies model-response guardrails to user intent and safety scores.

- Decision: `ai_safety`
- Effects: `allow`, `deny`, `require_review`
- Inputs: intent, risk score, and classifier confidence
- Coverage: blocked intent refusal, high-risk review, safe answer, and safety-risk scoring
- Run: `go run ./examples/bcl_decision_platform/use_cases/ai-safety`

Expected highlights: disallowed intent is denied and audited, uncertain high-risk requests are reviewed, benign requests are allowed, and classifier signals contribute a safety score.

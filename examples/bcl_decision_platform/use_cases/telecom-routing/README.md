# Telecom Carrier Routing

Routes outbound messages over telecom carrier paths based on compliance, destination support, and network health.

- Decision: `telecom_routing`
- Effects: `allow`, `deny`, `require_review`
- Inputs: recipient country, message priority/compliance, provider active/support flags
- Coverage: compliance block, fallback review, premium route, and carrier-path ranking
- Run: `go run ./examples/bcl_decision_platform/use_cases/telecom-routing`

Expected highlights: unchecked messages are blocked, unsupported routes trigger fallback review, compliant high-priority messages use premium routing, and carrier paths are ranked by delivery/cost.

# Communications Provider Routing

Selects the best SMS or email provider from a routing dataset after compliance and market checks.

- Decision: `communications_provider_routing`
- Inputs: user country/tier, message channel/type, compliance status, and required quality floor
- Coverage: unchecked message denial, SMS provider ranking, email provider ranking, unsupported market review
- Run: `go run ./examples/bcl_decision_platform/use_cases/communications-provider-routing`

Expected highlights: compliant Nepal SMS traffic selects `sms-np-primary`, compliant US email selects `email-us-primary`, and unsupported destinations route to provider fallback review.

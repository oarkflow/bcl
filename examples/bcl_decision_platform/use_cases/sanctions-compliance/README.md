# Sanctions And Export Compliance

Screens counterparties and shipments before trade or fulfillment.

- Decision: `sanctions_compliance`
- Effects: `allow`, `deny`, `require_review`
- Inputs: sanctions hit, watchlist score, counterparty country, export-control flag, destination, and product code
- Coverage: sanctions block, export review, clear-party approval, and watchlist/country risk scoring
- Run: `go run ./examples/bcl_decision_platform/use_cases/sanctions-compliance`

Expected highlights: sanctions hits are blocked and audited, export-controlled shipments require review, and clear transactions proceed.

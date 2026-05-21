# Dynamic Pricing And Promotions

Evaluates requested discounts against margin, loyalty, and approval policy.

- Decision: `dynamic_pricing`
- Effects: `allow`, `deny`, `require_review`
- Inputs: customer segment/loyalty, cart discount/margin fields, and inventory demand
- Coverage: margin denial, pricing review, loyalty discount approval, and pricing-risk scoring
- Run: `go run ./examples/bcl_decision_platform/use_cases/dynamic-pricing`

Expected highlights: discounts below margin floor are denied, large discounts are reviewed, moderate loyalty discounts are applied, and demand/margin signals contribute pricing risk.

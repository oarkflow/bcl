# Procurement Approval

Approves, declines, or routes procurement requests based on budget, vendor status, and amount.

- Decision: `procurement_approval`
- Effects: `allow`, `deny`, `require_review`
- Inputs: request amount/category/department, available budget, approved vendor flag, and vendor risk tier
- Coverage: budget denial, finance review, small-request approval, and approver-queue ranking
- Run: `go run ./examples/bcl_decision_platform/use_cases/procurement-approval`

Expected highlights: unfunded spend is denied, large or new-vendor spend gets finance review, small funded requests are approved, and approver queues are ranked by fit/load.

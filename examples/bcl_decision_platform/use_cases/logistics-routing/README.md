# Logistics Carrier Routing

Chooses fulfillment routes based on hazmat status, capacity, weight, and service level.

- Decision: `logistics_routing`
- Effects: `allow`, `deny`, `require_review`
- Inputs: shipment hazmat/weight/service/destination and carrier capability flags
- Coverage: hazmat denial, capacity review, express routing, and carrier-route ranking
- Run: `go run ./examples/bcl_decision_platform/use_cases/logistics-routing`

Expected highlights: uncertified hazmat carriers are blocked, heavy or capacity-constrained shipments are reviewed, express shipments route automatically, and route candidates are ranked.

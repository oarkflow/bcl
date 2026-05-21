# Support Ticket Routing

Routes customer support tickets by abuse status, severity, and customer plan.

- Decision: `support_routing`
- Effects: `allow`, `deny`, `require_review`
- Inputs: ticket severity, age, abuse flag, topic, and customer plan
- Coverage: abuse block, critical enterprise escalation, standard routing, and support-queue ranking
- Run: `go run ./examples/bcl_decision_platform/use_cases/support-routing`

Expected highlights: abuse is audited, critical enterprise tickets page support leadership, routine tickets go to the general support queue, and queues are ranked by skill/load.

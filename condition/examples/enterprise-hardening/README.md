# Enterprise Hardening Example

This example shows the production hardening surface in one place:

- tenant-partitioned publish/evaluate/canary flows
- deterministic runtime time with `fixed_time`
- deny-by-default external dataset policy with an explicit `file` adapter allowlist
- audit metadata that includes tenant, runtime policy, decision outcome, and canary details

Run the Go example:

```sh
go run ./examples/enterprise-hardening
```

Run the same shape through the CLI:

```sh
go run ./cmd/condition publish --config ./examples/enterprise-hardening/condition.bcl --tenant acme --name enterprise-hardening --version 1 --tests ./examples/enterprise-hardening/decision.bcl
go run ./cmd/condition evaluate --config ./examples/enterprise-hardening/condition.bcl --tenant acme --decision access --input ./examples/enterprise-hardening/inputs/request.json enterprise-hardening
go run ./cmd/condition canary --config ./examples/enterprise-hardening/condition.bcl --tenant acme --candidate ./examples/enterprise-hardening/candidate.bcl --decision access --dataset canary_cases --promote --promote-version 2 enterprise-hardening
```

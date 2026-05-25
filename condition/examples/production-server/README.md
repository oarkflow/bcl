# Production Server Example

This example uses BCL for Condition runtime configuration and the authz DSL for route permissions.

## Files

- `condition.bcl`: hardened HTTPS server config with SQLite, strict validation/evaluation, approval gates, rate limits, trusted proxy CIDRs, and TLS file paths.
- `condition.local.bcl`: local HTTP variant with the same production safety switches, useful when TLS is terminated by a local proxy or gateway.
- `condition.authz`: route-scoped roles for admins, publishers, operators, simulators, and auditors.

## Run Locally

From the `condition` module:

```sh
go run ./cmd/condition serve --config ./examples/production-server/condition.local.bcl
```

Check readiness:

```sh
curl -H 'X-Roles: condition-auditor' http://localhost:8080/v1/readiness
```

Publish a strict, tested definition:

```sh
go run ./cmd/condition publish \
  --config ./examples/production-server/condition.local.bcl \
  --name payment-risk \
  --version 1 \
  --tests \
  ./examples/payment-risk/decision.bcl
```

Because activation approval is required, approve and activate the version:

```sh
curl -H 'X-Roles: condition-publisher' \
  -H 'Content-Type: application/json' \
  -d '{"approved_by":"risk-owner","reason":"reviewed strict tests"}' \
  http://localhost:8080/v1/definitions/payment-risk/versions/1/approve

curl -H 'X-Roles: condition-publisher' \
  -X POST \
  http://localhost:8080/v1/definitions/payment-risk/versions/1/activate
```

Evaluate through the HTTP API:

```sh
curl -H 'X-Roles: condition-operator' \
  -H 'X-Subject-ID: svc-payments' \
  -H 'X-Tenant-ID: tenant-a' \
  -H 'Content-Type: application/json' \
  -d '{"decision":"payment_risk","input":{"card":{"stolen":false},"payment":{"risk_score":12,"cross_border":false},"customer":{"trusted":true}}}' \
  http://localhost:8080/v1/definitions/payment-risk/evaluate
```

Verify the audit chain:

```sh
go run ./cmd/condition audit verify --config ./examples/production-server/condition.local.bcl
```

## HTTPS Variant

`condition.bcl` expects certificate files at `./examples/production-server/tls/dev.crt` and `./examples/production-server/tls/dev.key`. Generate development-only certificates however your environment normally does, then run:

```sh
go run ./cmd/condition serve --config ./examples/production-server/condition.bcl
```

In a real deployment, keep TLS keys outside the repository, point `tls.cert_file` and `tls.key_file` to managed secret mounts, and put only trusted gateway CIDRs in `trusted_proxies`.

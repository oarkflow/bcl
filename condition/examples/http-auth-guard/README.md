# HTTP Auth Guard Example

This is the full multi-file Condition HTTP guard example. It demonstrates:

- route catalogs for public, admin, project, document, tenant API, and failure routes
- `lifecycle "http_request"` with `pre` authorization/rate-limit and `post` response observability
- lifecycle request/response envelopes with headers, body, and format facts
- normalized body aliases: `body_json`, `body_form`, `body_text`, `body_raw`, and `body_format`
- stateful `chain`/`watch`/`step` escalation with cooldown, decay scoring, and reset events
- RBAC, ABAC, ACL, endpoint guard, auth guard, and response observability decisions
- policy package metadata, action catalog, output contract, standard facts, and policy overlays
- BCL `test` and `lifecycle_test` coverage
- curlable action history, incident history, route coverage, and readiness endpoints

Run the demo:

```sh
go run ./examples/http-auth-guard
```

Run it as a server:

```sh
go run ./examples/http-auth-guard --serve --addr :8080
```

Inspect the policy surface:

```sh
curl -i http://127.0.0.1:8080/_rules
curl -s http://127.0.0.1:8080/_coverage
curl -s http://127.0.0.1:8080/_actions
curl -s http://127.0.0.1:8080/_incidents
curl -s http://127.0.0.1:8080/_readiness
```

Exercise pre-request policies:

```sh
curl -i http://127.0.0.1:8080/public/status
curl -i -H "Authorization: Bearer admin-token" http://127.0.0.1:8080/admin/reports
curl -i -H "Authorization: Bearer analyst-token" http://127.0.0.1:8080/admin/reports
curl -i -H "Authorization: Bearer analyst-token" http://127.0.0.1:8080/projects/acme/export
curl -i -H "Authorization: Bearer analyst-token" http://127.0.0.1:8080/projects/other/export
curl -i -H "Authorization: Bearer viewer-token" http://127.0.0.1:8080/documents/alpha
curl -i -H "Authorization: Bearer viewer-token" http://127.0.0.1:8080/documents/beta
curl -i -X PATCH -H "Authorization: Bearer analyst-token" http://127.0.0.1:8080/api/v1/tenants/acme/users/u-analyst
curl -i -X PATCH -H "Authorization: Bearer admin-token" http://127.0.0.1:8080/api/v1/tenants/acme/users/u-analyst
```

Exercise stateful escalation:

```sh
for i in {1..7}; do curl -i -H "Authorization: Bearer analyst-token" http://127.0.0.1:8080/api/v1/tenants/acme/users/u-analyst; done
for i in {1..8}; do curl -i -H "Authorization: Bearer admin-token" http://127.0.0.1:8080/admin/reports; done
for i in {1..5}; do curl -i http://127.0.0.1:8080/fail/endpoint; done
for i in {1..5}; do curl -i http://127.0.0.1:8080/fail/app/database; done
```

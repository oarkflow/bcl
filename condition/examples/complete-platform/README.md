# Complete Platform Example

This example demonstrates the full native Condition platform surface with a runnable GoFiber application:

- `policy_package`, `action_catalog`, `output_contract`, and `standard_facts`
- route catalogs and policy overlays
- request/response envelopes with JSON, form, and text bodies
- BCL-owned detection, event derivation, enforcement status, retry headers, and response bodies
- response classification for expected client errors, unexpected `4xx`, and `5xx`
- stateful chains with TTL based progression, decay, reset, composite watches, and suppression
- reusable result references where chain steps own structured `response { body { ... } }` envelopes and pre-request decisions use `attributes { result "..." }`
- lifecycle `pre`, `post`, and `error` phases
- BCL `test` and `lifecycle_test` blocks
- curlable use cases for login, admin access, documents, endpoint errors, app errors, actions, incidents, readiness, and route coverage

Run the demo once:

```sh
go run ./examples/complete-platform
```

Run it as a Fiber server:

```sh
go run ./examples/complete-platform --serve --addr :8082
```

Validate only the policy package:

```sh
go run ./cmd/condition validate --name complete-platform ./examples/complete-platform/decision.bcl
```

## Policy Layout

`decision.bcl` is intentionally only the entrypoint now. The platform behavior is split by ownership and purpose:

```text
decision.bcl
rules/package.bcl
rules/catalogs.bcl
rules/routes.bcl
rules/overlays.bcl
rules/decisions/pre_request_guard.bcl
rules/decisions/response_observability.bcl
rules/endpoints/login.bcl
rules/endpoints/admin_reports.bcl
rules/endpoints/documents.bcl
rules/endpoints/failures.bcl
rules/chains/account_risk.bcl
rules/chains/response_errors.bcl
rules/lifecycle.bcl
tests/lifecycle_tests.bcl
```

## Use Cases

- Login observability: `POST /login` accepts JSON and form bodies. Failed logins emit `failed_login`; repeated failures move through `rate_limit_2m`, `rate_limit_5m`, `block_30m`, `suspend_24h`, and `lock_ban`.
- Test credentials: `alice/correct` and `bob/correct` are valid; unknown users and wrong passwords are rejected.
- Progressive jitter: attempts during an active cooldown are blocked by `pre` middleware and do not advance the ladder. After the current TTL expires, two invalid logins are grace failures and the third invalid login advances to the next step.
- Pre-request enforcement: active `rate_limit`, `block`, `suspend`, and `ban` state is enforced by Fiber middleware before the handler runs.
- Result references: `rules/chains/account_risk.bcl` defines each response body once on the step, and `rules/decisions/pre_request_guard.bcl` references those responses by stable result IDs such as `login.rate_limit.2m`.
- Thin host integration: Fiber parses HTTP facts and applies the Condition enforcement envelope; policy conditions, event names, status codes, retry seconds, and response body messages live in imported BCL rule files.
- Admin authorization: `GET /admin/reports` emits `admin_denied` for non-admin actors and contributes to account risk correlation.
- Document routing: `GET /documents/:document_id` demonstrates native route matching and unexpected `4xx` detection.
- Endpoint observability: `GET /fail/endpoint` emits endpoint-scoped `endpoint_5xx` and escalates through `notify`/`escalate`.
- App-wide observability: `GET /fail/app/:component` emits app-level `app_5xx` for broader incident grouping.
- Operations: `/_coverage`, `/_actions`, `/_incidents`, and `/_readiness` expose what the platform recorded.
- State inspection: `/_state?actor=alice` and `/_events?actor=alice` show durable watch state and events.

## Curl The Fiber App

```sh
curl -s http://127.0.0.1:8082/

curl -i -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"correct"}' \
  http://127.0.0.1:8082/login

curl -i -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"bad"}' \
  http://127.0.0.1:8082/login

curl -i -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"bad"}' \
  http://127.0.0.1:8082/login

curl -i -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"correct"}' \
  http://127.0.0.1:8082/login

curl -i -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"bad"}' \
  http://127.0.0.1:8082/login

curl -i -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=password&username=alice&password=bad' \
  http://127.0.0.1:8082/login

for i in {1..3}; do
  curl -i -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"bad"}' \
    http://127.0.0.1:8082/login
done

sleep 121
for i in {1..3}; do
  curl -i -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"bad"}' \
    http://127.0.0.1:8082/login
done

sleep 301
for i in {1..3}; do
  curl -i -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"bad"}' \
    http://127.0.0.1:8082/login
done

sleep 1801
for i in {1..3}; do
  curl -i -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"bad"}' \
    http://127.0.0.1:8082/login
done

sleep 86401
for i in {1..3}; do
  curl -i -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"bad"}' \
    http://127.0.0.1:8082/login
done

curl -i -H 'X-Actor: analyst-1' -H 'X-Role: analyst' \
  http://127.0.0.1:8082/admin/reports

curl -i -H 'X-Actor: admin-1' -H 'X-Role: admin' \
  http://127.0.0.1:8082/admin/reports

curl -i -H 'X-Document-Access: allow' \
  http://127.0.0.1:8082/documents/doc-123

curl -i http://127.0.0.1:8082/documents/missing

for i in {1..5}; do curl -i http://127.0.0.1:8082/fail/endpoint; done
for i in {1..5}; do curl -i http://127.0.0.1:8082/fail/app/database; done

curl -s http://127.0.0.1:8082/_coverage
curl -s 'http://127.0.0.1:8082/_state?actor=alice'
curl -s 'http://127.0.0.1:8082/_events?actor=alice'
curl -s http://127.0.0.1:8082/_actions
curl -s http://127.0.0.1:8082/_incidents
curl -s http://127.0.0.1:8082/_readiness
```

Expected login escalation for `alice`:

- first bad attempts return `401` with `failed_login`
- the third bad attempt is immediately rewritten to `429 rate_limit` with `rate_limit_2m`
- attempts during the 2 minute cooldown are stopped by pre middleware and do not count as new failures
- after the 2 minute cooldown, two more bad requests are grace failures and the third advances to `rate_limit_5m`
- after the 5 minute cooldown, two more bad requests are grace failures and the third advances to `block_30m`
- after the 30 minute block, two more bad requests are grace failures and the third advances to `suspend_24h`
- after the 24 hour suspension, two more bad requests are grace failures and the third advances to `423 ban` with `lock_ban`
- `/_state?actor=alice` shows the active watch state and counters

Every application response includes Condition headers when lifecycle evaluation succeeds:

- `X-Condition-Action`
- `X-Condition-Reason`
- `X-Condition-Route`
- `X-Condition-State`
- `X-Condition-Severity`

## Server API Mode

You can also publish the same policy to a running Condition server:

```sh
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"name":"complete-platform","version":"1","path":"./examples/complete-platform/decision.bcl","run_tests":true}' \
  http://localhost:8080/v1/definitions
```

Evaluate a JSON login failure:

```sh
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"phase":"post","method":"POST","path":"/login","request":{"headers":{"content_type":"application/json"},"body":{"username":"alice"},"format":"json"},"response":{"status":401,"headers":{"content_type":"application/json"},"body":{"error":"invalid_password"},"format":"json"},"input":{"request":{"actor_key":"alice"}}}' \
  http://localhost:8080/v1/definitions/complete-platform/lifecycles/http_request/evaluate
```

Evaluate a form login failure:

```sh
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"phase":"post","method":"POST","path":"/login","request":{"headers":{"content_type":"application/x-www-form-urlencoded"},"body":{"grant_type":"password","username":"alice"},"format":"form"},"response":{"status":401,"headers":{"content_type":"text/plain"},"body":"invalid password","format":"text"},"input":{"request":{"actor_key":"alice"}}}' \
  http://localhost:8080/v1/definitions/complete-platform/lifecycles/http_request/evaluate
```

Inspect operations through the Condition server:

```sh
curl -H 'X-Roles: condition-admin' http://localhost:8080/v1/definitions/complete-platform/route-coverage
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/actions?definition=complete-platform&limit=20'
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/incidents?definition=complete-platform&limit=20'
```

# Request Lifecycle Example

This example shows native Condition lifecycle evaluation for post-response observability:

- native `routes` catalog and route matching
- `lifecycle "http_request"` with `post` phase
- request and response envelopes with headers, body, and format facts
- normalized body aliases: `body_json`, `body_form`, `body_text`, `body_raw`, and `body_format`
- response classification where `400`, `401`, and `403` are expected client outcomes
- unexpected `4xx`, endpoint `5xx`, and app-wide `5xx` escalation
- `chain`/`watch`/`step` thresholds with durable action records and incidents
- `policy_package`, `policy_overlay`, `action_catalog`, `output_contract`, `standard_facts`, and `lifecycle_test`

Run the demo:

```sh
go run ./examples/request-lifecycle
```

Run it as a server:

```sh
go run ./examples/request-lifecycle --serve --addr :8081
```

Curl the main paths:

```sh
curl -i http://127.0.0.1:8081/ok
curl -i http://127.0.0.1:8081/bad-request
curl -i http://127.0.0.1:8081/unauthorized
curl -i http://127.0.0.1:8081/forbidden
curl -i http://127.0.0.1:8081/teapot
for i in {1..5}; do curl -i http://127.0.0.1:8081/endpoint-error; done
for i in {1..5}; do curl -i http://127.0.0.1:8081/app-error/db; done
```

Inspect platform output:

```sh
curl -s http://127.0.0.1:8081/_coverage
curl -s http://127.0.0.1:8081/_actions
curl -s http://127.0.0.1:8081/_incidents
curl -s http://127.0.0.1:8081/_readiness
```

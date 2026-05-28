# Condition Platform Guide

This guide explains the native Condition platform features added around route matching, lifecycle evaluation, stateful chains, action delivery, incident handling, policy contracts, and developer tooling.

Condition is not only a decision table runner. It is a policy platform for evaluating business, security, abuse, reliability, and operational rules against durable state and request lifecycle facts.

## What You Can Build

Condition can be used for:

- HTTP authorization before a request reaches a handler.
- Rate limiting, abuse detection, temporary blocks, session suspension, and account escalation.
- Post-response observability, including endpoint-level and app-wide `5xx` escalation.
- Unexpected `4xx` tracking where `400`, `401`, and `403` are treated as expected client outcomes.
- Cross-condition escalation, where one policy reacts to actions emitted by another policy.
- Tenant/environment/route metadata overlays without duplicating base route catalogs.
- Durable action history, incident grouping, audit, route coverage, and state compaction.
- BCL-native lifecycle scenarios for pre/post/error request flows.

## Core Authoring Blocks

### `policy_package`

Declares who owns a policy bundle and what capabilities it expects.

```bcl
policy_package "security" {
  owner "platform-security"
  domain "http-authorization"
  version "1.0.0"
  capabilities ["routes", "lifecycle", "state", "actions"]
  routes ["http"]
  actions ["allow", "deny", "notify", "escalate"]
  state true
  external false
}
```

Purpose:

- Makes ownership explicit.
- Documents whether policies use state, actions, routes, lifecycles, or external sinks.
- Gives validation and review systems a stable manifest.

Effect:

- Validation rejects incomplete manifests such as missing owner/domain.
- Package diff/explain can summarize route/action/lifecycle/chain changes.

### `action_catalog`

Defines the actions a policy is allowed to emit.

```bcl
action_catalog "default" {
  action "allow" { sink "event" severity "low" }
  action "notify" { sinks ["event", "webhook"] severity "high" approval "required" retries 3 }
  action "suspend" { sink "event" severity "critical" approval "required" retries 1 }
}
```

Purpose:

- Prevents misspelled or undocumented actions.
- Captures severity, retry policy, and approval requirements.
- Separates policy intent from runtime execution.

Effect:

- Emitted actions always become durable events.
- External execution is safe by default and requires runtime allowlists.
- Delivery records capture handled/error/retry/dead-letter status.

### `output_contract`

Constrains action names and severities emitted by decisions, chains, and lifecycles.

```bcl
output_contract "guard_output" {
  actions ["allow", "deny", "notify", "escalate"]
  severities ["low", "medium", "high", "critical"]
}
```

Purpose:

- Keeps decision outputs predictable.
- Lets reviewers understand the effect surface of a policy.

Effect:

- Validation rejects unknown actions when contracts and catalogs are attached.

### `routes` and `route`

Declare a native route catalog.

```bcl
routes "http" {
  route "document" {
    method "GET"
    pattern "/documents/{document_id}"
    metadata { policy_type "acl" category "documents" }
  }

  route "admin" {
    method "GET"
    pattern "/admin/{path...}"
    metadata { policy_type "rbac" category "admin" }
  }
}
```

Supported route syntax:

- `/x/{id}`
- `/x/:id`
- `*`
- `*rest`
- `{rest...}`

Purpose:

- Gives policies normalized route facts.
- Makes route coverage measurable.
- Avoids slow ad hoc route scans in hot paths.

Effect:

- Condition compiles routes into method-specific matchers.
- Lifecycle evaluation injects `route.id`, `route.params`, `route.metadata`, `route.route_template`, and normalized pattern facts.
- Diagnostics catch duplicate route IDs, duplicate method+pattern pairs, invalid catch-alls, and ambiguous route shapes.

### `policy_overlay`

Applies deterministic metadata overrides by layer.

```bcl
policy_overlay "tenant_strict" {
  layer "tenant"
  tenant "tenant-a"
  environment "production"
  metadata { risk_tier "strict" owner "security" }
}

policy_overlay "admin_owner" {
  layer "route"
  route "admin"
  metadata { escalation_path "security-oncall" }
}
```

Supported layers:

- `global`
- `environment`
- `tenant`
- `endpoint`
- `route`

Purpose:

- Avoids duplicating rules for every environment or tenant.
- Lets platform teams ship global defaults while tenants/routes customize metadata.

Effect:

- Applied in deterministic order: global, environment, tenant, endpoint, route.
- Overlay metadata is merged into route facts before decisions run.

### `lifecycle` and `phase`

Defines request lifecycle evaluation.

```bcl
lifecycle "http_request" {
  entity "request.actor_key"
  routes "http"

  phase "pre" {
    decision "global_guard"
    chain "request_chain"
  }

  phase "post" {
    decision "response_observability"
    chain "response_error_chain"
  }

  phase "error" {
    decision "error_observability"
  }
}
```

Purpose:

- Runs policies at request phases.
- Supports pre-request guardrails and post-response/error observability.
- Provides a single API for HTTP, jobs, messages, deployments, or custom lifecycles.

Effect:

- `pre` can allow, deny, rate-limit, block, or emit actions.
- `post` can evaluate response status/body/headers/duration.
- `error` can inspect panic, timeout, cancellation, or upstream failure facts.
- Final action uses conflict rules: deny/block/suspend override allow; otherwise highest severity wins; equal severity favors later chain steps.

## Request And Response Envelopes

Lifecycle requests can carry structured request and response envelopes.

```json
{
  "phase": "post",
  "method": "POST",
  "path": "/login",
  "request": {
    "headers": {
      "content_type": "application/json",
      "x_request_id": "req-123"
    },
    "body": {
      "username": "alice"
    },
    "format": "json"
  },
  "response": {
    "status": 500,
    "headers": {
      "content_type": "application/json"
    },
    "body": {
      "error": "upstream timeout"
    },
    "format": "json"
  },
  "input": {
    "request": {
      "actor_key": "alice"
    }
  }
}
```

Normalized facts:

- `request.headers.*`
- `request.body`
- `request.body_json`
- `request.body_form`
- `request.body_text`
- `request.body_raw`
- `request.body_format`
- `request.content_type`
- `response.headers.*`
- `response.body`
- `response.body_json`
- `response.body_form`
- `response.body_text`
- `response.body_raw`
- `response.body_format`
- `response.content_type`

Formats:

- `json`
- `form`
- `multipart`
- `text`
- `xml`
- `html`
- `unknown` or raw

Example BCL:

```bcl
row "json-login-failure" {
  when {
    all {
      request.body_json.username != ""
      response.body_json.error == "invalid_password"
    }
  }
  then { decision require_review reason "login failure" attributes { action "failed_login" } metadata { severity "medium" } }
}

row "form-login-failure" {
  when {
    all {
      request.body_format == "form"
      request.body_form.grant_type == "password"
      response.status == 401
    }
  }
  then { decision require_review reason "form login failed" attributes { action "failed_login" } metadata { severity "medium" } }
}
```

## Response Classification

Use `response_classifier` to convert HTTP status into stable facts.

```bcl
response_classifier "http" {
  healthy_below 400
  unhealthy_at_or_above 500
  expected_client_statuses [400, 401, 403]
}
```

Purpose:

- Separates expected client outcomes from unhealthy responses.
- Lets post-response rules reason on `response.class` instead of hardcoding every status.

Effect:

- `400`, `401`, and `403` are expected by default.
- Other `4xx` statuses are unhealthy unless configured otherwise.
- `5xx` statuses are `server_error`.

Generated facts:

- `response.class`
- `response.healthy`
- `response.unhealthy`
- `response.expected_client_error`

## Stateful Chains, Watches, And Steps

Use `chain`, `watch`, and `step` for durable progressive behavior.

```bcl
chain "auth_risk_chain" {
  entity "request.actor_key"
  decision "login_guard"

  watch "failed_logins" {
    event "failed_login"
    window "10m"
    decay "30m"
    cooldown "2m"
    reset "successful_login"

    step "rate_limit" { threshold 3 action "rate_limit" severity "medium" ttl "5m" }
    step "warn" { threshold 5 action "warning" severity "high" ttl "10m" }
    step "block" { threshold 6 action "block" severity "critical" ttl "30m" }
  }
}
```

Purpose:

- Turns single decisions into progressive stateful policies.
- Supports escalation based on repeated events.
- Supports cool-down and reset behavior after remediation.

Effect:

- Events are persisted by tenant/entity/chain/watch.
- State records keep counters, current step/action, severity, attributes, metadata, and expiry.
- TTL/window expiry resets behavior automatically.
- `decay` computes a decaying `risk_score` in watch metadata.
- `cooldown` suppresses repeated generated actions for the same active step.
- `reset` clears state when a reset event appears in the window.

### Composite Watches

```bcl
watch "account_takeover" {
  events ["failed_login", "admin_denied", "new_device"]
  window "30m"
  suppress true
  step "notify" { threshold 1 action "notify" severity "high" ttl "15m" }
}
```

Purpose:

- Detects correlated behavior, not only repeated identical events.

Effect:

- Threshold count is the minimum count across all required events.
- `suppress true` prevents repeated notifications during an active window.

### Analytics Watches

```bcl
watch "latency" {
  event "response_observed"
  window "10m"
  distinct "attributes.user_id"
  field "attributes.duration_ms"
  metrics ["count", "rate", "min", "max", "avg", "p95", "p99"]

  step "slow" { threshold 10 action "notify" severity "high" ttl "15m" }
}
```

Generated attributes:

- `count`
- `rate_per_minute`
- `distinct_count`
- `min`
- `max`
- `avg`
- `p95`
- `p99`

## Lifecycle Tests

`lifecycle_test` blocks test whole request/response/error sequences.

```bcl
lifecycle_test "post json error response" {
  lifecycle "http_request"
  phase "post"
  method "POST"
  path "/login"

  input {
    request.actor_key "alice"
  }

  request {
    headers { content_type "application/json" x_request_id "req-123" }
    body { username "alice" }
    format "json"
  }

  response {
    status 401
    headers { content_type "application/json" }
    body { error "invalid_password" }
    format "json"
  }

  expect {
    final_action "failed_login"
    route "login"
  }
}
```

Purpose:

- Makes lifecycle policies testable in BCL.
- Tests route matching, request facts, response facts, decisions, chains, and final action.

Effect:

- `Publish(... RunTests: true)` fails if lifecycle scenarios fail.
- Test results are included in the Condition test report.

## Actions, Delivery, And Incidents

Every emitted action is persisted. External execution is opt-in.

Runtime allowlist example in Go:

```go
svc := condition.NewService(store, condition.Config{
  Runtime: condition.RuntimePolicy{
    ActionAllowlists: []condition.ActionAllowlist{
      {
        TenantID: "tenant-a",
        Environment: "production",
        Actions: []string{"notify", "escalate"},
        Sinks: []string{"event", "webhook"},
      },
    },
  },
})
```

Purpose:

- Avoids unsafe external side effects from policy text alone.
- Makes action delivery auditable.

Effect:

- Action delivery records track status, attempts, next retry, max attempts, and dead-letter state.
- High-severity actions can create or update incident records.
- `dry_run` lifecycle evaluations persist dry-run delivery records without external side effects.

## Storage, Retention, And Readiness

Condition supports memory, file, SQLite, and shared storage conformance behavior for policy state.

State compaction request:

```json
{
  "definition": "request-lifecycle",
  "environment": "production",
  "before": "2026-05-28T00:00:00Z",
  "delete_open_incidents": false,
  "delete_active_deliveries": false
}
```

Purpose:

- Keeps event/action/incident tables bounded.
- Removes expired chain state and old event history.

Effect:

- Compaction returns deleted counts for events, states, actions, and incidents.
- Readiness includes state cardinality and failed action/open incident checks.

## Runtime APIs

Evaluate a lifecycle:

```sh
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"phase":"post","method":"POST","path":"/login","request":{"headers":{"content_type":"application/json"},"body":{"username":"alice"},"format":"json"},"response":{"status":401,"headers":{"content_type":"application/json"},"body":{"error":"invalid_password"},"format":"json"},"input":{"request":{"actor_key":"alice"}}}' \
  http://localhost:8080/v1/definitions/security/lifecycles/http_request/evaluate
```

Inspect route coverage:

```sh
curl -H 'X-Roles: condition-admin' \
  http://localhost:8080/v1/definitions/security/route-coverage
```

Inspect actions and incidents:

```sh
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/actions?action=notify&limit=20'
curl -H 'X-Roles: condition-admin' 'http://localhost:8080/v1/incidents?status=open&limit=20'
```

Compact state:

```sh
curl -H 'X-Roles: condition-admin' -H 'Content-Type: application/json' \
  -d '{"definition":"security","before":"2026-05-28T00:00:00Z"}' \
  http://localhost:8080/v1/state/compact
```

## Complete Examples

- `examples/request-lifecycle`: compact lifecycle observability example.
- `examples/http-auth-guard`: multi-file HTTP authorization and observability example.
- `examples/complete-platform`: single-file reference that demonstrates the full feature set.

## VSCode Extension

The VSCode extension provides:

- Snippets for route catalogs, lifecycle phases, chains, watches, policy packages, overlays, catalogs, contracts, response classifiers, and lifecycle tests.
- Completions for route IDs, lifecycle IDs, phase names, chain IDs, action IDs, watch fields, and lifecycle envelope fields.
- Hover details for normalized route patterns, lifecycle phase flow, watches, overlays, and action delivery behavior.
- Commands for route coverage, lifecycle playground, state compaction, and opening reference examples.

Here are high-value next features that would make Condition feel like a serious, production-grade decision and policy platform rather than just a rules engine.

**Core Policy Model**
- **Policy packages/modules**: versioned reusable bundles for auth, abuse, observability, fraud, compliance, routing, etc.
- **Policy inheritance/overrides**: global defaults, tenant overrides, endpoint overrides, environment overrides.
- **Policy capabilities manifest**: each definition declares what it needs: state, routes, lifecycle, webhooks, datasets, time, external HTTP.
- **Typed policy inputs/outputs**: first-class schemas for `request`, `response`, `principal`, `route`, `tenant`, `resource`, `risk`, `state`.
- **Policy contracts**: enforce allowed effects/actions/severities per decision or chain.
- **Action catalog**: define allowed actions such as `allow`, `deny`, `rate_limit`, `log`, `notify`, `escalate`, `suspend`, with metadata and execution rules.

**Stateful Decisions**
- **Sliding counters**: native count, distinct count, rate, error ratio, percentile latency.
- **Aggregations over windows**: `count(status >= 500) over 5m by route.id`.
- **Decay/scoring state**: risk scores that cool down over time.
- **Composite watches**: trigger when multiple event types happen together, e.g. `admin_denied + failed_login + new_device`.
- **Suppression/dedup windows**: avoid notifying repeatedly for the same incident.
- **State reset actions**: allow policies to clear counters after remediation.

**Lifecycle And Runtime**
- **Named lifecycle profiles**: `http_request`, `job_run`, `message_consume`, `deployment`, `payment_authorization`.
- **Phase ordering and guards**: `pre`, `post`, `error`, `finally`, plus custom phases.
- **Error/panic phase**: capture handler panic, timeout, canceled request, upstream failure.
- **Response classification**: healthy/unhealthy policy helpers for 2xx/3xx/4xx/5xx with configurable exceptions.
- **Latency SLO rules**: trigger on slow endpoint, p95/p99 latency, timeout bursts.
- **App-wide health policies**: aggregate across route, tenant, service, region.

**Routing**
- **Route groups**: group routes by service, namespace, tenant, product area.
- **Route metadata schema**: validate route metadata keys like `policy_type`, `owner`, `category`, `slo`.
- **Host/method matching**: support host, scheme, method, path together.
- **Route conflict diagnostics**: detect shadowed routes or ambiguous patterns.
- **Route discovery/import**: generate route catalogs from OpenAPI, Go server mux, Fiber/Echo/Chi routes.

**Actions And Integrations**
- **Action execution registry**: host-registered handlers for `notify`, `escalate`, `ticket`, `webhook`, `log`.
- **Dry-run actions**: simulate action execution without side effects.
- **Action result feedback**: persist whether notification/webhook/ticket succeeded.
- **Retries/dead-letter queue**: for failed action delivery.
- **Action policy**: allow/deny which policies can call which sinks.
- **Incident grouping**: group repeated events into one incident ID.

**Testing And Simulation**
- **Lifecycle tests**: test full pre/post/error flow, not only decision tables.
- **Time-travel tests**: simulate windows, TTL expiry, cooldown.
- **Stateful scenario tests**: sequence of events with expected state after each step.
- **Golden audit tests**: verify emitted events/actions/audit envelopes.
- **Route coverage tests**: ensure every route has matching policy.
- **Mutation tests**: ensure dangerous policy changes fail gates.

**Governance**
- **Policy ownership**: required owner/team/contact/SLO.
- **Approval workflows**: different approval levels for deny/suspend/webhook policies.
- **Blast-radius checks**: detect if a change affects many routes/tenants.
- **Policy diff explain**: human-readable change summary between versions.
- **Risk classification**: low/medium/high policy change levels.
- **Deprecation lifecycle**: mark decisions/routes/actions as deprecated with warnings.

**Observability**
- **Metrics by decision/chain/route/action**.
- **Decision latency histograms**.
- **Action emission metrics**.
- **State cardinality reports**.
- **Policy health endpoint**: active definitions, stale versions, failed actions, state volume.
- **Trace integration**: attach decision IDs and audit IDs to request traces.

**Developer Experience**
- **BCL formatter awareness for chain/routes/lifecycle**.
- **VSCode tree view**: definitions, decisions, chains, routes, lifecycle phases.
- **Route pattern hover**: show normalized pattern, params, conflicts.
- **Policy graph view**: route -> lifecycle -> decision -> chain -> actions.
- **Local playground**: run a lifecycle request with JSON input and inspect state changes.
- **Explain mode for chains**: why a step triggered, which event count crossed threshold.

**Storage And Production**
- **Transactional chain evaluation** for SQLite and future stores.
- **Postgres store** for production deployments.
- **State compaction** and event retention policies.
- **Multi-region event consistency model**.
- **Backup/restore for policy state**.
- **Tenant-level quotas** for events, states, actions.

**Security**
- **PII redaction in audit/action payloads**.
- **Secret-safe action config**.
- **Policy sandbox profiles**.
- **Signed policy bundles**.
- **Tamper-evident state/event history**.
- **Strict external sink allowlists per tenant/environment.**

# Condition Platform Tasks

## Phase 1: Native Runtime Foundation
- [x] Add durable `chain`, `watch`, and `step` policy state.
- [x] Add memory, SQLite, and file storage for chain events/state.
- [x] Add native route catalogs and compiled route matching.
- [x] Add generic lifecycle evaluation with `pre`, `post`, and custom phases.
- [x] Add post-response observability examples for 5xx and unexpected 4xx.
- [x] Add host action handler registry for `log`, `notify`, `escalate`, `ticket`, and custom actions.
- [x] Add lifecycle sequence tests with time travel and expected state after each step.
- [x] Add route conflict/shadowing diagnostics.

## Phase 2: Policy Packages And Contracts
- [x] Add policy package manifests with owner, domain, capabilities, routes, actions, state, and external access requirements.
- [x] Add action catalogs with allowed sinks, severity, payload schema, retries, and approval requirements.
- [x] Add typed standard facts for `request`, `response`, `principal`, `route`, `tenant`, `resource`, `risk`, and `state`.
- [x] Add output contracts for decisions, chains, and lifecycles.
- [x] Add policy inheritance and overrides for global, tenant, route, endpoint, and environment layers.
- [x] Add package diff/explain for route, action, decision, chain, and lifecycle changes.

## Phase 3: Stateful Policy Analytics
- [x] Add native sliding counters and distinct counters.
- [x] Add windowed aggregations: count, rate, ratio, min, max, avg, p95, p99.
- [x] Add response/error classifiers with configurable healthy and unhealthy status sets.
- [x] Add decay-based risk scores and cooldown/reset actions.
- [x] Add composite watches across multiple event types.
- [x] Add suppression and deduplication windows for notifications/incidents.

## Phase 4: Actions And Incident Handling
- [x] Add durable action delivery records with status and retry metadata.
- [x] Add dead-letter queue for failed action deliveries.
- [x] Add action retry policy with backoff and max attempts.
- [x] Add incident grouping keys and incident lifecycle state.
- [x] Add dry-run action execution mode for simulation and canary.
- [x] Add per-tenant and per-environment action allowlists.

## Phase 5: Testing, Simulation, And Governance
- [x] Add lifecycle test blocks for full request/response/error sequences.
- [ ] Add stateful scenario tests with clock controls.
- [x] Add route coverage gates to ensure every route has a policy.
- [ ] Add mutation and blast-radius checks for policy releases.
- [ ] Add approval policies based on action severity and external sinks.
- [ ] Add signed policy bundles and bundle verification.

## Phase 6: Observability And Operations
- [ ] Add decision, chain, lifecycle, route, and action metrics.
- [x] Add state cardinality and retention reports.
- [x] Add policy health/readiness checks for stale definitions, failed actions, and large state.
- [x] Add trace IDs and audit IDs to lifecycle responses.
- [x] Add state compaction and retention controls.
- [ ] Add Postgres storage backend for production deployments.

## Phase 7: Developer Experience
- [x] Add VSCode grammar/snippets for routes, lifecycle, chain, watch, and step.
- [x] Add LSP diagnostics for route conflicts and lifecycle references.
- [x] Add completions for route IDs, lifecycle IDs, phase names, chain IDs, and action IDs.
- [x] Add hover details for normalized route patterns and lifecycle phase flow.
- [ ] Add policy graph view: route -> lifecycle -> decision -> chain -> action.
- [ ] Add local lifecycle playground for JSON request/response inputs.

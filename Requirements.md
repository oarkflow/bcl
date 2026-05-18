Yes. Make BCL a **generic configuration + policy + expression language**, not only AuthZ.

## BCL core goals

BCL should support:

```bcl
object "id" {
  field value
  list  [a, b, c]
  map   {
    key value
  }

  when {
    expression
  }
}
```

It should handle:

* scalar values
* maps
* slices/lists
* nested blocks
* references
* environment variables
* expressions
* conditions
* reusable constants
* imports/includes
* schema validation
* runtime overrides

---

## 1. Primitive values

```bcl
name     "ProcessGate"
enabled  true
workers  8
timeout  5s
size     512MB
ratio    0.75
```

Supported types:

```bcl
string
int
float
bool
duration
bytes
date
datetime
regex
cidr
url
email
identifier
```

Examples:

```bcl
cache_ttl  5m
max_size   2GB
start_date 2026-05-17
network    cidr("10.0.0.0/8")
pattern    regex("^admin-.*$")
```

---

## 2. Lists / slices

Support both inline and multiline.

```bcl
roles ["admin", "superadmin"]
```

```bcl
roles {
  admin
  superadmin
  auditor
}
```

For generic parsing, normalize both into:

```go
[]any{"admin", "superadmin", "auditor"}
```

---

## 3. Maps / objects

```bcl
attrs {
  department "engineering"
  clearance  "high"
  active     true
}
```

Nested maps:

```bcl
limits {
  request {
    per_minute 100
    burst      20
  }

  storage {
    max_size 5GB
  }
}
```

Normalize into:

```go
map[string]any{
  "limits": map[string]any{
    "request": map[string]any{
      "per_minute": 100,
      "burst": 20,
    },
  },
}
```

---

## 4. Block objects

```bcl
policy "allow-admin" {
  tenant   "org1"
  effect   allow
  priority 100

  actions {
    *
  }

  resources {
    *
  }

  when {
    subject.roles has_any ["admin", "superadmin"]
  }
}
```

Generic grammar:

```bcl
block_type "block_id" {
  body
}
```

Examples:

```bcl
tenant "org1" {}
policy "allow-admin" {}
role "admin" {}
pipeline "loan-approval" {}
route "admin-api" {}
rule "detect-risk" {}
```

---

## 5. Environment variables

```bcl
database_url env("DATABASE_URL")
secret_key   env("SECRET_KEY")
port         env("PORT", 8080)
debug        env.bool("DEBUG", false)
workers      env.int("WORKERS", 8)
timeout      env.duration("TIMEOUT", 5s)
```

Recommended env functions:

```bcl
env("KEY")
env("KEY", default)
env.int("KEY", default)
env.bool("KEY", default)
env.float("KEY", default)
env.duration("KEY", default)
env.bytes("KEY", default)
env.list("KEY", ",")
env.required("KEY")
```

Example:

```bcl
server {
  host env("HOST", "0.0.0.0")
  port env.int("PORT", 8080)
}
```

---

## 6. Constants

```bcl
const ADMIN_ROLES = ["admin", "superadmin"]
const READ_ACTIONS = ["read", "list", "view"]
const OFFICE_HOURS = "09:00-18:00"
```

Usage:

```bcl
when {
  subject.roles has_any ADMIN_ROLES
}
```

---

## 7. Reusable sets

```bcl
set "admin-roles" {
  admin
  superadmin
  root
}

set "document-actions" {
  read
  write
  delete
}
```

Usage:

```bcl
actions {
  use set("document-actions")
}

when {
  subject.roles has_any set("admin-roles")
}
```

---

## 8. References

```bcl
role "team-lead" {
  inherits {
    ref role.editor
  }
}
```

Or simpler:

```bcl
inherits {
  role("editor")
}
```

For config references:

```bcl
server {
  port config.app.port
}
```

---

## 9. Expressions

BCL should support safe expressions inside `when`.

```bcl
when {
  subject.type == "user"
}
```

Operators:

```bcl
==
!=
>
>=
<
<=
in
not_in
contains
starts_with
ends_with
matches
has
has_any
has_all
between
exists
empty
```

Examples:

```bcl
when {
  subject.roles has_any ["admin", "superadmin"]
}

when {
  request.ip in cidr("10.0.0.0/8")
}

when {
  resource.owner_id == subject.id
}

when {
  time.now between time("09:00") and time("18:00")
}

when {
  request.path matches regex("^/admin/.*")
}
```

---

## 10. Boolean conditions

```bcl
when {
  all {
    subject.type == "user"
    subject.active == true
    subject.roles has_any ["admin", "manager"]
  }
}
```

```bcl
when {
  any {
    subject.id == resource.owner_id
    subject.roles has_any ["admin"]
  }
}
```

```bcl
when {
  not {
    subject.status == "blocked"
  }
}
```

Recommended condition blocks:

```bcl
all {}
any {}
not {}
none {}
```

---

## 11. Functions

Useful generic functions:

```bcl
lower(value)
upper(value)
len(value)
contains(list, value)
regex(pattern)
cidr(range)
time(value)
date(value)
now()
duration(value)
ip(value)
json(value)
base64(value)
sha256(value)
```

AuthZ/rule-specific:

```bcl
is_owner(subject, resource)
has_role(subject, "admin")
has_permission(subject, "read:document:*")
within_business_hours("09:00", "18:00")
geo_in(request.geo.country, ["NP", "IN"])
```

---

## 12. Interpolation

```bcl
log_path "/var/log/${app.name}.log"
tenant_db "tenant_${tenant.id}"
```

With env:

```bcl
database_url "postgres://${env("DB_USER")}:${env("DB_PASS")}@${env("DB_HOST")}/app"
```

Use carefully. For security-sensitive config, prefer structured fields over string interpolation.

---

## 13. Imports

```bcl
import "./common.bcl"
import "./roles.bcl"
import "./policies/*.bcl"
```

Named imports:

```bcl
import "./common.bcl" as common
```

Usage:

```bcl
roles {
  use common.set("admin-roles")
}
```

---

## 14. Profiles / environments

```bcl
profile "dev" {
  debug true
  database_url env("DEV_DATABASE_URL")
}

profile "prod" {
  debug false
  database_url env.required("DATABASE_URL")
}
```

Run with:

```bash
app --profile prod
```

Or:

```bcl
active_profile env("APP_ENV", "dev")
```

---

## 15. Overrides

```bcl
override policy "allow-admin" {
  priority 200
}
```

Or environment-specific:

```bcl
profile "prod" {
  override engine {
    workers 16
    cache_ttl 10m
  }
}
```

---

## 16. Validation schema

BCL should allow schema definitions.

```bcl
schema policy {
  required tenant string
  required effect enum ["allow", "deny"]
  required actions list<string>
  required resources list<string>
  optional priority int default 0
  optional when expression
}
```

This allows:

```bash
bcl validate authz.bcl
```

---

## 17. Metadata

```bcl
policy "deny-sensitive" {
  meta {
    owner       "security-team"
    description "Deny sensitive document access without clearance"
    tags        ["security", "compliance"]
    created_by  "admin"
  }

  tenant   "org1"
  effect   deny
  priority 100
}
```

---

## 18. Lifecycle hooks

Useful for rule engines and policy systems:

```bcl
rule "high-risk-login" {
  when {
    request.geo.country not_in subject.allowed_countries
  }

  then {
    action "require_mfa"
    action "notify_security"
  }
}
```

Generic action syntax:

```bcl
then {
  emit "risk.detected"
  log  "High risk login"
  deny "Country mismatch"
}
```

---

## 19. Full generic example

```bcl
import "./common.bcl"

const ADMIN_ROLES = ["admin", "superadmin"]

tenant "org1" {
  name   "Engineering Org"
  parent "root"

  attrs {
    region "NP"
    tier   "enterprise"
  }
}

set "write-actions" {
  write
  update
  delete
}

policy "document-owner-or-admin" {
  tenant   "org1"
  effect   allow
  priority 100

  actions {
    read
    use set("write-actions")
  }

  resources {
    document:*
  }

  when {
    any {
      resource.owner_id == subject.id
      subject.roles has_any ADMIN_ROLES
    }
  }

  meta {
    description "Allow owners or admins to access documents"
    tags ["document", "rbac", "abac"]
  }
}

engine {
  cache_ttl      env.duration("CACHE_TTL", 5m)
  attr_ttl       10m
  workers        env.int("WORKERS", 8)
  strict_mode    true
}
```

---

## Best BCL feature set

Your BCL should support:

```text
1. comments
2. strings
3. numbers
4. booleans
5. durations
6. byte sizes
7. dates
8. lists
9. maps
10. nested blocks
11. references
12. constants
13. reusable sets
14. env variables
15. default values
16. required env values
17. imports
18. profiles
19. overrides
20. expressions
21. condition blocks
22. functions
23. metadata
24. schemas
25. validation
26. formatting
27. linting
28. AST normalization
29. runtime hot reload
30. backward-compatible compact syntax
```

Final recommendation: design BCL as **HCL-like block syntax + CEL-like safe expressions + JSON-compatible AST**. That gives you readability, easy parsing, runtime safety, and future extensibility.

Yes — a few important pieces are still missing to make BCL truly complete.

## Missing features to add

### 1. First-class `null`

```bcl
description null
```

Useful for clearing overrides or optional values.

---

### 2. Multi-line strings

```bcl
description """
This policy allows document owners
to read, write, and delete their own documents.
"""
```

Also useful for templates, SQL, scripts, error messages.

---

### 3. Heredoc blocks

```bcl
template <<EOF
Hello ${user.name},
Your request was approved.
EOF
```

Good for emails, SQL, JSON payloads, policy explanations.

---

### 4. Tuple / fixed-length arrays

```bcl
range [10, 100]
geo   [27.7172, 85.3240]
```

Schema can enforce:

```bcl
field geo tuple<float, float>
```

---

### 5. Object arrays

```bcl
headers [
  {
    name  "X-Tenant-ID"
    value request.tenant_id
  },
  {
    name  "X-Request-ID"
    value request.id
  }
]
```

Very useful for actions, routing, workflows, HTTP, pipelines.

---

### 6. Namespaces

```bcl
namespace authz {
  policy "allow-admin" {}
  role "admin" {}
}
```

Prevents naming conflict in large systems.

---

### 7. Version declaration

```bcl
bcl {
  version "1.0"
  strict  true
}
```

Important for future compatibility.

---

### 8. Deprecation support

```bcl
policy "old-rule" {
  deprecated true
  replaced_by policy("new-rule")
}
```

Useful for long-lived configs.

---

### 9. Conflict detection

Built-in validation should detect:

```text
duplicate IDs
cyclic inheritance
unknown references
shadowed policies
unreachable rules
conflicting allow/deny priorities
invalid condition fields
unused sets/constants
```

---

### 10. Module system

```bcl
module "authz" {
  source "./modules/authz"
  inputs {
    tenant "org1"
  }
}
```

This makes reusable config packages possible.

---

### 11. Sensitive values

```bcl
secret_key sensitive(env.required("SECRET_KEY"))
```

CLI/logs should redact it:

```text
secret_key = ****
```

---

### 12. Computed values

```bcl
full_name concat(user.first_name, " ", user.last_name)
```

But keep computation limited and sandboxed.

---

### 13. Capability permissions for functions

Not all functions should be allowed everywhere.

```bcl
runtime {
  allow_functions {
    env
    regex
    cidr
    time
  }

  deny_functions {
    exec
    file
    http
  }
}
```

For security, avoid dangerous functions by default.

---

### 14. Error messages

Policies/rules should support reason codes.

```bcl
deny_message "You do not have clearance for this document"
reason_code  "CLEARANCE_REQUIRED"
```

Useful for APIs and audit logs.

---

### 15. Audit annotations

```bcl
audit {
  level detailed
  fields {
    subject.id
    resource.id
    decision
    matched_policy
  }
}
```

Very useful for AuthZ, fraud detection, workflows, and compliance.

---

### 16. Priority and evaluation strategy

```bcl
evaluation {
  strategy deny_overrides
  default  deny
}
```

Supported strategies:

```text
deny_overrides
allow_overrides
first_match
highest_priority
weighted_score
all_must_pass
```

---

### 17. Type aliases

```bcl
type Action = string
type TenantID = string
type Permission = string
```

Useful later if you add schema validation and code generation.

---

### 18. Formatting rules

Add official formatter:

```bash
bcl fmt authz.bcl
```

Without formatter, large configs become messy.

---

### 19. Lockfile for imports

```text
bcl.lock
```

Needed if modules/imports can come from Git, registry, or remote source.

---

### 20. JSON/YAML export

BCL should compile into portable JSON:

```bash
bcl compile authz.bcl --out authz.json
```

Your runtime should consume the normalized AST, not raw text.

---

## Final “complete” BCL checklist

```text
Syntax:
- comments
- scalar values
- null
- lists
- maps
- nested blocks
- object arrays
- multi-line strings
- heredoc strings

Config:
- constants
- sets
- env variables
- references
- imports
- modules
- namespaces
- profiles
- overrides

Logic:
- expressions
- condition blocks
- functions
- safe function permissions
- evaluation strategies

Safety:
- schema validation
- strict mode
- sensitive values
- conflict detection
- cycle detection
- unknown reference detection

Operations:
- formatter
- linter
- compiler
- JSON export
- lockfile
- hot reload
- migration/version support

AuthZ-specific:
- priority
- effect
- reason codes
- deny messages
- audit annotations
- default decision
- policy simulation
- explain output
```

The most important additions are: **versioning, namespaces, sensitive values, modules, schema validation, conflict detection, evaluation strategy, explain output, and JSON AST compilation**.

For BCL, do **not** make command/file/HTTP execution part of normal config evaluation. Treat them as **capabilities** executed only in controlled phases.

## Recommended model

```bcl
runtime {
  mode sandboxed

  capabilities {
    command disabled
    file    read_only
    http    allowlist
  }
}
```

## 1. Command execution

Use explicit `command` blocks, never inline arbitrary shell.

```bcl
command "backup-db" {
  enabled false

  exec "/usr/local/bin/pg_dump"

  args {
    "--format=custom"
    "--file=/backup/app.dump"
  }

  env {
    PGDATABASE env.required("DB_NAME")
    PGUSER     env.required("DB_USER")
  }

  timeout 30s

  output {
    capture stdout
    max_size 1MB
    redact ["password", "secret", "token"]
  }
}
```

Avoid:

```bcl
run "rm -rf /tmp/*"
```

Prefer structured execution:

```bcl
exec "/bin/echo"
args ["hello"]
```

## 2. File handling

Use explicit permissions.

```bcl
files {
  root "/app/config"

  allow_read {
    "./*.bcl"
    "./policies/*.bcl"
  }

  allow_write {
    "./compiled/*.json"
  }

  deny {
    "/etc/*"
    "~/.ssh/*"
  }
}
```

File resource example:

```bcl
file "compiled-policy" {
  path "./compiled/authz.json"
  mode write
  format json
}
```

Reading:

```bcl
source "users" {
  type file
  path "./data/users.json"
  format json
  mode read
}
```

## 3. HTTP handling

Use connectors, not raw HTTP everywhere.

```bcl
http "identity-api" {
  base_url env.required("IDENTITY_API_URL")

  auth {
    type bearer
    token sensitive(env.required("IDENTITY_API_TOKEN"))
  }

  timeout 5s

  retry {
    attempts 3
    backoff exponential
  }

  allow_methods {
    GET
    POST
  }

  allow_paths {
    "/users/*"
    "/groups/*"
  }
}
```

Usage:

```bcl
source "subject-attrs" {
  type http
  connector http("identity-api")
  method GET
  path "/users/${subject.id}"
}
```

## 4. Generic integrations

Use `connector` blocks.

```bcl
connector "postgres-main" {
  type postgres

  dsn sensitive(env.required("DATABASE_URL"))

  pool {
    max_open 20
    max_idle 5
    max_lifetime 30m
  }
}
```

```bcl
connector "redis-cache" {
  type redis
  address env.required("REDIS_ADDR")
  password sensitive(env("REDIS_PASSWORD", null))
  db 0
}
```

```bcl
connector "kafka-events" {
  type kafka
  brokers env.list("KAFKA_BROKERS", ",")
  topic   "authz.decisions"
}
```

## 5. Actions

Use action blocks for side effects.

```bcl
action "notify-security" {
  type http
  connector http("security-api")

  request {
    method POST
    path "/alerts"

    body {
      subject_id subject.id
      resource_id resource.id
      reason decision.reason
    }
  }
}
```

```bcl
action "write-audit-log" {
  type file

  target file("audit-log")

  content {
    time        time.now
    subject_id subject.id
    action      request.action
    decision    decision.effect
  }
}
```

## 6. Execution phases

Separate safe config from active execution.

```bcl
phase load {
  allow {
    env
    file_read
  }
}

phase validate {
  allow {
    schema
    references
  }
}

phase evaluate {
  allow {
    expressions
    cache
  }
}

phase execute {
  allow {
    http
    file_write
    command
  }
}
```

Best rule:

```text
load      = parse config
validate  = check config
evaluate  = decide
execute   = perform side effects
```

## 7. Capability policy

```bcl
capabilities {
  command {
    enabled false
  }

  file {
    read  ["./config/*", "./data/*"]
    write ["./out/*"]
  }

  http {
    allow_hosts {
      "api.company.com"
      "*.internal.local"
    }

    deny_hosts {
      "169.254.169.254"
      "localhost"
      "127.0.0.1"
    }
  }
}
```

## 8. Output handling

```bcl
output {
  format json

  fields {
    decision.effect
    decision.reason
    matched_policy.id
    audit.trace_id
  }

  redact {
    subject.password
    subject.token
    request.headers.Authorization
  }
}
```

## 9. Safer integration pattern

The cleanest model is:

```bcl
connector "name" {
  type ...
  config ...
}

source "name" {
  type ...
  connector ...
  read ...
}

action "name" {
  type ...
  connector ...
  write/execute ...
}
```

So BCL has three integration layers:

```text
connector = connection config
source    = read external data
action    = side effect / output / mutation
```

## Final recommendation

Do not allow free-form execution. Use this structure:

```bcl
runtime {
  mode sandboxed
}

capabilities {
  command disabled
  file read_only
  http allowlist
}

connector "..." {}
source "..." {}
action "..." {}
output "..." {}
```

This keeps BCL powerful but safe, parseable, auditable, and enterprise-friendly.

For BCL, HTTP handling should be split into **connector**, **request**, **auth**, **headers**, **body**, **retry**, **timeout**, **TLS**, **security**, and **response mapping**.

## Recommended HTTP connector

```bcl
http "identity-api" {
  base_url env.required("IDENTITY_API_URL")
  timeout 5s

  auth {
    type bearer
    token sensitive(env.required("IDENTITY_API_TOKEN"))
  }

  headers {
    "Accept"       "application/json"
    "Content-Type" "application/json"
    "User-Agent"   "BCL-Client/1.0"
  }

  retry {
    attempts 3
    backoff exponential
    min_delay 200ms
    max_delay 3s

    retry_on_status {
      408
      429
      500
      502
      503
      504
    }
  }

  circuit_breaker {
    enabled true
    failure_threshold 5
    cooldown 30s
  }

  tls {
    verify true
    min_version "1.2"
  }

  security {
    allow_methods {
      GET
      POST
    }

    allow_paths {
      "/users/*"
      "/groups/*"
    }

    deny_hosts {
      "localhost"
      "127.0.0.1"
      "169.254.169.254"
    }

    max_response_size 2MB
  }
}
```

## Request usage

```bcl
source "subject-profile" {
  type http
  connector http("identity-api")

  request {
    method GET
    path "/users/${subject.id}"

    query {
      include "roles,groups,attrs"
    }

    headers {
      "X-Tenant-ID"  subject.tenant
      "X-Request-ID" request.id
    }
  }

  response {
    expect_status 200
    format json

    map {
      subject.email      body.email
      subject.roles      body.roles
      subject.attrs      body.attributes
      subject.groups     body.groups
    }
  }
}
```

## POST request with JSON body

```bcl
action "send-risk-alert" {
  type http
  connector http("security-api")

  request {
    method POST
    path "/alerts"

    headers {
      "Content-Type" "application/json"
      "X-Tenant-ID"  subject.tenant
    }

    body json {
      subject_id  subject.id
      resource_id resource.id
      action      request.action
      risk_score  decision.risk_score
      reason      decision.reason
    }
  }

  response {
    expect_status [200, 201, 202]
    capture body.alert_id as alert_id
  }
}
```

## Header handling features

Support static headers:

```bcl
headers {
  "Accept" "application/json"
}
```

Dynamic headers:

```bcl
headers {
  "X-Tenant-ID" subject.tenant
  "X-Trace-ID"  request.trace_id
}
```

Sensitive headers:

```bcl
headers {
  "Authorization" sensitive("Bearer ${env.required("API_TOKEN")}")
}
```

Conditional headers:

```bcl
headers {
  when request.id exists {
    "X-Request-ID" request.id
  }

  when subject.tenant exists {
    "X-Tenant-ID" subject.tenant
  }
}
```

Header templates:

```bcl
headers {
  "Authorization" "Bearer ${env.required("API_TOKEN")}"
}
```

Header removal:

```bcl
remove_headers {
  "Cookie"
  "X-Debug"
}
```

## Auth types

```bcl
auth {
  type none
}
```

```bcl
auth {
  type bearer
  token sensitive(env.required("API_TOKEN"))
}
```

```bcl
auth {
  type basic
  username env.required("API_USER")
  password sensitive(env.required("API_PASS"))
}
```

```bcl
auth {
  type api_key
  location header
  name "X-API-Key"
  value sensitive(env.required("API_KEY"))
}
```

```bcl
auth {
  type api_key
  location query
  name "api_key"
  value sensitive(env.required("API_KEY"))
}
```

```bcl
auth {
  type mtls
  cert_file "/secrets/client.crt"
  key_file  sensitive("/secrets/client.key")
}
```

## Query parameters

```bcl
query {
  page 1
  limit 100
  include ["roles", "groups", "attrs"]
}
```

Or repeated query style:

```bcl
query {
  include {
    roles
    groups
    attrs
  }
}
```

Should compile to:

```text
?include=roles&include=groups&include=attrs
```

## Body types

```bcl
body json {
  user_id subject.id
}
```

```bcl
body form {
  username subject.username
  password sensitive(subject.password)
}
```

```bcl
body text """
plain text body
"""
```

```bcl
body raw {
  content_type "application/octet-stream"
  value file("payload.bin")
}
```

## Response handling

```bcl
response {
  expect_status 200
  format json
  timeout 5s

  capture {
    user_id body.id
    roles   body.roles
  }

  on_status 404 {
    default {
      roles []
      attrs {}
    }
  }

  on_status 401 {
    fail "IDENTITY_UNAUTHORIZED"
  }

  on_status 500 {
    retry
  }
}
```

## Full reusable HTTP block

```bcl
http "processgate-api" {
  base_url "https://api.processgate.app"
  timeout 10s

  auth {
    type bearer
    token sensitive(env.required("PROCESSGATE_TOKEN"))
  }

  headers {
    "Accept"       "application/json"
    "Content-Type" "application/json"
    "User-Agent"   "BCL/1.0"
  }

  retry {
    attempts 3
    backoff exponential
    retry_on_status [408, 429, 500, 502, 503, 504]
  }

  request_defaults {
    headers {
      "X-App" "authz-engine"
    }
  }

  response_defaults {
    format json
    max_size 2MB
  }
}
```

## Final HTTP feature checklist

```text
HTTP connector:
- base_url
- timeout
- auth
- default headers
- default query
- retry
- backoff
- circuit breaker
- TLS config
- proxy config
- security allowlist
- max response size

Request:
- method
- path
- query
- headers
- remove_headers
- body json/form/text/raw
- per-request timeout
- idempotency key
- correlation ID

Auth:
- none
- bearer
- basic
- api_key header/query
- mTLS
- OAuth2 client credentials

Response:
- expected status
- response format
- capture fields
- default values
- error mapping
- retry decision
- redaction
- max body size

Security:
- deny localhost
- deny metadata IP
- allow hosts
- allow paths
- allow methods
- redact sensitive headers
- block redirects by default
```

Best final pattern:

```bcl
http "name" {
  connection config
  auth {}
  headers {}
  retry {}
  tls {}
  security {}
}

source/action {
  request {}
  response {}
}
```

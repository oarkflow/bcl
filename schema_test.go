package bcl

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSchemaMetadataDefaultsAndGeneratedFunctions(t *testing.T) {
	src := []byte(`
schema widget {
  required name string pattern "^[a-z]+$"
  optional priority int default 10 min 1 max 100
  optional id string default uuid() generated
  optional id_v4 string default uuid_v4() generated
  optional random_id string default random_uuid() generated
  optional slug string default unique_id("wid") generated
  optional short_id string default uid("short") generated
  optional created_at datetime default now() generated
  optional generated_at string default current_timestamp() generated
  optional updated_at datetime default CURRENT_TIMESTAMP generated
  optional effective_on date default date()
  optional generated_on string default current_date()
  optional generated_time string default current_time()
  optional epoch_seconds int default unix_timestamp()
  optional epoch_millis int default unix_millis()
  optional expires_on date default date("2027-01-01")
  optional review_at datetime default datetime()
  optional review_time time default time()
  optional secret string sensitive
  optional meta block {
    required owner email format email
  }
}

widget "ok" {
  name alpha
  meta {
    owner "team@example.com"
  }
}
`)
	n, err := CompileBytes(src, &Options{AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	body := n.Blocks[0]["body"].(map[string]any)
	if body["priority"] != int64(10) {
		t.Fatalf("default priority = %#v", body["priority"])
	}
	if !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(body["id"].(string)) {
		t.Fatalf("bad uuid default: %#v", body["id"])
	}
	if !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(body["id_v4"].(string)) {
		t.Fatalf("bad uuid_v4 default: %#v", body["id_v4"])
	}
	if !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(body["random_id"].(string)) {
		t.Fatalf("bad random_uuid default: %#v", body["random_id"])
	}
	if !regexp.MustCompile(`^wid_[0-9a-f]{24}$`).MatchString(body["slug"].(string)) {
		t.Fatalf("bad unique_id default: %#v", body["slug"])
	}
	if !regexp.MustCompile(`^short_[0-9a-f]{24}$`).MatchString(body["short_id"].(string)) {
		t.Fatalf("bad uid default: %#v", body["short_id"])
	}
	if _, err := time.Parse(time.RFC3339, body["generated_at"].(string)); err != nil {
		t.Fatalf("bad current_timestamp default: %#v", body["generated_at"])
	}
	if _, ok := body["effective_on"].(map[string]any)["$date"]; !ok {
		t.Fatalf("bad date() default: %#v", body["effective_on"])
	}
	if _, err := time.Parse("2006-01-02", body["generated_on"].(string)); err != nil {
		t.Fatalf("bad current_date default: %#v", body["generated_on"])
	}
	if _, err := time.Parse("15:04:05", body["generated_time"].(string)); err != nil {
		t.Fatalf("bad current_time default: %#v", body["generated_time"])
	}
	if body["epoch_seconds"].(int64) <= 0 || body["epoch_millis"].(int64) <= body["epoch_seconds"].(int64) {
		t.Fatalf("bad unix timestamp defaults: seconds=%#v millis=%#v", body["epoch_seconds"], body["epoch_millis"])
	}
	if body["expires_on"].(map[string]any)["$date"] != "2027-01-01" {
		t.Fatalf("bad date(value) default: %#v", body["expires_on"])
	}
	if _, ok := body["review_at"].(map[string]any)["$datetime"]; !ok {
		t.Fatalf("bad datetime() default: %#v", body["review_at"])
	}
	if _, ok := body["review_time"].(map[string]any)["$time"]; !ok {
		t.Fatalf("bad time() default: %#v", body["review_time"])
	}
	schema := n.Schemas["widget"].(map[string]any)
	fields := schema["fields"].([]map[string]any)
	var sawSensitive, sawNested bool
	for _, f := range fields {
		if f["name"] == "secret" && f["sensitive"] == true {
			sawSensitive = true
		}
		if f["name"] == "meta" && len(f["fields"].([]map[string]any)) == 1 {
			sawNested = true
		}
	}
	if !sawSensitive || !sawNested {
		t.Fatalf("schema metadata = %#v", fields)
	}
}

func TestSchemaConstraintsValidate(t *testing.T) {
	doc, err := Parse([]byte(`
schema widget {
  required name string pattern "^[a-z]+$"
  optional priority int min 1 max 5
}

widget "bad" {
  name "NotLower"
  priority 9
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	if len(diags) < 2 {
		t.Fatalf("expected pattern and max diagnostics, got %#v", diags)
	}
}

func TestComprehensiveSchemaKeywordsCompileValidateAndExport(t *testing.T) {
	src := []byte(`
schema sla_escalation {
  required ticket object {
    closed true
    title "Ticket"
    description "Ticket schema clauses can be written as a block."
    required minutes_open number min 0 multiple_of 1
    required sla_minutes number min 1
    required warning_minutes number min 0 lte_field sla_minutes
    optional status string const "open"
    optional priority string enum ["low", "normal", "critical"]
    optional requester object closed true {
      optional email string format email sensitive pii email
    }
    optional tags list items string min_items 1 max_items 3 unique_items
  }
}
`)
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	schema := n.Schemas["sla_escalation"]
	diags := ValidateSchemaValue("sla_escalation", schema, map[string]any{
		"ticket": map[string]any{
			"minutes_open":    int64(10),
			"sla_minutes":     int64(60),
			"warning_minutes": int64(30),
			"status":          "open",
			"priority":        "critical",
			"requester":       map[string]any{"email": "team@example.com"},
			"tags":            []any{"vip", "sla"},
		},
	})
	if len(diags) != 0 {
		t.Fatalf("valid schema value diagnostics = %#v", diags)
	}
	diags = ValidateSchemaValue("sla_escalation", schema, map[string]any{
		"ticket": map[string]any{
			"minutes_open":    int64(10),
			"sla_minutes":     int64(60),
			"warning_minutes": int64(70),
			"status":          "closed",
			"priority":        "urgent",
			"requester":       map[string]any{"email": "not-email", "extra": true},
			"tags":            []any{"vip", "vip"},
			"extra":           true,
		},
	})
	text := FormatDiagnostics(diags)
	for _, want := range []string{"must be <= sla_minutes", "does not match const", "is not in enum", "format email", "duplicate items", "not allowed by closed schema"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in diagnostics:\n%s", want, text)
		}
	}
	exported, err := ExportJSONSchema(n, "sla_escalation")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exported, []byte(`"additionalProperties": false`)) || !bytes.Contains(exported, []byte(`"uniqueItems": true`)) {
		t.Fatalf("exported JSON Schema missing expected keywords:\n%s", exported)
	}
	components, err := ExportOpenAPIComponents(n)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(components, []byte(`"components"`)) || !bytes.Contains(components, []byte(`"sla_escalation"`)) {
		t.Fatalf("OpenAPI components missing schema:\n%s", components)
	}
}

func TestSchemaOneLinerClausesRemainSupported(t *testing.T) {
	src := []byte(`schema widget {
  optional id string default uuid() generated min_len 3 max_len 128 format uuid description "Generated ID"
  optional tags list items string min_items 0 max_items 5 unique_items
}
`)
	n, err := CompileBytes(src, &Options{AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	fields := n.Schemas["widget"].(map[string]any)["fields"].([]map[string]any)
	if fields[0]["generated"] != true || fields[0]["default"] == nil || fields[0]["min_len"] == nil || fields[1]["items"] != "string" {
		t.Fatalf("one-line schema clauses not normalized: %#v", fields)
	}
	formatted, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	text := string(formatted)
	if strings.Contains(text, "optional id string {\n") || !strings.Contains(text, "optional id string default uuid()") || !strings.Contains(text, "optional tags list") {
		t.Fatalf("one-line schema clauses should remain compact:\n%s", text)
	}
}

func TestLegacyConditionPathSchemaTypeClauses(t *testing.T) {
	src := []byte(`schema transaction_review {
  required customer.id
  required transaction.amount
  optional transaction.currency
  type customer.id string
  type transaction.amount number
  type transaction.currency string
}
`)
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := n.Schemas["transaction_review"].(map[string]any)["fields"].([]map[string]any)
	if len(fields) != 3 {
		t.Fatalf("expected merged legacy fields, got %#v", fields)
	}
	want := map[string]struct {
		typ      string
		required bool
	}{
		"customer.id":          {typ: "string", required: true},
		"transaction.amount":   {typ: "number", required: true},
		"transaction.currency": {typ: "string", required: false},
	}
	for _, field := range fields {
		name, _ := field["name"].(string)
		expected, ok := want[name]
		if !ok {
			t.Fatalf("unexpected field %#v in %#v", name, fields)
		}
		if field["type"] != expected.typ || field["required"] != expected.required {
			t.Fatalf("field %s = %#v, want type=%s required=%v", name, field, expected.typ, expected.required)
		}
	}
	valid := map[string]any{
		"customer":    map[string]any{"id": "cust_123"},
		"transaction": map[string]any{"amount": 42.50},
	}
	if diags := ValidateSchemaValue("transaction_review", n.Schemas["transaction_review"], valid); len(diags) != 0 {
		t.Fatalf("valid legacy nested value diagnostics = %#v", diags)
	}
	invalid := map[string]any{
		"customer":    map[string]any{"id": 123},
		"transaction": map[string]any{},
	}
	text := FormatDiagnostics(ValidateSchemaValue("transaction_review", n.Schemas["transaction_review"], invalid))
	for _, want := range []string{`"customer.id" should be string`, `"transaction.amount" is required`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in diagnostics:\n%s", want, text)
		}
	}
}

func TestSectionedSchemaOptionsFieldsAndCustomSections(t *testing.T) {
	src := []byte(`schema transaction_review {
  options {
    validate_required_fields true
    validate_field_types true
  }
  fields {
    id string required default uuid()
    customer.id string required default "unknown_customer"
    transaction.amount number required default 0.0
    transaction.currency string optional default "USD"
    transaction.created_at string optional default current_timestamp()
  }
  workflow {
    owner "risk-platform"
    stage "review"
  }
}
`)
	n, err := CompileBytes(src, &Options{AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	schema := n.Schemas["transaction_review"].(map[string]any)
	options := schema["options"].(map[string]any)
	if options["validate_required_fields"] != true || options["validate_field_types"] != true {
		t.Fatalf("schema options not preserved: %#v", schema)
	}
	sections := schema["sections"].(map[string]any)
	workflow := sections["workflow"].(map[string]any)
	if workflow["owner"] != "risk-platform" || workflow["stage"] != "review" {
		t.Fatalf("custom schema section not preserved: %#v", sections)
	}
	fields := schema["fields"].([]map[string]any)
	if len(fields) != 5 {
		t.Fatalf("expected sectioned fields, got %#v", fields)
	}
	diags := ValidateSchemaValue("transaction_review", schema, map[string]any{
		"id":          "txn_123",
		"customer":    map[string]any{"id": "cust_123"},
		"transaction": map[string]any{"amount": 42.50, "currency": "USD", "created_at": "2026-05-22T10:15:00Z"},
	})
	if len(diags) != 0 {
		t.Fatalf("valid sectioned schema diagnostics = %#v", diags)
	}
	text := FormatDiagnostics(ValidateSchemaValue("transaction_review", schema, map[string]any{
		"id":          "txn_123",
		"customer":    map[string]any{"id": 123},
		"transaction": map[string]any{},
	}))
	for _, want := range []string{"customer.id", "transaction.amount"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in diagnostics:\n%s", want, text)
		}
	}
	formatted, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(formatted), "fields {\n    id string required default uuid()") || !strings.Contains(string(formatted), "workflow {") {
		t.Fatalf("sectioned schema format lost structure:\n%s", formatted)
	}
}

func TestSectionedSchemaDottedFieldsWorkWithStrictLint(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
  strict true
}

schema route {
  required upstream url
}

schema transaction_review {
  options {
    validate_required_fields true
    validate_field_types true
  }
  fields {
    customer.id string required
    transaction.amount number required
  }
}

route "schema-backed-route" {
  upstream url("https://schema.internal.example.com")
}

transaction_review "legacy-path-schema" {
  customer {
    id "cust_123"
  }
  transaction {
    amount 42.50
  }
}
`)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	text := FormatDiagnostics(Lint(doc, nil))
	for _, unwanted := range []string{`schema "route" value "upstream" should be url`, `unknown field "customer"`, `unknown field "transaction"`} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("unexpected strict lint diagnostic %q in:\n%s", unwanted, text)
		}
	}
}

func TestImportJSONSchema(t *testing.T) {
	decl, diags := ImportJSONSchema("ticket", []byte(`{
	  "type": "object",
	  "required": ["id"],
	  "properties": {
	    "id": {"type": "string", "minLength": 3},
	    "count": {"type": "integer", "minimum": 1}
	  }
	}`))
	if len(diags) != 0 {
		t.Fatalf("import diagnostics = %#v", diags)
	}
	if decl.Name != "ticket" || len(decl.Fields) != 2 {
		t.Fatalf("bad imported schema: %#v", decl)
	}
}

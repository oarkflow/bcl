package bcl

import (
	"bytes"
	"strings"
	"testing"
)

func TestRemainingExportCodegenDocsMigrate(t *testing.T) {
	doc, err := Parse([]byte(`
schema policy {
  required tenant string
  optional priority int default 0
}

policy "p1" {
  tenant "org1"
}
`))
	if err != nil {
		t.Fatal(err)
	}
	migrated, diags := MigrateDocument(doc, "1.0")
	if len(diags) == 0 {
		t.Fatalf("expected migration diagnostic")
	}
	formatted, err := FormatDocument(migrated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(formatted, []byte(`bcl {`)) {
		t.Fatalf("migration did not add bcl block:\n%s", formatted)
	}
	goTypes, err := GenerateGoTypes(migrated, "config")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(goTypes, []byte("type Policy struct")) {
		t.Fatalf("missing generated type:\n%s", goTypes)
	}
	docs := GenerateDocs(migrated)
	if !bytes.Contains(docs, []byte("Schema `policy`")) {
		t.Fatalf("missing docs:\n%s", docs)
	}
	n, err := Compile(migrated, nil)
	if err != nil {
		t.Fatal(err)
	}
	yml, err := Export(n, ExportOptions{Format: "yaml", Fields: []string{"schemas"}, Redact: []string{"schemas.policy"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(yml, []byte("schemas:")) {
		t.Fatalf("bad yaml export:\n%s", yml)
	}
}

func TestRemainingConditionalHeadersAndTypedTextBody(t *testing.T) {
	src := []byte(`
headers {
  when request.id exists {
    "X-Request-ID" request.id
  }
}

request {
  body text """
hello
"""
}
`)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := FormatDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "when request.id exists") {
		t.Fatalf("conditional header not formatted:\n%s", text)
	}
	n, err := Compile(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := n.Body["request"].(map[string]any)
	body := req["body"].(map[string]any)
	if body["type"] != "text" || !strings.Contains(body["value"].(string), "hello") {
		t.Fatalf("bad typed text body: %#v", body)
	}
}

func TestRemainingSimulationTrace(t *testing.T) {
	n, err := CompileBytes([]byte(`
evaluation {
  strategy highest_priority
  default deny
}

rule "low" {
  effect allow
  priority 1
  when { subject.roles has_any ["admin"] }
}

rule "high" {
  effect deny
  priority 10
  reason_code "HIGH_PRIORITY_DENY"
  when { subject.roles has_any ["admin"] }
}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	result := Simulate(n, map[string]any{"subject": map[string]any{"roles": []any{"admin"}}}, nil)
	if result.Decision["effect"] != "deny" || result.Decision["reason"] != "HIGH_PRIORITY_DENY" {
		t.Fatalf("bad decision: %#v", result.Decision)
	}
	if len(result.Trace) == 0 {
		t.Fatalf("missing trace")
	}
}

func TestRemainingResponsePlanAndSensitiveValidation(t *testing.T) {
	doc, err := Parse([]byte(`
http "api" {
  base_url "https://api.example.com"
  auth {
    type bearer
    token "plain"
  }
  proxy {
    mode http
  }
  redirects {
    mode open
  }
}

action "call" {
  type http
  request {
    method POST
    headers {
      "Authorization" "Bearer plain"
    }
  }
  response {
    expect_status [200, 202]
    format json
    capture {
      alert_id body.alert_id
    }
    on_status 500 {
      retry
    }
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	if len(diags) < 4 {
		t.Fatalf("expected proxy/redirect/sensitive diagnostics: %#v", diags)
	}
	action := doc.Items[1].(*Block)
	var response *Block
	for _, n := range action.Body {
		if a, ok := n.(*Assignment); ok && a.Name == "response" {
			obj := a.Value.(*Object)
			response = &Block{Type: "response", Body: obj.Fields, Span: obj.Span}
		}
	}
	plan, err := ResponsePlanFromBlock(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ExpectStatus) != 2 || plan.Capture["alert_id"] != "body.alert_id" || plan.OnStatus[500] != "retry" {
		t.Fatalf("bad response plan: %#v", plan)
	}
}

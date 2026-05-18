package bcl

import (
	"regexp"
	"testing"
)

func TestSchemaMetadataDefaultsAndGeneratedFunctions(t *testing.T) {
	src := []byte(`
schema widget {
  required name string pattern "^[a-z]+$"
  optional priority int default 10 min 1 max 100
  optional id string default uuid() generated
  optional slug string default unique_id("wid") generated
  optional created_at datetime default now() generated
  optional updated_at datetime default CURRENT_TIMESTAMP generated
  optional effective_on date default date()
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
	if !regexp.MustCompile(`^wid_[0-9a-f]{24}$`).MatchString(body["slug"].(string)) {
		t.Fatalf("bad unique_id default: %#v", body["slug"])
	}
	if _, ok := body["effective_on"].(map[string]any)["$date"]; !ok {
		t.Fatalf("bad date() default: %#v", body["effective_on"])
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

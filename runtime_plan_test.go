package bcl

import (
	"strings"
	"testing"
	"time"
)

type mapSecrets map[string]string

func (m mapSecrets) GetSecret(name string) (string, bool, error) {
	v, ok := m[name]
	return v, ok, nil
}

func TestPrepareKeepsRuntimeValuesDeferredAndResolvesAtRuntime(t *testing.T) {
	src := []byte(`
const api_base env.required("EMAIL_API_BASE")

workflow "email" {
  vars {
    request_id coalesce(request.headers.X_Request_ID, runtime.run_id)
    message_id runtime.uuid
    received_at runtime.now
  }

  request {
    method "POST"
    url "${const.api_base}/tenant/${session.tenant_id}/send"
    headers {
      X_Request_ID vars.request_id
      X_Tenant_ID session.tenant_id
      Authorization secrets.required("EMAIL_API_KEY")
    }
    body {
      to input.to
      subject "[script] " + input.subject
      batch "nightly-${input.schedule_id}"
      message_id vars.message_id
      received_at vars.received_at
    }
  }
}
`)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Prepare(doc, &PrepareOptions{
		AllowEnv: true,
		Strict:   true,
		Env: func(k string) (string, bool) {
			if k == "EMAIL_API_BASE" {
				return "https://email.example.com", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	prepared := plan.ToInterface(true)
	wf := prepared["body"].(map[string]any)["workflow"].([]any)[0].(map[string]any)
	body := wf["body"].(map[string]any)
	request := body["request"].(map[string]any)
	url := request["url"].(map[string]any)
	if _, ok := url["$template"]; !ok {
		t.Fatalf("url should remain a runtime template: %#v", url)
	}
	secretRef, ok := request["headers"].(map[string]any)["Authorization"].(map[string]any)
	if !ok || secretRef["$redact"] != true {
		t.Fatalf("secret should stay as a redacted descriptor in prepared view, got %#v", request["headers"].(map[string]any)["Authorization"])
	}

	fixed := time.Date(2026, 6, 25, 10, 30, 0, 0, time.UTC)
	resolved, err := plan.Resolve(&RuntimeContext{
		Input: map[string]any{
			"to":          "user@example.com",
			"subject":     "Welcome",
			"schedule_id": "sched-001",
		},
		Request: map[string]any{"headers": map[string]any{"X_Request_ID": "req-123"}},
		Session: map[string]any{"tenant_id": "acme"},
		Runtime: map[string]any{"uuid": "msg-001", "run_id": "run-001"},
		Secrets: mapSecrets{"EMAIL_API_KEY": "secret-token"},
		Now:     func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatal(err)
	}
	wfOut := resolved["workflow"].([]any)[0].(map[string]any)
	req := wfOut["body"].(map[string]any)["request"].(map[string]any)
	if got := req["url"]; got != "https://email.example.com/tenant/acme/send" {
		t.Fatalf("bad url: %#v", got)
	}
	payload := req["body"].(map[string]any)
	if got := payload["subject"]; got != "[script] Welcome" {
		t.Fatalf("bad subject: %#v", got)
	}
	if got := payload["batch"]; got != "nightly-sched-001" {
		t.Fatalf("bad batch: %#v", got)
	}
	if got := req["headers"].(map[string]any)["Authorization"]; got != "secret-token" {
		t.Fatalf("bad secret: %#v", got)
	}
}

func TestPrepareMissingRuntimeVariableReturnsUsefulError(t *testing.T) {
	doc, err := Parse([]byte(`payload { tenant session.tenant_id }`))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Prepare(doc, &PrepareOptions{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = plan.Resolve(&RuntimeContext{})
	if err == nil || !strings.Contains(err.Error(), "session.tenant_id") {
		t.Fatalf("expected missing runtime variable error, got %v", err)
	}
}

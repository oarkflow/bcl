package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oarkflow/bcl"
)

type secrets map[string]string

func (s secrets) GetSecret(name string) (string, bool, error) {
	v, ok := s[name]
	return v, ok, nil
}

func main() {
	const src = `
const api_base env.required("EMAIL_API_BASE")

workflow "script_email_enrich" {
  route POST "/api/email/script-enrich"

  vars {
    correlation_id coalesce(request.headers.X_Request_ID, runtime.run_id)
    message_id     runtime.uuid
    received_at    runtime.now
  }

  node "send" {
    type "http"

    request {
      method "POST"
      url "${const.api_base}/tenant/${session.tenant_id}/send"
      headers {
        X_Request_ID  vars.correlation_id
        X_Tenant_ID   session.tenant_id
        Authorization secrets.required("EMAIL_API_KEY")
      }
      body {
        to          input.to
        subject     "[script] " + input.subject
        body        input.body
        batch       "nightly-${input.schedule_id}"
        message_id  vars.message_id
        received_at vars.received_at
      }
    }
  }

  response {
    status     "queued"
    message_id vars.message_id
    request_id vars.correlation_id
  }
}
`
	doc, err := bcl.Parse([]byte(src))
	must(err)

	plan, err := bcl.Prepare(doc, &bcl.PrepareOptions{
		AllowEnv: true,
		Strict:   true,
		Env: func(key string) (string, bool) {
			if key == "EMAIL_API_BASE" {
				return "https://email.example.com", true
			}
			return "", false
		},
	})
	must(err)
	resolved, err := plan.Resolve(&bcl.RuntimeContext{
		Input: map[string]any{
			"to":          "customer@example.com",
			"subject":     "Welcome",
			"body":        "Hello from BCL runtime execution",
			"schedule_id": "sched-20260625",
		},
		Request: map[string]any{"headers": map[string]any{"X_Request_ID": "req-demo-001"}},
		Session: map[string]any{"tenant_id": "acme"},
		Runtime: map[string]any{"uuid": "msg-demo-001", "run_id": "run-demo-001"},
		Secrets: secrets{"EMAIL_API_KEY": "secret-runtime-token"},
		Now: func() time.Time {
			return time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
		},
	})
	must(err)

	out, _ := json.MarshalIndent(resolved, "", "  ")
	fmt.Println("--- resolved runtime payload ---")
	fmt.Println(string(out))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

package condition

import (
	"context"
	"testing"
)

func TestContextFactsAndSessionHelpers(t *testing.T) {
	ctx := WithContextFacts(context.Background(), map[string]any{
		"tenant": map[string]any{"id": "body-tenant"},
	})
	ctx = WithContextValue(ctx, "request.id", "req-1")
	ctx = WithContextValue(ctx, "tenant.region", "us")
	ctx = WithRequestValue(ctx, "path", "/v1/definitions/demo/evaluate")
	ctx = WithRequest(ctx, map[string]any{"method": "POST"})
	ctx = WithSession(ctx, map[string]any{"id": "sess-1"})
	ctx = WithSessionValue(ctx, "attrs.mfa", true)

	facts := ContextFactsFromContext(ctx)
	if lookupMapValue(facts, "tenant.id") != "body-tenant" || lookupMapValue(facts, "tenant.region") != "us" || lookupMapValue(facts, "request.id") != "req-1" {
		t.Fatalf("facts = %#v", facts)
	}
	if lookupMapValue(facts, "request.path") != "/v1/definitions/demo/evaluate" || lookupMapValue(facts, "request.method") != "POST" {
		t.Fatalf("facts = %#v", facts)
	}
	request := RequestFromContext(ctx)
	if lookupMapValue(request, "path") != "/v1/definitions/demo/evaluate" || lookupMapValue(request, "method") != "POST" {
		t.Fatalf("request = %#v", request)
	}
	session := SessionFromContext(ctx)
	if lookupMapValue(session, "id") != "sess-1" || lookupMapValue(session, "attrs.mfa") != true {
		t.Fatalf("session = %#v", session)
	}

	facts["tenant"].(map[string]any)["id"] = "mutated"
	if got := lookupMapValue(ContextFactsFromContext(ctx), "tenant.id"); got != "body-tenant" {
		t.Fatalf("facts were mutated through returned map: %v", got)
	}
}

func TestContextWithSubjectMirrorsSubjectFact(t *testing.T) {
	ctx := ContextWithSubject(context.Background(), "user-1")
	if SubjectFromContext(ctx) != "user-1" {
		t.Fatalf("subject = %q", SubjectFromContext(ctx))
	}
	if got := lookupMapValue(ContextFactsFromContext(ctx), "subject.id"); got != "user-1" {
		t.Fatalf("subject fact = %#v", ContextFactsFromContext(ctx))
	}
}

func TestNewContextExposesFactsAndSession(t *testing.T) {
	base := WithContextValue(context.Background(), "tenant.id", "tenant-1")
	base = WithRequestValue(base, "id", "req-1")
	base = WithSessionValue(base, "id", "sess-1")
	ctx := NewContext(base)
	if lookupMapValue(ctx.Facts(), "tenant.id") != "tenant-1" {
		t.Fatalf("facts = %#v", ctx.Facts())
	}
	if lookupMapValue(ctx.Request(), "id") != "req-1" {
		t.Fatalf("request = %#v", ctx.Request())
	}
	if lookupMapValue(ctx.Session(), "id") != "sess-1" {
		t.Fatalf("session = %#v", ctx.Session())
	}
}

func lookupMapValue(values map[string]any, path string) any {
	var cur any = values
	for _, part := range splitPath(path) {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

package routing

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestMatcherMixedSyntaxAndPrecedence(t *testing.T) {
	matcher, err := Compile([]Route{
		{ID: "catch", Method: http.MethodGet, Pattern: "/files/{rest...}"},
		{ID: "wild", Method: http.MethodGet, Pattern: "/files/*"},
		{ID: "param", Method: http.MethodGet, Pattern: "/files/{id}"},
		{ID: "static", Method: http.MethodGet, Pattern: "/files/current"},
		{ID: "colon", Method: http.MethodPost, Pattern: "/users/:id"},
		{ID: "star_name", Method: http.MethodGet, Pattern: "/assets/*path"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method string
		path   string
		id     string
		param  string
		value  string
		fiber  string
	}{
		{http.MethodGet, "/files/current", "static", "", "", "/files/current"},
		{http.MethodGet, "/files/alpha", "param", "id", "alpha", "/files/:id"},
		{http.MethodGet, "/files/a/b", "catch", "rest", "a/b", "/files/*rest"},
		{http.MethodPost, "/users/u%201", "colon", "id", "u 1", "/users/:id"},
		{http.MethodGet, "/assets/css/app.css", "star_name", "path", "css/app.css", "/assets/*path"},
	}
	for _, tc := range cases {
		got := matcher.Match(tc.method, tc.path)
		if !got.Matched || got.ID != tc.id {
			t.Fatalf("%s %s matched %#v, want %s", tc.method, tc.path, got, tc.id)
		}
		if tc.param != "" && got.Params[tc.param] != tc.value {
			t.Fatalf("param %s = %q, want %q in %#v", tc.param, got.Params[tc.param], tc.value, got)
		}
		if got.FiberPattern != tc.fiber {
			t.Fatalf("fiber pattern = %q, want %q", got.FiberPattern, tc.fiber)
		}
	}
	if got := matcher.Match(http.MethodDelete, "/files/current"); got.Matched {
		t.Fatalf("unexpected method match: %#v", got)
	}
}

func TestCompileRejectsInvalidPatterns(t *testing.T) {
	for _, route := range []Route{
		{ID: "empty"},
		{ID: "bad_catch", Pattern: "/a/{rest...}/b"},
		{ID: "empty_param", Pattern: "/a/{}"},
	} {
		if _, err := Compile([]Route{route}); err == nil {
			t.Fatalf("expected error for %#v", route)
		}
	}
}

func TestAnalyzeReportsRouteConflictsAndShadowing(t *testing.T) {
	diags := Analyze([]Route{
		{ID: "users", Method: http.MethodGet, Pattern: "/users/{id}"},
		{ID: "users", Method: http.MethodGet, Pattern: "/users/:name"},
		{ID: "files", Method: http.MethodGet, Pattern: "/files/{rest...}"},
		{ID: "file_static", Method: http.MethodGet, Pattern: "/files/current"},
		{ID: "bad", Method: http.MethodGet, Pattern: "/bad/{rest...}/x"},
	})
	for _, want := range []string{"duplicate route id", "duplicate route GET /users", "ambiguous route shape", "shadow or broadly overlap", "invalid pattern"} {
		if !routingDiagnosticsContain(diags, want) {
			t.Fatalf("missing %q diagnostic: %#v", want, diags)
		}
	}
}

func BenchmarkMatcherExact(b *testing.B) {
	matcher := benchmarkMatcher(b, 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !matcher.Match(http.MethodGet, "/api/v1/static/999").Matched {
			b.Fatal("miss")
		}
	}
}

func routingDiagnosticsContain(diags []Diagnostic, text string) bool {
	for _, diag := range diags {
		if strings.Contains(diag.Message, text) {
			return true
		}
	}
	return false
}

func BenchmarkMatcherParam(b *testing.B) {
	matcher := benchmarkMatcher(b, 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !matcher.Match(http.MethodGet, "/api/v1/users/abc").Matched {
			b.Fatal("miss")
		}
	}
}

func BenchmarkMatcherCatchAll(b *testing.B) {
	matcher := benchmarkMatcher(b, 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !matcher.Match(http.MethodGet, "/assets/css/app.css").Matched {
			b.Fatal("miss")
		}
	}
}

func BenchmarkMatcherMiss(b *testing.B) {
	matcher := benchmarkMatcher(b, 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if matcher.Match(http.MethodGet, "/nope").Matched {
			b.Fatal("hit")
		}
	}
}

func benchmarkMatcher(b *testing.B, n int) *Matcher {
	b.Helper()
	routes := []Route{
		{ID: "users", Method: http.MethodGet, Pattern: "/api/v1/users/{id}"},
		{ID: "assets", Method: http.MethodGet, Pattern: "/assets/{path...}"},
	}
	for i := 0; i < n; i++ {
		routes = append(routes, Route{ID: fmt.Sprintf("static_%d", i), Method: http.MethodGet, Pattern: fmt.Sprintf("/api/v1/static/%d", i)})
	}
	matcher, err := Compile(routes)
	if err != nil {
		b.Fatal(err)
	}
	return matcher
}

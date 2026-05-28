package server

import (
	"net/http"

	"github.com/oarkflow/authz"
	"github.com/oarkflow/condition/pkg/routing"
)

type routeSpec struct {
	method  string
	pattern string
	handler http.HandlerFunc
}

func (s *Server) routes() []routeSpec {
	return []routeSpec{
		{http.MethodGet, "/healthz", s.health},
		{http.MethodGet, "/readyz", s.ready},
		{http.MethodGet, "/v1/readiness", s.productionReadiness},
		{http.MethodPost, "/v1/definitions/validate", s.validate},
		{http.MethodPost, "/v1/definitions", s.publish},
		{http.MethodGet, "/v1/definitions", s.listDefinitions},
		{http.MethodGet, "/v1/definitions/{name}", s.getDefinition},
		{http.MethodGet, "/v1/definitions/{name}/versions", s.listVersions},
		{http.MethodPost, "/v1/definitions/{name}/versions/{version}/approve", s.approve},
		{http.MethodPost, "/v1/definitions/{name}/versions/{version}/activate", s.activate},
		{http.MethodPost, "/v1/definitions/{name}/disable", s.disable},
		{http.MethodPost, "/v1/definitions/{name}/enable", s.enable},
		{http.MethodPost, "/v1/definitions/{name}/rollback", s.rollback},
		{http.MethodPost, "/v1/definitions/{name}/evaluate", s.evaluate},
		{http.MethodPost, "/v1/definitions/{name}/chains/{chain}/evaluate", s.evaluateChain},
		{http.MethodPost, "/v1/definitions/{name}/lifecycles/{lifecycle}/evaluate", s.evaluateLifecycle},
		{http.MethodPost, "/v1/definitions/{name}/tests", s.test},
		{http.MethodPost, "/v1/definitions/{name}/gates", s.gates},
		{http.MethodPost, "/v1/definitions/{name}/simulate", s.simulate},
		{http.MethodPost, "/v1/definitions/{name}/compare", s.compare},
		{http.MethodPost, "/v1/definitions/{name}/explain", s.explainPackage},
		{http.MethodGet, "/v1/definitions/{name}/route-coverage", s.routeCoverage},
		{http.MethodPost, "/v1/definitions/{name}/canary", s.canary},
		{http.MethodPost, "/v1/definitions/{name}/workflows/{workflow}/start", s.startWorkflow},
		{http.MethodPost, "/v1/workflows/{id}/advance", s.advanceWorkflow},
		{http.MethodGet, "/v1/workflows/{id}", s.getWorkflow},
		{http.MethodGet, "/v1/workflows", s.listWorkflows},
		{http.MethodGet, "/v1/audits", s.listAudits},
		{http.MethodGet, "/v1/audits/{id}", s.getAudit},
		{http.MethodPost, "/v1/audits/verify", s.verifyAudits},
		{http.MethodGet, "/v1/actions", s.listActions},
		{http.MethodGet, "/v1/incidents", s.listIncidents},
		{http.MethodPost, "/v1/state/compact", s.compactState},
		{http.MethodGet, "/v1/reports", s.listReports},
		{http.MethodGet, "/v1/metrics", s.metricsEndpoint},
		{http.MethodPost, "/v1/reload", s.reload},
	}
}

func (s *Server) routeResourceFromRequest(r *http.Request) *authz.Resource {
	match := s.matchRoute(r.Method, r.URL.Path)
	pattern := match.NormalizedPattern
	if pattern == "" {
		pattern = r.URL.Path
	}
	attrs := map[string]any{
		"method":         r.Method,
		"path":           r.URL.Path,
		"route_template": pattern,
		"route_pattern":  firstNonEmpty(match.FiberPattern, routing.FiberPattern(pattern)),
		"params":         match.Params,
	}
	for key, value := range match.Params {
		attrs[key] = value
	}
	return &authz.Resource{
		ID:       r.Method + ":" + firstNonEmpty(match.FiberPattern, routing.FiberPattern(pattern)),
		Type:     "route",
		TenantID: r.Header.Get("X-Tenant-ID"),
		Attrs:    attrs,
	}
}

func matchRoutePattern(routes []routeSpec, method, path string) (string, map[string]string) {
	matcher, err := routing.Compile(serverRoutesForMatcher(routes))
	if err != nil {
		return "", nil
	}
	match := matcher.Match(method, path)
	if !match.Matched {
		return "", nil
	}
	return match.NormalizedPattern, match.Params
}

func matchPathPattern(pattern, path string) (map[string]string, bool) {
	matcher, err := routing.Compile([]routing.Route{{Method: http.MethodGet, Pattern: pattern}})
	if err != nil {
		return nil, false
	}
	match := matcher.Match(http.MethodGet, path)
	return match.Params, match.Matched
}

func (s *Server) matchRoute(method, path string) routing.Match {
	if s != nil && s.routeMatcher != nil {
		return s.routeMatcher.Match(method, path)
	}
	return routing.Match{}
}

func serverRoutesForMatcher(routes []routeSpec) []routing.Route {
	out := make([]routing.Route, 0, len(routes))
	for _, route := range routes {
		out = append(out, routing.Route{Method: route.method, Pattern: route.pattern})
	}
	return out
}

func fiberRoutePattern(pattern string) string {
	return routing.FiberPattern(pattern)
}

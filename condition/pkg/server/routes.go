package server

import (
	"net/http"
	"strings"

	"github.com/oarkflow/authz"
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
		{http.MethodPost, "/v1/definitions/validate", s.validate},
		{http.MethodPost, "/v1/definitions", s.publish},
		{http.MethodGet, "/v1/definitions", s.listDefinitions},
		{http.MethodGet, "/v1/definitions/{name}", s.getDefinition},
		{http.MethodGet, "/v1/definitions/{name}/versions", s.listVersions},
		{http.MethodPost, "/v1/definitions/{name}/versions/{version}/activate", s.activate},
		{http.MethodPost, "/v1/definitions/{name}/rollback", s.rollback},
		{http.MethodPost, "/v1/definitions/{name}/evaluate", s.evaluate},
		{http.MethodPost, "/v1/definitions/{name}/tests", s.test},
		{http.MethodPost, "/v1/definitions/{name}/gates", s.gates},
		{http.MethodPost, "/v1/definitions/{name}/simulate", s.simulate},
		{http.MethodPost, "/v1/definitions/{name}/compare", s.compare},
		{http.MethodPost, "/v1/definitions/{name}/workflows/{workflow}/start", s.startWorkflow},
		{http.MethodPost, "/v1/workflows/{id}/advance", s.advanceWorkflow},
		{http.MethodGet, "/v1/workflows/{id}", s.getWorkflow},
		{http.MethodGet, "/v1/workflows", s.listWorkflows},
		{http.MethodGet, "/v1/audits", s.listAudits},
		{http.MethodGet, "/v1/audits/{id}", s.getAudit},
		{http.MethodPost, "/v1/audits/verify", s.verifyAudits},
		{http.MethodGet, "/v1/reports", s.listReports},
		{http.MethodGet, "/v1/metrics", s.metricsEndpoint},
		{http.MethodPost, "/v1/reload", s.reload},
	}
}

func (s *Server) routeResourceFromRequest(r *http.Request) *authz.Resource {
	pattern, params := matchRoutePattern(s.routes(), r.Method, r.URL.Path)
	if pattern == "" {
		pattern = r.URL.Path
	}
	attrs := map[string]any{
		"method":         r.Method,
		"path":           r.URL.Path,
		"route_template": pattern,
		"route_pattern":  fiberRoutePattern(pattern),
		"params":         params,
	}
	for key, value := range params {
		attrs[key] = value
	}
	return &authz.Resource{
		ID:       r.Method + ":" + fiberRoutePattern(pattern),
		Type:     "route",
		TenantID: r.Header.Get("X-Tenant-ID"),
		Attrs:    attrs,
	}
}

func matchRoutePattern(routes []routeSpec, method, path string) (string, map[string]string) {
	for _, route := range routes {
		if route.method != method {
			continue
		}
		params, ok := matchPathPattern(route.pattern, path)
		if ok {
			return route.pattern, params
		}
	}
	return "", nil
}

func matchPathPattern(pattern, path string) (map[string]string, bool) {
	patternParts := splitPath(pattern)
	pathParts := splitPath(path)
	params := map[string]string{}
	for i, part := range patternParts {
		if isCatchAll(part) {
			name := paramName(part)
			if name != "" {
				params[name] = strings.Join(pathParts[i:], "/")
			}
			return params, true
		}
		if i >= len(pathParts) {
			return nil, false
		}
		if isParam(part) {
			params[paramName(part)] = pathParts[i]
			continue
		}
		if part == "*" {
			continue
		}
		if part != pathParts[i] {
			return nil, false
		}
	}
	if len(patternParts) != len(pathParts) {
		return nil, false
	}
	return params, true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func fiberRoutePattern(pattern string) string {
	parts := splitPath(pattern)
	for i, part := range parts {
		if isParam(part) {
			parts[i] = ":" + paramName(part)
		}
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

func isParam(part string) bool {
	return strings.HasPrefix(part, ":") || (strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"))
}

func isCatchAll(part string) bool {
	return strings.HasPrefix(part, "*") || (strings.HasPrefix(part, "{") && strings.HasSuffix(part, "...}"))
}

func paramName(part string) string {
	part = strings.TrimPrefix(part, ":")
	part = strings.TrimPrefix(part, "*")
	part = strings.TrimPrefix(part, "{")
	part = strings.TrimSuffix(part, "}")
	part = strings.TrimSuffix(part, "...")
	return part
}

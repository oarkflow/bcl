package routing

import (
	"fmt"
	"net/url"
	"strings"
)

type Route struct {
	ID       string         `json:"id,omitempty"`
	Method   string         `json:"method,omitempty"`
	Pattern  string         `json:"pattern"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Match struct {
	Matched           bool              `json:"matched"`
	ID                string            `json:"id,omitempty"`
	Method            string            `json:"method,omitempty"`
	Pattern           string            `json:"pattern,omitempty"`
	NormalizedPattern string            `json:"normalized_pattern,omitempty"`
	FiberPattern      string            `json:"fiber_pattern,omitempty"`
	Params            map[string]string `json:"params,omitempty"`
	Metadata          map[string]any    `json:"metadata,omitempty"`
	Specificity       int               `json:"specificity,omitempty"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	RouteID  string `json:"route_id,omitempty"`
	Method   string `json:"method,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
}

type Matcher struct {
	methods map[string]*node
}

type node struct {
	static   map[string]*node
	param    *edge
	wildcard *edge
	catchAll *edge
	route    *compiledRoute
}

type edge struct {
	name string
	next *node
}

type compiledRoute struct {
	route       Route
	normalized  string
	fiber       string
	specificity int
	order       int
}

type segment struct {
	raw     string
	kind    segmentKind
	name    string
	literal string
}

type segmentKind int

const (
	segmentStatic segmentKind = iota
	segmentParam
	segmentWildcard
	segmentCatchAll
)

func Compile(routes []Route) (*Matcher, error) {
	m := &Matcher{methods: map[string]*node{}}
	for i, route := range routes {
		if err := m.add(route, i); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func MustCompile(routes []Route) *Matcher {
	m, err := Compile(routes)
	if err != nil {
		panic(err)
	}
	return m
}

func Analyze(routes []Route) []Diagnostic {
	type parsedRoute struct {
		route      Route
		parts      []segment
		normalized string
		shape      string
	}
	var diags []Diagnostic
	ids := map[string]Route{}
	keys := map[string]Route{}
	shapes := map[string]Route{}
	var parsed []parsedRoute
	for _, route := range routes {
		route.Method = strings.ToUpper(strings.TrimSpace(route.Method))
		parts, err := parsePattern(route.Pattern)
		if err != nil {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("route %q has invalid pattern %q: %v", first(route.ID, route.Pattern), route.Pattern, err), RouteID: route.ID, Method: route.Method, Pattern: route.Pattern})
			continue
		}
		if strings.TrimSpace(route.ID) != "" {
			if prev, ok := ids[route.ID]; ok {
				diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate route id %q for %s and %s", route.ID, prev.Pattern, route.Pattern), RouteID: route.ID, Method: route.Method, Pattern: route.Pattern})
			} else {
				ids[route.ID] = route
			}
		}
		normalized := renderPattern(parts, false)
		key := route.Method + " " + normalized
		if prev, ok := keys[key]; ok {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate route %s %s conflicts with route %q", route.Method, normalized, prev.ID), RouteID: route.ID, Method: route.Method, Pattern: route.Pattern})
		} else {
			keys[key] = route
		}
		shape := route.Method + " " + patternShape(parts)
		if prev, ok := shapes[shape]; ok && prev.Pattern != route.Pattern {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate route %s %s conflicts with route %q pattern %q", route.Method, patternConflictTemplate(parts), prev.ID, prev.Pattern), RouteID: route.ID, Method: route.Method, Pattern: route.Pattern})
			diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("ambiguous route shape %s overlaps route %q pattern %q", patternShape(parts), prev.ID, prev.Pattern), RouteID: route.ID, Method: route.Method, Pattern: route.Pattern})
		} else {
			shapes[shape] = route
		}
		parsed = append(parsed, parsedRoute{route: route, parts: parts, normalized: normalized, shape: patternShape(parts)})
	}
	for i := range parsed {
		for j := range parsed {
			if i == j || parsed[i].route.Method != parsed[j].route.Method {
				continue
			}
			if routeCanShadow(parsed[i].parts, parsed[j].parts) {
				diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("route %q pattern %q can shadow or broadly overlap route %q pattern %q", parsed[i].route.ID, parsed[i].route.Pattern, parsed[j].route.ID, parsed[j].route.Pattern), RouteID: parsed[i].route.ID, Method: parsed[i].route.Method, Pattern: parsed[i].route.Pattern})
			}
		}
	}
	return diags
}

func (m *Matcher) Match(method, path string) Match {
	if m == nil {
		return Match{}
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	root := m.methods[method]
	if root == nil {
		root = m.methods[""]
	}
	if root == nil {
		return Match{}
	}
	params := map[string]string(nil)
	route, params, ok := matchNode(root, cleanPath(path), 0, params)
	if !ok || route == nil {
		return Match{}
	}
	return Match{
		Matched:           true,
		ID:                route.route.ID,
		Method:            route.route.Method,
		Pattern:           route.route.Pattern,
		NormalizedPattern: route.normalized,
		FiberPattern:      route.fiber,
		Params:            params,
		Metadata:          cloneMap(route.route.Metadata),
		Specificity:       route.specificity,
	}
}

func Normalize(pattern string) (string, error) {
	parts, err := parsePattern(pattern)
	if err != nil {
		return "", err
	}
	return renderPattern(parts, false), nil
}

func FiberPattern(pattern string) string {
	parts, err := parsePattern(pattern)
	if err != nil {
		return pattern
	}
	return renderPattern(parts, true)
}

func (m *Matcher) add(route Route, order int) error {
	route.Method = strings.ToUpper(strings.TrimSpace(route.Method))
	parts, err := parsePattern(route.Pattern)
	if err != nil {
		return fmt.Errorf("route %q: %w", first(route.ID, route.Pattern), err)
	}
	root := m.methods[route.Method]
	if root == nil {
		root = &node{}
		m.methods[route.Method] = root
	}
	cur := root
	for _, part := range parts {
		switch part.kind {
		case segmentStatic:
			if cur.static == nil {
				cur.static = map[string]*node{}
			}
			if cur.static[part.literal] == nil {
				cur.static[part.literal] = &node{}
			}
			cur = cur.static[part.literal]
		case segmentParam:
			if cur.param == nil {
				cur.param = &edge{name: part.name, next: &node{}}
			}
			cur = cur.param.next
		case segmentWildcard:
			if cur.wildcard == nil {
				cur.wildcard = &edge{name: part.name, next: &node{}}
			}
			cur = cur.wildcard.next
		case segmentCatchAll:
			if cur.catchAll == nil {
				cur.catchAll = &edge{name: part.name, next: &node{}}
			}
			cur = cur.catchAll.next
		}
	}
	if cur.route != nil {
		return fmt.Errorf("duplicate route pattern %q", route.Pattern)
	}
	cur.route = &compiledRoute{
		route:       route,
		normalized:  renderPattern(parts, false),
		fiber:       renderPattern(parts, true),
		specificity: specificity(parts),
		order:       order,
	}
	return nil
}

func matchNode(cur *node, path string, pos int, params map[string]string) (*compiledRoute, map[string]string, bool) {
	if cur == nil {
		return nil, nil, false
	}
	part, nextPos, okPart := nextSegment(path, pos)
	if !okPart {
		if cur.route != nil {
			return cur.route, params, true
		}
		if cur.catchAll != nil && cur.catchAll.next.route != nil {
			params = setParam(params, cur.catchAll.name, "")
			return cur.catchAll.next.route, params, true
		}
		return nil, nil, false
	}
	if cur.static != nil {
		if nextNode := cur.static[part]; nextNode != nil {
			if route, p, ok := matchNode(nextNode, path, nextPos, params); ok {
				return route, p, true
			}
		}
	}
	if cur.param != nil {
		nextParams := setParam(params, cur.param.name, decode(part))
		if route, p, ok := matchNode(cur.param.next, path, nextPos, nextParams); ok {
			return route, p, true
		}
	}
	if cur.wildcard != nil {
		nextParams := params
		if cur.wildcard.name != "" {
			nextParams = setParam(params, cur.wildcard.name, decode(part))
		}
		if route, p, ok := matchNode(cur.wildcard.next, path, nextPos, nextParams); ok {
			return route, p, true
		}
	}
	if cur.catchAll != nil && cur.catchAll.next.route != nil {
		params = setParam(params, cur.catchAll.name, decode(path[pos:]))
		return cur.catchAll.next.route, params, true
	}
	return nil, nil, false
}

func parsePattern(pattern string) ([]segment, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	if pattern == "/" {
		return nil, nil
	}
	raw := strings.Split(strings.Trim(pattern, "/"), "/")
	out := make([]segment, 0, len(raw))
	for i, part := range raw {
		if part == "" {
			continue
		}
		seg := segment{raw: part, literal: part}
		switch {
		case part == "*":
			seg.kind = segmentWildcard
		case strings.HasPrefix(part, "*"):
			seg.kind = segmentCatchAll
			seg.name = strings.TrimPrefix(part, "*")
		case strings.HasPrefix(part, ":"):
			seg.kind = segmentParam
			seg.name = strings.TrimPrefix(part, ":")
		case strings.HasPrefix(part, "{") && strings.HasSuffix(part, "...}"):
			seg.kind = segmentCatchAll
			seg.name = strings.TrimSuffix(strings.TrimPrefix(part, "{"), "...}")
		case strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"):
			seg.kind = segmentParam
			seg.name = strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
		default:
			seg.kind = segmentStatic
		}
		if (seg.kind == segmentParam || seg.kind == segmentCatchAll) && seg.name == "" {
			return nil, fmt.Errorf("empty parameter in pattern %q", pattern)
		}
		if seg.kind == segmentCatchAll && i != len(raw)-1 {
			return nil, fmt.Errorf("catch-all must be the final segment in pattern %q", pattern)
		}
		out = append(out, seg)
	}
	return out, nil
}

func cleanPath(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	return strings.Trim(path, "/")
}

func nextSegment(path string, start int) (segment string, next int, ok bool) {
	if start >= len(path) {
		return "", start, false
	}
	end := strings.IndexByte(path[start:], '/')
	if end < 0 {
		return path[start:], len(path), true
	}
	end += start
	return path[start:end], end + 1, true
}

func renderPattern(parts []segment, fiber bool) string {
	if len(parts) == 0 {
		return "/"
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.kind {
		case segmentStatic:
			out = append(out, part.literal)
		case segmentParam:
			if fiber {
				out = append(out, ":"+part.name)
			} else {
				out = append(out, "{"+part.name+"}")
			}
		case segmentWildcard:
			out = append(out, "*")
		case segmentCatchAll:
			if fiber {
				out = append(out, "*"+part.name)
			} else {
				out = append(out, "{"+part.name+"...}")
			}
		}
	}
	return "/" + strings.Join(out, "/")
}

func specificity(parts []segment) int {
	score := 0
	for _, part := range parts {
		switch part.kind {
		case segmentStatic:
			score += 100
		case segmentParam:
			score += 50
		case segmentWildcard:
			score += 10
		case segmentCatchAll:
			score += 1
		}
	}
	return score
}

func patternShape(parts []segment) string {
	if len(parts) == 0 {
		return "/"
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.kind {
		case segmentStatic:
			out = append(out, "="+part.literal)
		case segmentParam:
			out = append(out, ":")
		case segmentWildcard:
			out = append(out, "*")
		case segmentCatchAll:
			out = append(out, "**")
		}
	}
	return "/" + strings.Join(out, "/")
}

func patternConflictTemplate(parts []segment) string {
	if len(parts) == 0 {
		return "/"
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.kind {
		case segmentStatic:
			out = append(out, part.literal)
		case segmentParam:
			out = append(out, "{param}")
		case segmentWildcard:
			out = append(out, "*")
		case segmentCatchAll:
			out = append(out, "{rest...}")
		}
	}
	return "/" + strings.Join(out, "/")
}

func routeCanShadow(a, b []segment) bool {
	for i := 0; i < len(a); i++ {
		if i >= len(b) {
			return false
		}
		switch a[i].kind {
		case segmentCatchAll:
			return len(b) > i
		case segmentWildcard:
			if b[i].kind == segmentStatic || b[i].kind == segmentParam || b[i].kind == segmentWildcard {
				continue
			}
			return false
		case segmentParam:
			if b[i].kind == segmentStatic || b[i].kind == segmentParam {
				continue
			}
			return false
		case segmentStatic:
			if b[i].kind != segmentStatic || a[i].literal != b[i].literal {
				return false
			}
		}
	}
	return false
}

func setParam(params map[string]string, key, value string) map[string]string {
	if key == "" {
		return params
	}
	out := make(map[string]string, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	out[key] = value
	return out
}

func decode(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

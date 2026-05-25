package condition

import "context"

type contextKey string

const (
	subjectContextKey contextKey = "condition-subject"
	factsContextKey   contextKey = "condition-context-facts"
	requestContextKey contextKey = "condition-request"
	sessionContextKey contextKey = "condition-session"
)

type Context interface {
	context.Context
	Facts() map[string]any
	Request() map[string]any
	Session() map[string]any
}

type conditionContext struct {
	context.Context
}

func NewContext(parent context.Context) Context {
	if parent == nil {
		parent = context.Background()
	}
	return conditionContext{Context: parent}
}

func ContextWithSubject(ctx context.Context, subject string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, subjectContextKey, subject)
	if subject == "" {
		return ctx
	}
	return WithContextValue(ctx, "subject.id", subject)
}

func SubjectFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if subject, ok := ctx.Value(subjectContextKey).(string); ok {
		return subject
	}
	return ""
}

func WithContextFacts(ctx context.Context, facts map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := mergeMaps(ContextFactsFromContext(ctx), facts)
	return context.WithValue(ctx, factsContextKey, merged)
}

func ContextFactsFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	if c, ok := ctx.(Context); ok {
		if _, self := c.(conditionContext); !self {
			return cloneMap(c.Facts())
		}
	}
	if facts, ok := ctx.Value(factsContextKey).(map[string]any); ok {
		return cloneMap(facts)
	}
	return nil
}

func WithContextValue(ctx context.Context, path string, value any) context.Context {
	facts := ContextFactsFromContext(ctx)
	if facts == nil {
		facts = map[string]any{}
	}
	setPathValue(facts, path, value)
	return context.WithValue(nonNilContext(ctx), factsContextKey, facts)
}

func WithSession(ctx context.Context, session map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := mergeMaps(SessionFromContext(ctx), session)
	return context.WithValue(ctx, sessionContextKey, merged)
}

func WithRequest(ctx context.Context, request map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := mergeMaps(RequestFromContext(ctx), request)
	ctx = context.WithValue(ctx, requestContextKey, merged)
	return WithContextFacts(ctx, map[string]any{"request": merged})
}

func RequestFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	if c, ok := ctx.(Context); ok {
		if _, self := c.(conditionContext); !self {
			return cloneMap(c.Request())
		}
	}
	if request, ok := ctx.Value(requestContextKey).(map[string]any); ok {
		return cloneMap(request)
	}
	if facts := ContextFactsFromContext(ctx); facts != nil {
		if request, ok := facts["request"].(map[string]any); ok {
			return cloneMap(request)
		}
	}
	return nil
}

func WithRequestValue(ctx context.Context, path string, value any) context.Context {
	request := RequestFromContext(ctx)
	if request == nil {
		request = map[string]any{}
	}
	setPathValue(request, path, value)
	return WithRequest(ctx, request)
}

func SessionFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	if c, ok := ctx.(Context); ok {
		if _, self := c.(conditionContext); !self {
			return cloneMap(c.Session())
		}
	}
	if session, ok := ctx.Value(sessionContextKey).(map[string]any); ok {
		return cloneMap(session)
	}
	return nil
}

func WithSessionValue(ctx context.Context, path string, value any) context.Context {
	session := SessionFromContext(ctx)
	if session == nil {
		session = map[string]any{}
	}
	setPathValue(session, path, value)
	return context.WithValue(nonNilContext(ctx), sessionContextKey, session)
}

func (c conditionContext) Facts() map[string]any {
	return ContextFactsFromContext(c.Context)
}

func (c conditionContext) Request() map[string]any {
	return RequestFromContext(c.Context)
}

func (c conditionContext) Session() map[string]any {
	return SessionFromContext(c.Context)
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if nested, ok := v.(map[string]any); ok {
			out[k] = cloneMap(nested)
			continue
		}
		out[k] = v
	}
	return out
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	out := cloneMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range overlay {
		if nested, ok := v.(map[string]any); ok {
			if existing, ok := out[k].(map[string]any); ok {
				out[k] = mergeMaps(existing, nested)
			} else {
				out[k] = cloneMap(nested)
			}
			continue
		}
		out[k] = v
	}
	return out
}

func setPathValue(dst map[string]any, path string, value any) {
	if dst == nil || path == "" {
		return
	}
	parts := splitPath(path)
	if len(parts) == 0 {
		return
	}
	cur := dst
	for _, part := range parts[:len(parts)-1] {
		next, _ := cur[part].(map[string]any)
		if next == nil {
			next = map[string]any{}
			cur[part] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

func splitPath(path string) []string {
	var parts []string
	for path != "" {
		part := path
		if i := indexByte(path, '.'); i >= 0 {
			part = path[:i]
			path = path[i+1:]
		} else {
			path = ""
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

package condition

import "context"

type contextKey string

const subjectContextKey contextKey = "condition-subject"

func ContextWithSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, subjectContextKey, subject)
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

package storage

import (
	"context"
	"strings"
	"time"
)

type tenantContextKey struct{}

func ContextWithTenant(ctx context.Context, tenant string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tenantContextKey{}, firstTenant(tenant))
}

func TenantFromContext(ctx context.Context) string {
	if ctx == nil {
		return "default"
	}
	if tenant, ok := ctx.Value(tenantContextKey{}).(string); ok {
		return firstTenant(tenant)
	}
	return "default"
}

func definitionVersionKey(tenant, name, version, environment string) string {
	return firstTenant(tenant) + "\x00" + strings.TrimSpace(name) + "\x00" + strings.TrimSpace(version) + "\x00" + firstEnv(environment)
}

func definitionActiveKey(tenant, name, environment string) string {
	return firstTenant(tenant) + "\x00" + strings.TrimSpace(name) + "\x00" + firstEnv(environment)
}

func chainStateKey(tenant, chain, watch, entityKey string) string {
	return firstTenant(tenant) + "\x00" + strings.TrimSpace(chain) + "\x00" + strings.TrimSpace(watch) + "\x00" + strings.TrimSpace(entityKey)
}

func firstEnv(environment string) string {
	if strings.TrimSpace(environment) == "" {
		return "development"
	}
	return strings.TrimSpace(environment)
}

func firstTenant(tenant string) string {
	if strings.TrimSpace(tenant) == "" {
		return "default"
	}
	return strings.TrimSpace(tenant)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nowUTC() time.Time { return time.Now().UTC() }

func inRange(t time.Time, opts ListOptions) bool {
	if opts.Since != nil && t.Before(*opts.Since) {
		return false
	}
	if opts.Until != nil && t.After(*opts.Until) {
		return false
	}
	return true
}

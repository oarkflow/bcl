package storage

import (
	"strings"
	"time"
)

func definitionVersionKey(name, version, environment string) string {
	return strings.TrimSpace(name) + "\x00" + strings.TrimSpace(version) + "\x00" + firstEnv(environment)
}

func definitionActiveKey(name, environment string) string {
	return strings.TrimSpace(name) + "\x00" + firstEnv(environment)
}

func firstEnv(environment string) string {
	if strings.TrimSpace(environment) == "" {
		return "development"
	}
	return strings.TrimSpace(environment)
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

package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oarkflow/bcl"
)

type Config struct {
	Name     string            `bcl:"name"`
	Enabled  bool              `bcl:"enabled"`
	Workers  int               `bcl:"workers"`
	CacheTTL string            `bcl:"cache_ttl"`
	Endpoint Endpoint          `bcl:"public_endpoint"`
	Server   ServerConfig      `bcl:"server"`
	Database DatabaseConfig    `bcl:"database"`
	Features map[string]bool   `bcl:"features"`
	Labels   map[string]string `bcl:"labels,omitempty"`
	Routes   []RouteConfig     `bcl:"routes"`
	Metadata []map[string]any  `bcl:"metadata,omitempty"`
}

// Endpoint is a custom user type. It is not a string alias: it has its own
// structure and teaches BCL marshal/unmarshal through encoding.TextMarshaler
// and encoding.TextUnmarshaler.
type Endpoint struct {
	Scheme string
	Host   string
	Port   string
	Path   string
}

func (e *Endpoint) UnmarshalText(text []byte) error {
	u, err := url.Parse(string(text))
	if err != nil {
		return err
	}
	e.Scheme = u.Scheme
	e.Host = u.Hostname()
	e.Port = u.Port()
	e.Path = strings.TrimPrefix(u.Path, "/")
	return nil
}

func (e Endpoint) MarshalText() ([]byte, error) {
	host := e.Host
	if e.Port != "" {
		host += ":" + e.Port
	}
	u := url.URL{Scheme: e.Scheme, Host: host, Path: "/" + strings.TrimPrefix(e.Path, "/")}
	return []byte(u.String()), nil
}

type ServerConfig struct {
	Host           string   `bcl:"host"`
	Port           int      `bcl:"port"`
	TrustedProxies []string `bcl:"trusted_proxies"`
}

type DatabaseConfig struct {
	Driver  string `bcl:"driver"`
	URL     string `bcl:"url"`
	MaxOpen int    `bcl:"max_open"`
	// Sensitive fields are redacted into sensitive(...) when marshaled.
	Password string `bcl:"password,sensitive"`
}

type RouteConfig struct {
	Name    string         `bcl:"name"`
	Path    string         `bcl:"path"`
	Methods []string       `bcl:"methods"`
	Headers map[string]any `bcl:"headers,omitempty"`
}

func main() {
	input := filepath.Join(exampleDir(), "config.bcl")

	var cfg Config
	if err := bcl.DecodeFileWithOptions(input, &cfg, &bcl.Options{AllowEnv: true}); err != nil {
		log.Fatal(err)
	}

	cfg.Workers = 16
	cfg.Endpoint.Path = "v2/api"
	cfg.Features["hot_reload"] = true
	cfg.Labels = map[string]string{
		"owner": "platform",
		"tier":  "enterprise",
	}
	cfg.Routes = append(cfg.Routes, RouteConfig{
		Name:    "admin",
		Path:    "/admin",
		Methods: []string{"GET", "POST"},
		Headers: map[string]any{
			"x-owner": "platform",
			"mfa":     true,
		},
	})
	cfg.Metadata = []map[string]any{
		{"owner": "platform", "critical": true},
		{"owner": "security", "critical": false},
	}

	output := filepath.Join(os.TempDir(), "bcl-user-config.bcl")
	if err := bcl.EncodeFile(output, cfg); err != nil {
		log.Fatal(err)
	}

	encoded, err := os.ReadFile(output)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("decoded: %+v\n\n", cfg)
	fmt.Printf("encoded %s:\n%s", output, encoded)
}

func exampleDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("cannot locate example directory")
	}
	return filepath.Dir(file)
}

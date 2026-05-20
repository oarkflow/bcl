package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oarkflow/bcl"
)

type TCPGuardConfig struct {
	Pack         Pack
	Guard        Guard
	Safety       PolicySafety
	Datasources  []Datasource
	Lookups      []Lookup
	Actions      []Action
	Detectors    []Detector
	ThreatModels []ThreatModel
	Triggers     []Trigger
}

type Pack struct {
	Name    string
	Version string
	Mode    Mode
}

type Guard struct {
	Name     string
	Mode     Mode
	Version  string
	Timezone string
	Authz    Authz
	Includes []string
}

type Authz struct {
	File        string
	Strict      bool
	Timeout     Duration
	ErrorPolicy string
}

type PolicySafety struct {
	MaxDetectorTimeout Duration
	MaxLookupTimeout   Duration
	MaxActionTimeout   Duration
	MaxActionsPerRule  int
	AllowedSources     []string
}

type Datasource struct {
	Name         string
	Type         string
	Path         string
	URL          string
	Method       string
	Driver       string
	DSN          string
	CacheRefresh Duration
	Timeout      Duration
}

type Lookup struct {
	Name     string
	Source   string
	Mode     string
	Key      string
	Query    string
	Fallback string
}

type Action struct {
	Name         string
	Type         string
	Provider     string
	Subject      string
	SuccessCodes []string
	RetryCodes   []string
}

type Detector struct {
	Name string
	Type string
}

type ThreatModel struct {
	Name       string
	Categories []ThreatCategory
}

type ThreatCategory struct {
	Name     string
	Findings []string
}

type Trigger struct {
	Name   string
	Source string
	Emit   string
}

type Mode string

func (m *Mode) UnmarshalText(text []byte) error {
	*m = Mode(strings.ToLower(string(text)))
	return nil
}

func (m Mode) MarshalText() ([]byte, error) {
	return []byte(strings.ToLower(string(m))), nil
}

type Duration string

func (d *Duration) UnmarshalText(text []byte) error {
	*d = Duration(string(text))
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(string(d)), nil
}

type ConfigSummary struct {
	PackName        string   `bcl:"pack_name"`
	GuardName       string   `bcl:"guard_name"`
	Mode            Mode     `bcl:"mode"`
	AuthzTimeout    Duration `bcl:"authz_timeout"`
	Datasources     []string `bcl:"datasources"`
	Lookups         []string `bcl:"lookups"`
	Actions         []string `bcl:"actions"`
	Detectors       []string `bcl:"detectors"`
	ThreatModels    []string `bcl:"threat_models"`
	Triggers        []string `bcl:"triggers"`
	MaxActionBudget Duration `bcl:"max_action_budget"`
}

func main() {
	input := filepath.Join(exampleDir(), "config.bcl")
	normalized, err := bcl.CompileFile(input, &bcl.Options{
		AllowEnv: true,
		Env: func(key string) (string, bool) {
			if key == "ACCOUNT_DB_DSN" {
				return ":memory:", true
			}
			return os.LookupEnv(key)
		},
		Context: map[string]any{
			"request": map[string]any{
				"id":   "req-demo",
				"path": "/payments/approve",
			},
			"user": map[string]any{
				"id": "user-demo",
			},
			"tenant": map[string]any{
				"id": "tenant-demo",
			},
			"network": map[string]any{
				"ip": "10.10.0.25",
			},
		},
		Session: map[string]any{
			"id": "sess-demo",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	cfg := DecodeTCPGuard(normalized)
	summary := Summarize(cfg)

	out, err := bcl.Marshal(summary)
	if err != nil {
		log.Fatal(err)
	}
	output := filepath.Join(os.TempDir(), "bcl-tcpguard-summary.bcl")
	if err := os.WriteFile(output, out, 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("decoded tcpguard config: pack=%s datasources=%d lookups=%d actions=%d detectors=%d\n",
		cfg.Pack.Name, len(cfg.Datasources), len(cfg.Lookups), len(cfg.Actions), len(cfg.Detectors))
	fmt.Printf("encoded summary %s:\n%s", output, out)
}

func DecodeTCPGuard(n *bcl.Normalized) TCPGuardConfig {
	var cfg TCPGuardConfig
	for _, block := range n.Blocks {
		typ := stringValue(block["type"])
		id := stringValue(block["id"])
		body, _ := block["body"].(map[string]any)
		switch typ {
		case "pack":
			cfg.Pack = Pack{Name: id, Version: stringValue(body["version"]), Mode: Mode(stringValue(body["mode"]))}
		case "guard":
			cfg.Guard = Guard{
				Name:     id,
				Mode:     Mode(stringValue(body["mode"])),
				Version:  stringValue(body["version"]),
				Timezone: stringValue(body["timezone"]),
				Authz:    decodeAuthz(mapValue(body["authz"])),
				Includes: exprCommands(body["$expr"]),
			}
		case "datasource":
			cfg.Datasources = append(cfg.Datasources, decodeDatasource(id, body))
		case "lookup":
			cfg.Lookups = append(cfg.Lookups, decodeLookup(id, body))
		case "action":
			cfg.Actions = append(cfg.Actions, decodeAction(id, body))
		case "detector":
			cfg.Detectors = append(cfg.Detectors, Detector{Name: id, Type: stringValue(body["type"])})
		case "threat_model":
			cfg.ThreatModels = append(cfg.ThreatModels, decodeThreatModel(id, body))
		case "trigger":
			cfg.Triggers = append(cfg.Triggers, Trigger{Name: id, Source: refString(body["source"]), Emit: stringValue(body["emit"])})
		}
	}
	if safety, ok := n.Body["policy_safety"].(map[string]any); ok {
		cfg.Safety = PolicySafety{
			MaxDetectorTimeout: Duration(stringValue(safety["max_detector_timeout"])),
			MaxLookupTimeout:   Duration(stringValue(safety["max_lookup_timeout"])),
			MaxActionTimeout:   Duration(stringValue(safety["max_action_timeout"])),
			MaxActionsPerRule:  intValue(safety["max_actions_per_rule"]),
			AllowedSources:     stringList(safety["allow_datasource_types"]),
		}
	}
	return cfg
}

func decodeAuthz(body map[string]any) Authz {
	return Authz{
		File:        stringValue(body["file"]),
		Strict:      boolValue(body["strict"]),
		Timeout:     Duration(stringValue(body["timeout"])),
		ErrorPolicy: stringValue(body["error_policy"]),
	}
}

func decodeDatasource(id string, body map[string]any) Datasource {
	return Datasource{
		Name:         id,
		Type:         stringValue(body["type"]),
		Path:         stringValue(body["path"]),
		URL:          stringValue(body["url"]),
		Method:       stringValue(body["method"]),
		Driver:       stringValue(body["driver"]),
		DSN:          stringValue(body["dsn"]),
		CacheRefresh: Duration(stringValue(body["cache_refresh"])),
		Timeout:      Duration(stringValue(body["timeout"])),
	}
}

func decodeLookup(id string, body map[string]any) Lookup {
	return Lookup{
		Name:     id,
		Source:   stringValue(body["source"]),
		Mode:     stringValue(body["mode"]),
		Key:      refString(body["key"]),
		Query:    stringValue(body["query"]),
		Fallback: stringValue(mapValue(body["fallback"])["policy"]),
	}
}

func decodeAction(id string, body map[string]any) Action {
	return Action{
		Name:         id,
		Type:         stringValue(body["type"]),
		Provider:     stringValue(body["provider"]),
		Subject:      stringValue(body["subject"]),
		SuccessCodes: stringList(body["success_codes"]),
		RetryCodes:   stringList(body["retry_on_codes"]),
	}
}

func decodeThreatModel(id string, body map[string]any) ThreatModel {
	tm := ThreatModel{Name: id}
	for _, item := range anyList(body["category"]) {
		m, _ := item.(map[string]any)
		tm.Categories = append(tm.Categories, ThreatCategory{
			Name:     stringValue(m["id"]),
			Findings: stringList(mapValue(m["body"])["findings"]),
		})
	}
	return tm
}

func Summarize(cfg TCPGuardConfig) ConfigSummary {
	return ConfigSummary{
		PackName:        cfg.Pack.Name,
		GuardName:       cfg.Guard.Name,
		Mode:            cfg.Guard.Mode,
		AuthzTimeout:    cfg.Guard.Authz.Timeout,
		Datasources:     names(cfg.Datasources, func(v Datasource) string { return v.Name + ":" + v.Type }),
		Lookups:         names(cfg.Lookups, func(v Lookup) string { return v.Name + ":" + v.Mode }),
		Actions:         names(cfg.Actions, func(v Action) string { return v.Name + ":" + v.Type }),
		Detectors:       names(cfg.Detectors, func(v Detector) string { return v.Name + ":" + v.Type }),
		ThreatModels:    names(cfg.ThreatModels, func(v ThreatModel) string { return v.Name }),
		Triggers:        names(cfg.Triggers, func(v Trigger) string { return v.Name }),
		MaxActionBudget: cfg.Safety.MaxActionTimeout,
	}
}

func names[T any](xs []T, fn func(T) string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, fn(x))
	}
	return out
}

func exprCommands(v any) []string {
	out := make([]string, 0)
	for _, item := range anyList(v) {
		if m, ok := item.(map[string]any); ok {
			out = append(out, stringValue(m["$expr"]))
		}
	}
	return out
}

func mapValue(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func anyList(v any) []any {
	if xs, ok := v.([]any); ok {
		return xs
	}
	if v != nil {
		return []any{v}
	}
	return nil
}

func stringList(v any) []string {
	xs := anyList(v)
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, stringValue(x))
	}
	return out
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case map[string]any:
		if len(x) == 1 {
			for k, v := range x {
				if strings.HasPrefix(k, "$") {
					return stringValue(v)
				}
			}
		}
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func refString(v any) string {
	if m, ok := v.(map[string]any); ok {
		if ref, ok := m["$ref"]; ok {
			return stringValue(ref)
		}
	}
	return stringValue(v)
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func intValue(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

func exampleDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("cannot locate example directory")
	}
	return filepath.Dir(file)
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/server"
	"github.com/oarkflow/condition/pkg/storage"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "publish":
		return runPublish(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "evaluate":
		return runEvaluate(args[1:])
	case "test":
		return runTest(args[1:])
	case "gates":
		return runGates(args[1:])
	case "versions":
		return runVersions(args[1:])
	case "activate":
		return runActivate(args[1:])
	case "rollback":
		return runRollback(args[1:])
	case "simulate":
		return runSimulate(args[1:], false)
	case "compare":
		return runSimulate(args[1:], true)
	case "audits":
		return runAudits(args[1:])
	case "reports":
		return runReports(args[1:])
	case "audit":
		return runAudit(args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	addr := fs.String("addr", ":8080", "listen address")
	storeKind := fs.String("store", "file", "store kind: file, sqlite, memory")
	storePath := fs.String("store-path", ".condition", "file root or sqlite path")
	authzPath := fs.String("authz", "", "authz DSL config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Address, *addr, ":8080")
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	applyString(&cfg.AuthzPath, *authzPath, "")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	authzEngine, err := server.AuthzEngineFromFile(cfg.AuthzPath)
	if err != nil {
		return err
	}
	var opts []server.Option
	opts = append(opts, server.WithAuthzEngine(authzEngine))
	if cfg.RateLimit.Limit > 0 && cfg.RateLimit.Window != "" {
		if d, err := time.ParseDuration(cfg.RateLimit.Window); err == nil {
			opts = append(opts, server.WithRateLimit(cfg.RateLimit.Limit, d))
		}
	}
	fmt.Printf("condition listening on %s\n", cfg.Address)
	return http.ListenAndServe(cfg.Address, server.New(svc, opts...).Handler())
}

func runPublish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	name := fs.String("name", "", "definition name")
	version := fs.String("version", "1", "definition version")
	env := fs.String("env", "", "environment")
	runTests := fs.Bool("tests", false, "run tests before publishing")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("publish requires a decision.bcl path")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.Publish(context.Background(), condition.PublishRequest{Name: *name, Version: *version, Environment: *env, Path: fs.Arg(0), RunTests: *runTests})
	if err != nil {
		return err
	}
	printJSON(resp)
	return nil
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	name := fs.String("name", "", "definition name")
	version := fs.String("version", "1", "definition version")
	env := fs.String("env", "", "environment")
	runTests := fs.Bool("tests", false, "run tests")
	bundle := fs.String("bundle", "", "bundle id")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("validate requires a decision.bcl path")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.Validate(context.Background(), condition.ValidationRequest{Name: *name, Version: *version, Environment: *env, Path: fs.Arg(0), RunTests: *runTests, Bundle: *bundle})
	printJSON(resp)
	return err
}

func runEvaluate(args []string) error {
	fs := flag.NewFlagSet("evaluate", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	inputPath := fs.String("input", "", "input JSON path")
	decision := fs.String("decision", "", "decision id")
	compact := fs.Bool("compact", false, "print only decision answer")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("evaluate requires a definition name")
	}
	var input map[string]any
	if *inputPath != "" {
		if err := readJSON(*inputPath, &input); err != nil {
			return err
		}
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.Evaluate(context.Background(), fs.Arg(0), condition.EvaluateRequest{Decision: *decision, Input: input, IncludeFeatures: true, Counterfactuals: true})
	if err != nil {
		return err
	}
	if *compact && resp != nil && resp.Report != nil && resp.Report.Decision != nil {
		printJSON(resp.Report.Decision.Answer())
	} else {
		printJSON(resp)
	}
	return nil
}

func runTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	bundle := fs.String("bundle", "", "bundle id")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("test requires a definition name")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.Test(context.Background(), fs.Arg(0), *bundle)
	printJSON(resp)
	return err
}

func runGates(args []string) error {
	fs := flag.NewFlagSet("gates", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	bundle := fs.String("bundle", "", "bundle id")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("gates requires a definition name")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.Gates(context.Background(), fs.Arg(0), *bundle)
	printJSON(resp)
	return err
}

func runVersions(args []string) error {
	fs := flag.NewFlagSet("versions", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	env := fs.String("env", "", "environment")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("versions requires a definition name")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.ListVersions(context.Background(), fs.Arg(0), *env)
	printJSON(resp)
	return err
}

func runActivate(args []string) error {
	fs := flag.NewFlagSet("activate", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	env := fs.String("env", "", "environment")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("activate requires definition and version")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.Activate(context.Background(), fs.Arg(0), fs.Arg(1), *env)
	printJSON(resp)
	return err
}

func runRollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	env := fs.String("env", "", "environment")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("rollback requires definition and version")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.Rollback(context.Background(), fs.Arg(0), fs.Arg(1), *env)
	printJSON(resp)
	return err
}

func runSimulate(args []string, compare bool) error {
	fs := flag.NewFlagSet("simulate", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	candidate := fs.String("candidate", "", "candidate decision.bcl path")
	decision := fs.String("decision", "", "decision id")
	dataset := fs.String("dataset", "", "dataset id")
	casesPath := fs.String("cases", "", "cases JSON path")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("simulate requires a definition name")
	}
	var cases []bcl.DecisionBatchCase
	if *casesPath != "" {
		if err := readJSON(*casesPath, &cases); err != nil {
			return err
		}
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	req := condition.SimulationRequest{CandidatePath: *candidate, Decision: *decision, Dataset: *dataset, Cases: cases}
	var resp *condition.SimulationResponse
	if compare {
		resp, err = svc.Compare(context.Background(), fs.Arg(0), req)
	} else {
		resp, err = svc.Simulate(context.Background(), fs.Arg(0), req)
	}
	if resp != nil {
		printJSON(resp)
	}
	return err
}

func runAudits(args []string) error {
	fs := flag.NewFlagSet("audits", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	definition := fs.String("definition", "", "definition filter")
	operation := fs.String("operation", "", "operation filter")
	limit := fs.Int("limit", 0, "limit")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.QueryAudits(context.Background(), storage.ListOptions{Definition: *definition, Operation: *operation, Limit: *limit})
	printJSON(resp)
	return err
}

func runReports(args []string) error {
	fs := flag.NewFlagSet("reports", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	kind := fs.String("kind", "", "report kind")
	definition := fs.String("definition", "", "definition filter")
	limit := fs.Int("limit", 0, "limit")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := svc.QueryReports(context.Background(), storage.ListOptions{Kind: *kind, Definition: *definition, Limit: *limit})
	printJSON(resp)
	return err
}

func runAudit(args []string) error {
	if len(args) == 0 || args[0] != "verify" {
		return fmt.Errorf("usage: condition audit verify")
	}
	fs := flag.NewFlagSet("audit verify", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	applyString(&cfg.Store.Kind, *storeKind, "file")
	applyString(&cfg.Store.Path, *storePath, ".condition")
	svc, closer, err := newServiceFromConfig(cfg)
	if err != nil {
		return err
	}
	defer closer()
	if err := svc.VerifyAudits(context.Background()); err != nil {
		return err
	}
	fmt.Println("audit chain verified")
	return nil
}

func newService(kind, path string) (*condition.Service, func(), error) {
	cfg := RuntimeConfig{}
	cfg.Store.Kind = kind
	cfg.Store.Path = path
	return newServiceFromConfig(cfg)
}

func newServiceFromConfig(cfg RuntimeConfig) (*condition.Service, func(), error) {
	kind := cfg.Store.Kind
	path := cfg.Store.Path
	switch strings.ToLower(kind) {
	case "memory":
		return condition.NewService(storage.NewMemoryStore(), serviceConfig(cfg)), func() {}, nil
	case "sqlite":
		store, err := storage.NewSQLiteStore(path)
		if err != nil {
			return nil, nil, err
		}
		return condition.NewService(store, serviceConfig(cfg)), func() { _ = store.Close() }, nil
	case "file", "":
		store, err := storage.NewFileStore(path)
		if err != nil {
			return nil, nil, err
		}
		return condition.NewService(store, serviceConfig(cfg)), func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unknown store kind %q", kind)
	}
}

type RuntimeConfig struct {
	Address   string `json:"address" yaml:"address"`
	AuthzPath string `json:"authz_path" yaml:"authz_path"`
	Store     struct {
		Kind string `json:"kind" yaml:"kind"`
		Path string `json:"path" yaml:"path"`
	} `json:"store" yaml:"store"`
	Service struct {
		Environment     string `json:"environment" yaml:"environment"`
		RequestTimeout  string `json:"request_timeout" yaml:"request_timeout"`
		MaxRequestBytes int64  `json:"max_request_bytes" yaml:"max_request_bytes"`
	} `json:"service" yaml:"service"`
	RateLimit struct {
		Limit  int    `json:"limit" yaml:"limit"`
		Window string `json:"window" yaml:"window"`
	} `json:"rate_limit" yaml:"rate_limit"`
}

func loadConfig(path string) (RuntimeConfig, error) {
	cfg := RuntimeConfig{Address: ":8080"}
	cfg.Store.Kind = "file"
	cfg.Store.Path = ".condition"
	cfg.Service.Environment = "development"
	if path == "" {
		return cfg, nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if strings.HasSuffix(path, ".json") {
		return cfg, json.Unmarshal(payload, &cfg)
	}
	return cfg, yaml.Unmarshal(payload, &cfg)
}

func serviceConfig(cfg RuntimeConfig) condition.Config {
	out := condition.Config{Environment: cfg.Service.Environment, MaxRequestBytes: cfg.Service.MaxRequestBytes}
	if cfg.Service.RequestTimeout != "" {
		if d, err := time.ParseDuration(cfg.Service.RequestTimeout); err == nil {
			out.RequestTimeout = d
		}
	}
	return out
}

func applyString(dst *string, value, zero string) {
	if value != zero && value != "" {
		*dst = value
	}
}

func readJSON(path string, out any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

func printJSON(v any) {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(payload))
}

func usage() {
	fmt.Println(`condition commands:
  serve [--config condition.yaml]
  validate <decision.bcl>
  publish <decision.bcl>
  versions <definition>
  activate <definition> <version>
  rollback <definition> <version>
  evaluate <definition> --input input.json [--compact]
  test <definition>
  gates <definition> --bundle bundle
  simulate <definition> --candidate candidate.bcl --cases cases.json
  compare <definition> --candidate candidate.bcl --dataset dataset
  audits [--definition name] [--operation evaluate] [--limit 50]
  reports [--kind simulation]
  audit verify`)
}

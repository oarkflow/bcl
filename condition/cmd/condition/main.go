package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
	case "canary":
		return runCanary(args[1:])
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
	tlsCert := fs.String("tls-cert", "", "TLS certificate path")
	tlsKey := fs.String("tls-key", "", "TLS private key path")
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
	applyString(&cfg.TLS.CertFile, *tlsCert, "")
	applyString(&cfg.TLS.KeyFile, *tlsKey, "")
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
	opts = append(opts, server.WithTrustedProxies(cfg.TrustedProxies))
	fmt.Printf("condition listening on %s\n", cfg.Address)
	srv := &http.Server{
		Addr:              cfg.Address,
		Handler:           server.New(svc, opts...).Handler(),
		ReadHeaderTimeout: durationOrDefault(cfg.HTTP.ReadHeaderTimeout, 5*time.Second),
		ReadTimeout:       durationOrDefault(cfg.HTTP.ReadTimeout, 15*time.Second),
		WriteTimeout:      durationOrDefault(cfg.HTTP.WriteTimeout, 30*time.Second),
		IdleTimeout:       durationOrDefault(cfg.HTTP.IdleTimeout, 60*time.Second),
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}
	errCh := make(chan error, 1)
	go func() {
		if cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
			errCh <- srv.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			return
		}
		errCh <- srv.ListenAndServe()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), durationOrDefault(cfg.HTTP.ShutdownTimeout, 10*time.Second))
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

func runPublish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	name := fs.String("name", "", "definition name")
	version := fs.String("version", "1", "definition version")
	env := fs.String("env", "", "environment")
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.Publish(cliContext(cfg, *tenant), condition.PublishRequest{Name: *name, Version: *version, Environment: *env, Path: fs.Arg(0), RunTests: *runTests})
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
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.Validate(cliContext(cfg, *tenant), condition.ValidationRequest{Name: *name, Version: *version, Environment: *env, Path: fs.Arg(0), RunTests: *runTests, Bundle: *bundle})
	printJSON(resp)
	return err
}

func runEvaluate(args []string) error {
	fs := flag.NewFlagSet("evaluate", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	inputPath := fs.String("input", "", "input JSON path")
	decision := fs.String("decision", "", "decision id")
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.Evaluate(cliContext(cfg, *tenant), fs.Arg(0), condition.EvaluateRequest{Decision: *decision, Input: input, IncludeFeatures: true, Counterfactuals: true})
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
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.Test(cliContext(cfg, *tenant), fs.Arg(0), *bundle)
	printJSON(resp)
	return err
}

func runGates(args []string) error {
	fs := flag.NewFlagSet("gates", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	bundle := fs.String("bundle", "", "bundle id")
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.Gates(cliContext(cfg, *tenant), fs.Arg(0), *bundle)
	printJSON(resp)
	return err
}

func runVersions(args []string) error {
	fs := flag.NewFlagSet("versions", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	env := fs.String("env", "", "environment")
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.ListVersions(cliContext(cfg, *tenant), fs.Arg(0), *env)
	printJSON(resp)
	return err
}

func runActivate(args []string) error {
	fs := flag.NewFlagSet("activate", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	env := fs.String("env", "", "environment")
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.Activate(cliContext(cfg, *tenant), fs.Arg(0), fs.Arg(1), *env)
	printJSON(resp)
	return err
}

func runRollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	env := fs.String("env", "", "environment")
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.Rollback(cliContext(cfg, *tenant), fs.Arg(0), fs.Arg(1), *env)
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
	tenant := fs.String("tenant", "", "tenant id")
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
		resp, err = svc.Compare(cliContext(cfg, *tenant), fs.Arg(0), req)
	} else {
		resp, err = svc.Simulate(cliContext(cfg, *tenant), fs.Arg(0), req)
	}
	if resp != nil {
		printJSON(resp)
	}
	return err
}

func runCanary(args []string) error {
	fs := flag.NewFlagSet("canary", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	candidate := fs.String("candidate", "", "candidate decision.bcl path")
	decision := fs.String("decision", "", "decision id")
	dataset := fs.String("dataset", "", "dataset id")
	casesPath := fs.String("cases", "", "cases JSON path")
	maxChanged := fs.Int("max-changed", 0, "maximum changed cases")
	requireNoErrors := fs.Bool("require-no-errors", true, "fail canary on diagnostics")
	promote := fs.Bool("promote", false, "publish candidate when canary passes")
	promoteVersion := fs.String("promote-version", "", "candidate version to publish when promotion is enabled")
	tenant := fs.String("tenant", "", "tenant id")
	storeKind := fs.String("store", "file", "store kind")
	storePath := fs.String("store-path", ".condition", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("canary requires a definition name")
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
	resp, err := svc.Canary(cliContext(cfg, *tenant), fs.Arg(0), condition.CanaryRequest{
		SimulationRequest: condition.SimulationRequest{CandidatePath: *candidate, Decision: *decision, Dataset: *dataset, Cases: cases},
		MaxChangedCases:   *maxChanged,
		RequireNoErrors:   *requireNoErrors,
		Promote:           *promote,
		PromoteVersion:    *promoteVersion,
	})
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
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.QueryAudits(cliContext(cfg, *tenant), storage.ListOptions{Definition: *definition, Operation: *operation, Limit: *limit})
	printJSON(resp)
	return err
}

func runReports(args []string) error {
	fs := flag.NewFlagSet("reports", flag.ExitOnError)
	configPath := fs.String("config", "", "condition config path")
	kind := fs.String("kind", "", "report kind")
	definition := fs.String("definition", "", "definition filter")
	limit := fs.Int("limit", 0, "limit")
	tenant := fs.String("tenant", "", "tenant id")
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
	resp, err := svc.QueryReports(cliContext(cfg, *tenant), storage.ListOptions{Kind: *kind, Definition: *definition, Limit: *limit})
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
	tenant := fs.String("tenant", "", "tenant id")
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
	if err := svc.VerifyAudits(cliContext(cfg, *tenant)); err != nil {
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
	Address        string   `json:"address" yaml:"address" bcl:"address"`
	AuthzPath      string   `json:"authz_path" yaml:"authz_path" bcl:"authz_path"`
	TrustedProxies []string `json:"trusted_proxies" yaml:"trusted_proxies" bcl:"trusted_proxies"`
	Store          struct {
		Kind string `json:"kind" yaml:"kind" bcl:"kind"`
		Path string `json:"path" yaml:"path" bcl:"path"`
	} `json:"store" yaml:"store" bcl:"store"`
	TLS struct {
		CertFile string `json:"cert_file" yaml:"cert_file" bcl:"cert_file"`
		KeyFile  string `json:"key_file" yaml:"key_file" bcl:"key_file"`
	} `json:"tls" yaml:"tls" bcl:"tls"`
	HTTP struct {
		ReadHeaderTimeout string `json:"read_header_timeout" yaml:"read_header_timeout" bcl:"read_header_timeout"`
		ReadTimeout       string `json:"read_timeout" yaml:"read_timeout" bcl:"read_timeout"`
		WriteTimeout      string `json:"write_timeout" yaml:"write_timeout" bcl:"write_timeout"`
		IdleTimeout       string `json:"idle_timeout" yaml:"idle_timeout" bcl:"idle_timeout"`
		ShutdownTimeout   string `json:"shutdown_timeout" yaml:"shutdown_timeout" bcl:"shutdown_timeout"`
		MaxHeaderBytes    int    `json:"max_header_bytes" yaml:"max_header_bytes" bcl:"max_header_bytes"`
	} `json:"http" yaml:"http" bcl:"http"`
	Service struct {
		Environment               string `json:"environment" yaml:"environment" bcl:"environment"`
		DefaultTenant             string `json:"default_tenant" yaml:"default_tenant" bcl:"default_tenant"`
		RequestTimeout            string `json:"request_timeout" yaml:"request_timeout" bcl:"request_timeout"`
		MaxRequestBytes           int64  `json:"max_request_bytes" yaml:"max_request_bytes" bcl:"max_request_bytes"`
		StrictValidation          bool   `json:"strict_validation" yaml:"strict_validation" bcl:"strict_validation"`
		StrictEvaluation          bool   `json:"strict_evaluation" yaml:"strict_evaluation" bcl:"strict_evaluation"`
		RequireTests              bool   `json:"require_tests" yaml:"require_tests" bcl:"require_tests"`
		RequireActivationApproval bool   `json:"require_activation_approval" yaml:"require_activation_approval" bcl:"require_activation_approval"`
		Runtime                   struct {
			AllowTime              bool     `json:"allow_time" yaml:"allow_time" bcl:"allow_time"`
			FixedTime              string   `json:"fixed_time" yaml:"fixed_time" bcl:"fixed_time"`
			AllowEnv               bool     `json:"allow_env" yaml:"allow_env" bcl:"allow_env"`
			AllowedDatasetAdapters []string `json:"allowed_dataset_adapters" yaml:"allowed_dataset_adapters" bcl:"allowed_dataset_adapters"`
			AllowedHTTPHosts       []string `json:"allowed_http_hosts" yaml:"allowed_http_hosts" bcl:"allowed_http_hosts"`
			AllowedHTTPMethods     []string `json:"allowed_http_methods" yaml:"allowed_http_methods" bcl:"allowed_http_methods"`
			ExternalTimeout        string   `json:"external_timeout" yaml:"external_timeout" bcl:"external_timeout"`
		} `json:"runtime" yaml:"runtime" bcl:"runtime"`
	} `json:"service" yaml:"service" bcl:"service"`
	RateLimit struct {
		Limit  int    `json:"limit" yaml:"limit" bcl:"limit"`
		Window string `json:"window" yaml:"window" bcl:"window"`
	} `json:"rate_limit" yaml:"rate_limit" bcl:"rate_limit"`
}

func loadConfig(path string) (RuntimeConfig, error) {
	cfg := RuntimeConfig{Address: ":8080"}
	cfg.Store.Kind = "file"
	cfg.Store.Path = ".condition"
	cfg.Service.Environment = "development"
	cfg.Service.DefaultTenant = "default"
	cfg.HTTP.ReadHeaderTimeout = "5s"
	cfg.HTTP.ReadTimeout = "15s"
	cfg.HTTP.WriteTimeout = "30s"
	cfg.HTTP.IdleTimeout = "60s"
	cfg.HTTP.ShutdownTimeout = "10s"
	cfg.HTTP.MaxHeaderBytes = 1 << 20
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
	if strings.HasSuffix(path, ".bcl") {
		return cfg, bcl.UnmarshalWithOptions(payload, &cfg, &bcl.Options{AllowEnv: true, BaseDir: filepath.Dir(path)})
	}
	return cfg, yaml.Unmarshal(payload, &cfg)
}

func serviceConfig(cfg RuntimeConfig) condition.Config {
	out := condition.Config{
		Environment:               cfg.Service.Environment,
		DefaultTenant:             cfg.Service.DefaultTenant,
		MaxRequestBytes:           cfg.Service.MaxRequestBytes,
		StrictValidation:          cfg.Service.StrictValidation,
		StrictEvaluation:          cfg.Service.StrictEvaluation,
		RequireTests:              cfg.Service.RequireTests,
		RequireActivationApproval: cfg.Service.RequireActivationApproval,
	}
	if cfg.Service.RequestTimeout != "" {
		if d, err := time.ParseDuration(cfg.Service.RequestTimeout); err == nil {
			out.RequestTimeout = d
		}
	}
	out.Runtime = condition.RuntimePolicy{
		AllowTime:              cfg.Service.Runtime.AllowTime,
		FixedTime:              cfg.Service.Runtime.FixedTime,
		AllowEnv:               cfg.Service.Runtime.AllowEnv,
		AllowedDatasetAdapters: cfg.Service.Runtime.AllowedDatasetAdapters,
		AllowedHTTPHosts:       cfg.Service.Runtime.AllowedHTTPHosts,
		AllowedHTTPMethods:     cfg.Service.Runtime.AllowedHTTPMethods,
	}
	if cfg.Service.Runtime.ExternalTimeout != "" {
		if d, err := time.ParseDuration(cfg.Service.Runtime.ExternalTimeout); err == nil {
			out.Runtime.ExternalTimeout = d
		}
	}
	return out
}

func cliContext(cfg RuntimeConfig, tenant string) context.Context {
	return condition.ContextWithTenant(context.Background(), firstNonEmpty(tenant, cfg.Service.DefaultTenant, "default"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func applyString(dst *string, value, zero string) {
	if value != zero && value != "" {
		*dst = value
	}
}

func durationOrDefault(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
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
  serve [--config condition.yaml] [--tls-cert cert.pem --tls-key key.pem]
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
  canary <definition> --candidate candidate.bcl --dataset dataset [--max-changed 0] [--promote --promote-version 2]
  audits [--definition name] [--operation evaluate] [--limit 50]
  reports [--kind simulation]
  audit verify`)
}

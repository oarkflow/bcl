package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oarkflow/bcl"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fmt":
		err = runFmt(os.Args[2:])
	case "lint":
		err = runLint(os.Args[2:])
	case "validate":
		err = runValidate(os.Args[2:])
	case "compile":
		err = runCompile(os.Args[2:])
	case "domain":
		err = runDomain(os.Args[2:])
	case "explain":
		err = runExplain(os.Args[2:])
	case "simulate":
		err = runSimulate(os.Args[2:])
	case "test":
		err = runTest(os.Args[2:])
	case "export":
		err = runExport(os.Args[2:])
	case "codegen":
		err = runCodegen(os.Args[2:])
	case "docs":
		err = runDocs(os.Args[2:])
	case "migrate":
		err = runMigrate(os.Args[2:])
	case "modules":
		err = runModules(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runFmt(args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ExitOnError)
	write := fs.Bool("w", false, "write result to source file")
	fs.Parse(args)
	for _, path := range fs.Args() {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := bcl.Format(src)
		if err != nil {
			return err
		}
		if *write {
			if err := os.WriteFile(path, out, 0644); err != nil {
				return err
			}
		} else {
			os.Stdout.Write(out)
		}
	}
	return nil
}

func runLint(args []string) error {
	doc, err := oneDoc(args)
	if err != nil {
		return err
	}
	diags := bcl.Lint(doc, nil)
	printDiags(diags)
	return hasErrors(diags)
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	strict := fs.Bool("strict", false, "enable strict validation")
	fs.Parse(args)
	if fs.NArg() == 1 && isDir(fs.Arg(0)) {
		prog, err := bcl.CompileDomainDir(fs.Arg(0), &bcl.Options{Strict: *strict, AllowEnv: true})
		if prog != nil {
			printDiags(prog.Diagnostics)
		}
		if err != nil {
			if prog == nil {
				return err
			}
			return hasErrors(prog.Diagnostics)
		}
		return nil
	}
	doc, err := oneDoc(fs.Args())
	if err != nil {
		return err
	}
	opts := &bcl.Options{Strict: *strict, ResolveImports: true, ResolveModules: true, BaseDir: filepath.Dir(fs.Arg(0))}
	resolved, resolveDiags := bcl.ResolveDocument(doc, opts)
	diags := append(resolveDiags, bcl.Validate(resolved, opts)...)
	printDiags(diags)
	return hasErrors(diags)
}

func runCompile(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	outPath := fs.String("out", "", "output JSON path")
	profile := fs.String("profile", "", "active profile")
	allowEnv := fs.Bool("allow-env", false, "allow env functions")
	strict := fs.Bool("strict", false, "enable strict mode")
	lockfile := fs.String("lockfile", "", "lockfile path")
	envFiles := multiFlag{}
	fs.Var(&envFiles, "env-file", "load KEY=VALUE entries from env file")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("compile requires one file or directory")
	}
	var n any
	var err error
	if isDir(fs.Arg(0)) {
		n, err = bcl.CompileDomainDir(fs.Arg(0), &bcl.Options{Profile: *profile, AllowEnv: true, AllowTime: true, Strict: *strict, EnvFiles: envFiles})
	} else if *lockfile != "" {
		n, err = bcl.CompileFileWithLock(fs.Arg(0), *lockfile, &bcl.Options{Profile: *profile, AllowEnv: *allowEnv, Strict: *strict, EnvFiles: envFiles})
	} else {
		n, err = bcl.CompileFile(fs.Arg(0), &bcl.Options{Profile: *profile, AllowEnv: *allowEnv, ResolveImports: true, ResolveModules: true, Strict: *strict, EnvFiles: envFiles})
	}
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if *outPath != "" {
		return os.WriteFile(*outPath, out, 0644)
	}
	_, err = os.Stdout.Write(out)
	return err
}

func runDomain(args []string) error {
	fs := flag.NewFlagSet("domain", flag.ExitOnError)
	outPath := fs.String("out", "", "output JSON path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("domain requires one config directory")
	}
	prog, err := bcl.CompileDomainDir(fs.Arg(0), &bcl.Options{AllowEnv: true, AllowTime: true})
	if prog != nil {
		printDiags(prog.Diagnostics)
	}
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(prog, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if *outPath != "" {
		return os.WriteFile(*outPath, out, 0644)
	}
	_, err = os.Stdout.Write(out)
	return err
}

func runExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	inputPath := fs.String("input", "", "input JSON path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("explain requires one file")
	}
	result, err := bcl.ExplainFile(fs.Arg(0), &bcl.Options{AllowEnv: true})
	if err != nil {
		return err
	}
	if *inputPath != "" {
		input, err := bcl.ReadJSONFile(*inputPath)
		if err != nil {
			return err
		}
		sim := bcl.Simulate(result.Normalized, input, &bcl.Options{})
		result.Explain = append(result.Explain, sim.Trace...)
		result.Diagnostics = append(result.Diagnostics, sim.Diagnostics...)
	}
	printDiags(result.Diagnostics)
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	_, err = os.Stdout.Write(out)
	return err
}

func runSimulate(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ExitOnError)
	inputPath := fs.String("input", "", "input JSON path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("simulate requires one file")
	}
	input, err := bcl.ReadJSONFile(*inputPath)
	if err != nil {
		return err
	}
	result, err := bcl.SimulateFile(fs.Arg(0), input, &bcl.Options{ResolveImports: true, ResolveModules: true, AllowEnv: true})
	if err != nil {
		return err
	}
	printDiags(result.Diagnostics)
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	_, err = os.Stdout.Write(out)
	return err
}

func runTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "emit JSON test result")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("test requires one file")
	}
	result, err := bcl.TestFile(fs.Arg(0), &bcl.Options{AllowEnv: true, AllowTime: true})
	if *jsonOut {
		out, jerr := json.MarshalIndent(result, "", "  ")
		if jerr != nil {
			return jerr
		}
		out = append(out, '\n')
		_, _ = os.Stdout.Write(out)
	} else {
		for _, t := range result.Tests {
			status := "PASS"
			if !t.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(os.Stdout, "%s %s\n", status, t.Name)
			printDiags(t.Diagnostics)
		}
		if len(result.Tests) == 0 {
			fmt.Fprintln(os.Stdout, "no tests")
		}
	}
	if err != nil {
		return err
	}
	if !result.Passed {
		return fmt.Errorf("tests failed")
	}
	return nil
}

func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	format := fs.String("format", "json", "json or yaml")
	outPath := fs.String("out", "", "output path")
	fields := fs.String("fields", "", "comma-separated fields to include")
	redact := fs.String("redact", "", "comma-separated paths to redact")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("export requires one file")
	}
	n, err := bcl.CompileFile(fs.Arg(0), &bcl.Options{AllowEnv: true, ResolveImports: true, ResolveModules: true})
	if err != nil {
		return err
	}
	out, err := bcl.Export(n, bcl.ExportOptions{Format: *format, Fields: splitCSV(*fields), Redact: splitCSV(*redact)})
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if *outPath != "" {
		return os.WriteFile(*outPath, out, 0644)
	}
	_, err = os.Stdout.Write(out)
	return err
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func runCodegen(args []string) error {
	fs := flag.NewFlagSet("codegen", flag.ExitOnError)
	pkg := fs.String("package", "config", "Go package name")
	fs.Parse(args)
	doc, err := oneDoc(fs.Args())
	if err != nil {
		return err
	}
	out, err := bcl.GenerateGoTypes(doc, *pkg)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

func runDocs(args []string) error {
	doc, err := oneDoc(args)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(bcl.GenerateDocs(doc))
	return err
}

func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	version := fs.String("version", "1.0", "target BCL version")
	fs.Parse(args)
	doc, err := oneDoc(fs.Args())
	if err != nil {
		return err
	}
	doc, _ = bcl.MigrateDocument(doc, *version)
	var nodes []byte
	nodes, err = bcl.FormatDocument(doc)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(nodes)
	return err
}

func runModules(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected modules <lock|fetch|verify>")
	}
	switch args[0] {
	case "lock":
		if len(args) != 2 {
			return fmt.Errorf("modules lock requires one file")
		}
		doc, err := bcl.ParsePath(args[1])
		if err != nil {
			return err
		}
		lock, err := bcl.GenerateLockfile(doc, filepath.Dir(args[1]))
		if err != nil {
			return err
		}
		return bcl.WriteLockfile("bcl.lock", lock)
	case "fetch":
		fs := flag.NewFlagSet("modules fetch", flag.ExitOnError)
		lockfile := fs.String("lockfile", "bcl.lock", "lockfile path")
		cacheDir := fs.String("cache", "", "module cache directory")
		fs.Parse(args[1:])
		if fs.NArg() != 1 {
			return fmt.Errorf("modules fetch requires one file")
		}
		return bcl.FetchModules(fs.Arg(0), *lockfile, &bcl.ModuleFetchOptions{CacheDir: *cacheDir})
	case "verify":
		fs := flag.NewFlagSet("modules verify", flag.ExitOnError)
		cacheDir := fs.String("cache", "", "module cache directory")
		fs.Parse(args[1:])
		lockfile := "bcl.lock"
		if fs.NArg() > 0 {
			lockfile = fs.Arg(0)
		}
		diags := bcl.VerifyModules(lockfile, &bcl.ModuleVerifyOptions{CacheDir: *cacheDir})
		printDiags(diags)
		return hasErrors(diags)
	default:
		return fmt.Errorf("expected modules <lock|fetch|verify>")
	}
}

func oneDoc(args []string) (*bcl.Document, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("requires one file")
	}
	return bcl.ParsePath(args[0])
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func printDiags(diags []bcl.Diagnostic) {
	if len(diags) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, bcl.FormatDiagnostics(diags))
}

func hasErrors(diags []bcl.Diagnostic) error {
	for _, d := range diags {
		if d.Severity == "error" {
			return fmt.Errorf("validation failed")
		}
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: bcl <fmt|lint|validate|compile|domain|explain|simulate|test|export|codegen|docs|migrate|modules lock|modules fetch|modules verify> [args]")
}

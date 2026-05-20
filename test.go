package bcl

import (
	"fmt"
	"path/filepath"
)

type TestResult struct {
	Name        string            `json:"name"`
	Passed      bool              `json:"passed"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Simulation  *SimulationResult `json:"simulation,omitempty"`
	Expected    map[string]any    `json:"expected,omitempty"`
}

type TestSuiteResult struct {
	Passed      bool         `json:"passed"`
	Tests       []TestResult `json:"tests,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

func Test(doc *Document, input map[string]any, opts *Options) TestResult {
	n, err := Compile(doc, opts)
	if err != nil {
		return TestResult{Name: "<document>", Passed: false, Diagnostics: n.Diagnostics}
	}
	sim := Simulate(n, input, opts)
	return TestResult{Name: "<document>", Passed: len(sim.Diagnostics) == 0, Diagnostics: sim.Diagnostics, Simulation: sim}
}

func TestFile(path string, opts *Options) (*TestSuiteResult, error) {
	doc, err := ParsePath(path)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &Options{}
	}
	opts.BaseDir = filepath.Dir(path)
	opts.ResolveImports = true
	opts.ResolveModules = true
	suite := &TestSuiteResult{Passed: true}
	tests := collectTestBlocks(doc.Items)
	if len(tests) == 0 {
		return suite, nil
	}
	decisionProgram, _ := CompileDecisionDocument(doc, opts)
	for _, testBlock := range tests {
		input := testInputFromBlock(testBlock, opts)
		testOpts := cloneOptionsForTest(opts, input)
		if decisionName := decisionNameFromTestBlock(testBlock, opts); decisionName != "" && decisionProgram != nil {
			result := runDecisionTest(decisionProgram, testBlock, decisionName, testOpts)
			if !result.Passed {
				suite.Passed = false
			}
			suite.Tests = append(suite.Tests, result)
			continue
		}
		n, err := Compile(doc, testOpts)
		if err != nil {
			suite.Passed = false
			if n != nil {
				suite.Diagnostics = append(suite.Diagnostics, n.Diagnostics...)
			}
			continue
		}
		test := compiledTestByName(n.Tests, testBlock.ID)
		if test == nil {
			continue
		}
		result := runCompiledTest(n, test, testOpts)
		if !result.Passed {
			suite.Passed = false
		}
		suite.Tests = append(suite.Tests, result)
	}
	return suite, nil
}

func decisionNameFromTestBlock(test *Block, opts *Options) string {
	c := &compiler{opts: opts, out: &Normalized{Body: map[string]any{}}, consts: map[string]Value{}, sets: map[string][]Value{}, types: map[string]string{}, schemaDecls: map[string]*SchemaDecl{}}
	for _, n := range test.Body {
		if a, ok := n.(*Assignment); ok && a.Name == "decision" {
			return scalarString(c.value(a.Value))
		}
	}
	return ""
}

func collectTestBlocks(nodes []Node) []*Block {
	var out []*Block
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			if b, ok := n.(*Block); ok {
				if b.Type == "test" && b.ID != "" {
					out = append(out, b)
				}
				walk(b.Body)
			}
		}
	}
	walk(nodes)
	return out
}

func testInputFromBlock(test *Block, opts *Options) map[string]any {
	c := &compiler{opts: opts, out: &Normalized{Body: map[string]any{}}, consts: map[string]Value{}, sets: map[string][]Value{}, types: map[string]string{}, schemaDecls: map[string]*SchemaDecl{}}
	for _, n := range test.Body {
		if a, ok := n.(*Assignment); ok && a.Name == "input" {
			if input, ok := c.value(a.Value).(map[string]any); ok {
				return input
			}
		}
	}
	return map[string]any{}
}

func cloneOptionsForTest(opts *Options, input map[string]any) *Options {
	cp := *opts
	if context, ok := input["context"].(map[string]any); ok {
		cp.Context = context
	}
	if session, ok := input["session"].(map[string]any); ok {
		cp.Session = session
	}
	return &cp
}

func compiledTestByName(tests []map[string]any, name string) map[string]any {
	for _, test := range tests {
		if stringValue(test["id"]) == name {
			return test
		}
	}
	return nil
}

func runCompiledTest(n *Normalized, test map[string]any, opts *Options) TestResult {
	name := stringValue(test["id"])
	body, _ := test["body"].(map[string]any)
	input, _ := body["input"].(map[string]any)
	expect, _ := body["expect"].(map[string]any)
	sim := Simulate(n, input, opts)
	result := TestResult{Name: name, Passed: true, Simulation: sim, Expected: expect}
	if diagnosticsExpectation(expect) == "none" && len(sim.Diagnostics) > 0 {
		result.Passed = false
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("test %q expected no diagnostics", name)})
	}
	if wantEmit, ok := expect["emit"].(string); ok && wantEmit != "" && !simulationHasEmit(sim, wantEmit) {
		result.Passed = false
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("test %q expected emit %q", name, wantEmit)})
	}
	if wantEffect, ok := expect["effect"].(string); ok && wantEffect != "" {
		if sim.Decision == nil || sim.Decision["effect"] != wantEffect {
			result.Passed = false
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("test %q expected effect %q", name, wantEffect)})
		}
	}
	result.Diagnostics = append(result.Diagnostics, sim.Diagnostics...)
	return result
}

func runDecisionTest(program *DecisionProgram, testBlock *Block, decisionName string, opts *Options) TestResult {
	builder := newDecisionBuilder(opts)
	test := builder.testFromBlock(testBlock)
	if test.Input == nil {
		test.Input = map[string]any{}
	}
	result := TestResult{Name: test.Name, Passed: true, Expected: test.Expect}
	decision, err := EvaluateDecision(program, decisionName, test.Input, opts)
	if err != nil {
		result.Passed = false
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: testBlock.Span})
		return result
	}
	result.Simulation = &SimulationResult{
		Decision: map[string]any{
			"effect":    decision.Effect,
			"allowed":   decision.Allowed,
			"policy_id": decision.PolicyID,
			"rank":      decision.Rank,
			"score":     decision.Score,
		},
		Diagnostics: decision.Diagnostics,
	}
	if wantEffect := scalarString(test.Expect["effect"]); wantEffect != "" && decision.Effect != wantEffect {
		result.Passed = false
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("test %q expected effect %q", test.Name, wantEffect)})
	}
	if wantAllowed, ok := test.Expect["allowed"].(bool); ok && decision.Allowed != wantAllowed {
		result.Passed = false
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("test %q expected allowed %v", test.Name, wantAllowed)})
	}
	result.Diagnostics = append(result.Diagnostics, decision.Diagnostics...)
	if len(decision.Diagnostics) > 0 {
		result.Passed = false
	}
	return result
}

func diagnosticsExpectation(expect map[string]any) string {
	if expect == nil {
		return ""
	}
	if s, ok := expect["diagnostics"].(string); ok {
		return s
	}
	return ""
}

func simulationHasEmit(sim *SimulationResult, want string) bool {
	for _, block := range sim.Matched {
		if blockHasEmit(block, want) {
			return true
		}
	}
	return false
}

func blockHasEmit(block map[string]any, want string) bool {
	body, _ := block["body"].(map[string]any)
	return mapHasEmit(body, want)
}

func mapHasEmit(m map[string]any, want string) bool {
	for k, v := range m {
		if k == "emit" {
			switch x := v.(type) {
			case string:
				if x == want {
					return true
				}
			case []any:
				for _, item := range x {
					if item == want {
						return true
					}
				}
			}
		}
		switch x := v.(type) {
		case map[string]any:
			if mapHasEmit(x, want) {
				return true
			}
		case []any:
			for _, item := range x {
				if child, ok := item.(map[string]any); ok && mapHasEmit(child, want) {
					return true
				}
			}
		}
	}
	return false
}

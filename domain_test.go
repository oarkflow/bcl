package bcl

import "testing"

func TestCompileDomainDirBuildsIndexedPolicyPlan(t *testing.T) {
	prog, err := CompileDomainDir("example/domain-dsl", &Options{AllowEnv: true, AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(prog.Policies) != 2 {
		t.Fatalf("policies = %#v", prog.Policies)
	}
	if prog.PolicyPlan == nil || len(prog.PolicyPlan.Index["org1"]["document"]["read"]) == 0 {
		t.Fatalf("missing policy index: %#v", prog.PolicyPlan)
	}
	decision := prog.PolicyPlan.Evaluate(map[string]any{
		"tenant": "org1",
		"request": map[string]any{
			"action":   "read",
			"resource": "document:sensitive:payroll",
		},
		"subject": map[string]any{
			"id":    "u1",
			"roles": []any{"employee"},
			"attrs": map[string]any{
				"clearance": "low",
			},
		},
		"resource": map[string]any{
			"owner_id": "u2",
		},
	}, nil)
	if decision.Effect != "deny" || decision.PolicyID != "deny-sensitive" {
		t.Fatalf("decision = %#v", decision)
	}
	decision = prog.PolicyPlan.Evaluate(map[string]any{
		"tenant": "org1",
		"request": map[string]any{
			"action":   "write",
			"resource": "document:public:roadmap",
		},
		"subject": map[string]any{
			"id":    "u1",
			"roles": []any{"admin"},
			"attrs": map[string]any{
				"clearance": "low",
			},
		},
		"resource": map[string]any{
			"owner_id": "u2",
		},
	}, nil)
	if decision.Effect != "allow" || decision.PolicyID != "owner-or-admin" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestDetectDomain(t *testing.T) {
	tests := map[string]DomainKind{
		"policies.pcl":   DomainPolicy,
		"engine.rcl":     DomainRuntime,
		"connectors.ccl": DomainConnect,
		"actions.acl":    DomainAction,
		"policy.schema":  DomainSchema,
		"main.bcl":       DomainBCL,
	}
	for path, want := range tests {
		if got := DetectDomain(path); got != want {
			t.Fatalf("%s got %s want %s", path, got, want)
		}
	}
}

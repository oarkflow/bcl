package condition

import (
	"fmt"
	"sort"
	"strings"

	"github.com/oarkflow/bcl"
)

func policyPackageManifests(program *bcl.DecisionProgram) ([]PolicyPackageManifest, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_policy_packages"] != nil {
		collectBlocks(program.Governance["_condition_policy_packages"], "policy_package", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "policy_package", &blocks)
	}
	var out []PolicyPackageManifest
	var diags []bcl.Diagnostic
	seen := map[string]bool{}
	for _, block := range blocks {
		manifest := PolicyPackageManifest{ID: strings.TrimSpace(stringAny(block["id"])), Metadata: metadataFromChildBlocks(block["body"])}
		if manifest.ID == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "policy_package is missing id"})
			continue
		}
		if seen[manifest.ID] {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate policy_package %q", manifest.ID)})
			continue
		}
		seen[manifest.ID] = true
		body := bodyMap(block["body"])
		manifest.Owner = stringAny(body["owner"])
		manifest.Domain = stringAny(body["domain"])
		manifest.Version = stringAny(body["version"])
		manifest.Capabilities = stringsFromAny(body["capabilities"])
		manifest.Routes = stringsFromAny(body["routes"])
		manifest.Actions = stringsFromAny(body["actions"])
		manifest.State = boolAny(body["state"])
		manifest.External = boolAny(body["external"])
		if manifest.Owner == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("policy_package %q is missing owner", manifest.ID)})
		}
		if manifest.Domain == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("policy_package %q is missing domain", manifest.ID)})
		}
		out = append(out, manifest)
	}
	return out, diags
}

func policyOverlays(program *bcl.DecisionProgram) ([]PolicyOverlayDefinition, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_policy_overlays"] != nil {
		collectBlocks(program.Governance["_condition_policy_overlays"], "policy_overlay", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "policy_overlay", &blocks)
	}
	out := []PolicyOverlayDefinition(nil)
	var diags []bcl.Diagnostic
	seen := map[string]bool{}
	for _, block := range blocks {
		body := bodyMap(block["body"])
		overlay := PolicyOverlayDefinition{
			ID:          strings.TrimSpace(stringAny(block["id"])),
			Layer:       strings.ToLower(strings.TrimSpace(stringAny(body["layer"]))),
			TenantID:    stringAny(body["tenant"]),
			Environment: stringAny(body["environment"]),
			RouteID:     first(stringAny(body["route"]), stringAny(body["route_id"])),
			Endpoint:    stringAny(body["endpoint"]),
			Metadata:    metadataFromChildBlocks(block["body"]),
		}
		if overlay.ID == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "policy_overlay is missing id"})
			continue
		}
		if seen[overlay.ID] {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate policy_overlay %q", overlay.ID)})
			continue
		}
		seen[overlay.ID] = true
		if overlay.Layer == "" {
			overlay.Layer = "global"
		}
		if !validOverlayLayer(overlay.Layer) {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("policy_overlay %q has unsupported layer %q", overlay.ID, overlay.Layer)})
			continue
		}
		if len(overlay.Metadata) == 0 {
			diags = append(diags, bcl.Diagnostic{Severity: "warning", Message: fmt.Sprintf("policy_overlay %q has no metadata", overlay.ID)})
		}
		out = append(out, overlay)
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := overlayLayerRank(out[i].Layer), overlayLayerRank(out[j].Layer)
		if li == lj {
			return out[i].ID < out[j].ID
		}
		return li < lj
	})
	return out, diags
}

func validOverlayLayer(layer string) bool {
	switch layer {
	case "global", "environment", "tenant", "endpoint", "route":
		return true
	default:
		return false
	}
}

func overlayLayerRank(layer string) int {
	switch layer {
	case "global":
		return 0
	case "environment":
		return 1
	case "tenant":
		return 2
	case "endpoint":
		return 3
	case "route":
		return 4
	default:
		return 9
	}
}

func actionCatalogs(program *bcl.DecisionProgram) ([]ActionCatalogDefinition, map[string]ActionDefinition, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_action_catalogs"] != nil {
		collectBlocks(program.Governance["_condition_action_catalogs"], "action_catalog", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "action_catalog", &blocks)
	}
	catalogs := []ActionCatalogDefinition(nil)
	actions := map[string]ActionDefinition{}
	var diags []bcl.Diagnostic
	for _, block := range blocks {
		catalog := ActionCatalogDefinition{ID: strings.TrimSpace(stringAny(block["id"]))}
		if catalog.ID == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "action_catalog is missing id"})
			continue
		}
		seen := map[string]bool{}
		for _, actionBlock := range childBlocks(block["body"], "action") {
			action := actionFromCatalogBlock(actionBlock)
			if action.ID == "" {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("action_catalog %q has action without id", catalog.ID)})
				continue
			}
			if seen[action.ID] {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("action_catalog %q has duplicate action %q", catalog.ID, action.ID)})
				continue
			}
			seen[action.ID] = true
			catalog.Actions = append(catalog.Actions, action)
			actions[action.ID] = action
		}
		catalogs = append(catalogs, catalog)
	}
	return catalogs, actions, diags
}

func actionFromCatalogBlock(block map[string]any) ActionDefinition {
	action := ActionDefinition{ID: strings.TrimSpace(stringAny(block["id"])), Metadata: metadataFromChildBlocks(block["body"])}
	body := bodyMap(block["body"])
	action.Sinks = stringsFromAny(body["sinks"])
	if len(action.Sinks) == 0 {
		if sink := stringAny(body["sink"]); sink != "" {
			action.Sinks = []string{sink}
		}
	}
	action.Severity = stringAny(body["severity"])
	action.Retries = intAny(body["retries"])
	action.Approval = stringAny(body["approval"])
	return action
}

func outputContracts(program *bcl.DecisionProgram) (map[string]OutputContractDefinition, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_output_contracts"] != nil {
		collectBlocks(program.Governance["_condition_output_contracts"], "output_contract", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "output_contract", &blocks)
	}
	out := map[string]OutputContractDefinition{}
	var diags []bcl.Diagnostic
	for _, block := range blocks {
		contract := OutputContractDefinition{ID: strings.TrimSpace(stringAny(block["id"]))}
		if contract.ID == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "output_contract is missing id"})
			continue
		}
		if _, ok := out[contract.ID]; ok {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate output_contract %q", contract.ID)})
			continue
		}
		body := bodyMap(block["body"])
		contract.Actions = stringsFromAny(body["actions"])
		contract.Severities = stringsFromAny(body["severities"])
		for _, item := range blockList(block["body"]) {
			switch stringAny(item["name"]) {
			case "action":
				contract.Actions = append(contract.Actions, blockValueString(item["value"]))
			case "severity":
				contract.Severities = append(contract.Severities, blockValueString(item["value"]))
			}
		}
		out[contract.ID] = contract
	}
	return out, diags
}

func standardFactContracts(program *bcl.DecisionProgram) ([]StandardFactContract, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_standard_facts"] != nil {
		collectBlocks(program.Governance["_condition_standard_facts"], "standard_facts", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "standard_facts", &blocks)
	}
	var out []StandardFactContract
	var diags []bcl.Diagnostic
	allowedFacts := stringSet([]string{"request", "response", "principal", "route", "tenant", "resource", "risk", "state"})
	for _, block := range blocks {
		contract := StandardFactContract{ID: strings.TrimSpace(stringAny(block["id"])), Facts: map[string]string{}, Metadata: metadataFromChildBlocks(block["body"])}
		if contract.ID == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "standard_facts is missing id"})
			continue
		}
		for _, item := range blockList(block["body"]) {
			if stringAny(item["name"]) == "fact" {
				fact := blockValueString(item["value"])
				if fact != "" {
					contract.Facts[fact] = ""
				}
			}
		}
		for _, factBlock := range childBlocks(block["body"], "fact") {
			fact := strings.TrimSpace(stringAny(factBlock["id"]))
			if fact == "" {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("standard_facts %q has fact without id", contract.ID)})
				continue
			}
			if !allowedFacts[fact] {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("standard_facts %q declares unsupported fact %q", contract.ID, fact)})
			}
			body := bodyMap(factBlock["body"])
			contract.Facts[fact] = stringAny(body["schema"])
		}
		out = append(out, contract)
	}
	return out, diags
}

func validatePolicyContracts(program *bcl.DecisionProgram) []bcl.Diagnostic {
	var diags []bcl.Diagnostic
	_, packageDiags := policyPackageManifests(program)
	diags = append(diags, packageDiags...)
	_, actions, actionDiags := actionCatalogs(program)
	diags = append(diags, actionDiags...)
	contracts, contractDiags := outputContracts(program)
	diags = append(diags, contractDiags...)
	_, factDiags := standardFactContracts(program)
	diags = append(diags, factDiags...)
	_, overlayDiags := policyOverlays(program)
	diags = append(diags, overlayDiags...)
	if len(actions) > 0 {
		for _, name := range declaredActionNames(program) {
			if _, ok := actions[name]; !ok {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("unknown action %q is not declared in any action_catalog", name)})
			}
		}
	}
	if len(contracts) > 0 {
		diags = append(diags, validateOutputContractRefs(program, contracts)...)
	}
	return diags
}

func actionDefinition(program *bcl.DecisionProgram, name string) (ActionDefinition, bool) {
	_, actions, _ := actionCatalogs(program)
	def, ok := actions[name]
	return def, ok
}

func validateOutputContractRefs(program *bcl.DecisionProgram, contracts map[string]OutputContractDefinition) []bcl.Diagnostic {
	var diags []bcl.Diagnostic
	for _, blockType := range []string{"decision_table", "decision", "chain", "lifecycle"} {
		var blocks []map[string]any
		if program != nil && program.Normalized != nil {
			collectBlocks(program.Normalized.Body, blockType, &blocks)
		}
		for _, block := range blocks {
			body := bodyMap(block["body"])
			contractID := stringAny(body["contract"])
			if contractID == "" {
				continue
			}
			contract, ok := contracts[contractID]
			if !ok {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("%s %q references unknown output_contract %q", blockType, stringAny(block["id"]), contractID)})
				continue
			}
			if len(contract.Actions) > 0 {
				allowed := stringSet(contract.Actions)
				for _, action := range actionNamesInBlock(block) {
					if !allowed[action] {
						diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("%s %q emits action %q outside output_contract %q", blockType, stringAny(block["id"]), action, contractID)})
					}
				}
			}
		}
	}
	return diags
}

func declaredActionNames(program *bcl.DecisionProgram) []string {
	set := map[string]bool{}
	chains, _ := chainDefinitions(program)
	for _, chain := range chains {
		for _, watch := range chain.Watches {
			for _, step := range watch.Steps {
				if step.Action != "" {
					set[step.Action] = true
				}
			}
		}
	}
	if program != nil {
		for _, decision := range program.Decisions {
			for _, rule := range decision.Rules {
				for _, action := range actionNamesFromAny(rule.Then) {
					set[action] = true
				}
			}
		}
	}
	return sortedKeys(set)
}

func actionNamesInBlock(block map[string]any) []string {
	set := map[string]bool{}
	for _, action := range actionNamesFromAny(block) {
		set[action] = true
	}
	return sortedKeys(set)
}

func actionNamesFromAny(v any) []string {
	set := map[string]bool{}
	var walk func(any, string)
	walk = func(v any, key string) {
		switch x := literalValue(v).(type) {
		case map[string]any:
			for k, value := range x {
				walk(value, k)
			}
		case []any:
			for _, value := range x {
				walk(value, key)
			}
		case string:
			if key == "action" && strings.TrimSpace(x) != "" {
				set[x] = true
			}
		}
	}
	walk(v, "")
	return sortedKeys(set)
}

func stringsFromAny(v any) []string {
	switch x := literalValue(v).(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := strings.TrimSpace(stringAny(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{strings.TrimSpace(x)}
	default:
		return nil
	}
}

func boolAny(v any) bool {
	switch x := literalValue(v).(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	default:
		return false
	}
}

func truthyAny(v any) bool {
	if boolAny(v) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(literalValue(v))), "true")
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

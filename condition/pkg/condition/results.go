package condition

import (
	"fmt"
	"strings"

	"github.com/oarkflow/bcl"
)

func resultReferenceDiagnostics(program *bcl.DecisionProgram) []bcl.Diagnostic {
	index, diags := resultIndex(program)
	for _, decision := range program.Decisions {
		for _, rule := range decision.Rules {
			if ref := stringAny(decisionAttributes(rule.Then)["result"]); ref != "" {
				if _, ok := index[ref]; !ok {
					diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("unknown result %q", ref), Span: rule.Span})
				}
			}
		}
	}
	return diags
}

func hydrateDecisionResult(program *bcl.DecisionProgram, decision *bcl.DecisionResult) {
	if decision == nil || decision.Attributes == nil {
		return
	}
	ref := stringAny(decision.Attributes["result"])
	if ref == "" {
		normalizeLegacyBodyAttributes(decision.Attributes)
		return
	}
	index, _ := resultIndex(program)
	base, ok := index[ref]
	if !ok {
		normalizeLegacyBodyAttributes(decision.Attributes)
		return
	}
	overrides := cloneMap(decision.Attributes)
	delete(overrides, "result")
	decision.Attributes = deepMergeMaps(cloneMap(base), overrides)
	normalizeLegacyBodyAttributes(decision.Attributes)
	if decision.Metadata == nil {
		decision.Metadata = map[string]any{}
	}
	if decision.Metadata["severity"] == nil && decision.Attributes["severity"] != nil {
		decision.Metadata["severity"] = decision.Attributes["severity"]
	}
}

func resultIndex(program *bcl.DecisionProgram) (map[string]map[string]any, []bcl.Diagnostic) {
	out := map[string]map[string]any{}
	var diags []bcl.Diagnostic
	chains, chainDiags := chainDefinitions(program)
	diags = append(diags, chainDiags...)
	for _, chain := range chains {
		for _, watch := range chain.Watches {
			for _, step := range watch.Steps {
				full := chain.ID + "." + watch.ID + "." + step.ID
				attrs := resultAttributes(chain.ID, watch.ID, step)
				out[full] = attrs
				if step.ResultID != "" {
					if _, exists := out[step.ResultID]; exists {
						diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate result id %q", step.ResultID)})
						continue
					}
					out[step.ResultID] = attrs
				}
			}
		}
	}
	return out, diags
}

func resultAttributes(chainID, watchID string, step ChainStep) map[string]any {
	attrs := cloneMap(step.Attributes)
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrs["action"] = step.Action
	if step.Severity != "" {
		attrs["severity"] = step.Severity
	}
	attrs["chain"] = chainID
	attrs["watch"] = watchID
	attrs["step"] = step.ID
	if step.ResultID != "" {
		attrs["result_id"] = step.ResultID
	}
	normalizeLegacyBodyAttributes(attrs)
	return attrs
}

func decisionAttributes(then map[string]any) map[string]any {
	outcome := mapFromAny(then["outcome"])
	if body := mapFromAny(outcome["body"]); len(body) > 0 {
		outcome = body
	}
	for _, source := range []map[string]any{mapFromAny(outcome["attributes"]), mapFromAny(then["attributes"])} {
		if len(source) > 0 {
			return source
		}
	}
	return nil
}

func deepMergeMaps(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range src {
		if existing, ok := dst[key].(map[string]any); ok {
			if incoming, ok := value.(map[string]any); ok {
				dst[key] = deepMergeMaps(existing, incoming)
				continue
			}
		}
		dst[key] = literalValue(value)
	}
	return dst
}

func normalizeLegacyBodyAttributes(attrs map[string]any) {
	if attrs == nil {
		return
	}
	body := mapFromAny(attrs["body"])
	if body == nil {
		body = map[string]any{}
	}
	if body["error"] == nil {
		if code := strings.TrimSpace(stringAny(attrs["body_code"])); code != "" {
			body["error"] = code
		}
	}
	if body["message"] == nil {
		if message := strings.TrimSpace(stringAny(attrs["body_message"])); message != "" {
			body["message"] = message
		}
	}
	if len(body) > 0 {
		attrs["body"] = body
	}
}

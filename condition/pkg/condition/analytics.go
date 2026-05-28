package condition

import (
	"strings"

	"github.com/oarkflow/bcl"
)

func responseClassifiers(program *bcl.DecisionProgram) map[string]ResponseClassifierDefinition {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_response_classifiers"] != nil {
		collectBlocks(program.Governance["_condition_response_classifiers"], "response_classifier", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "response_classifier", &blocks)
	}
	out := map[string]ResponseClassifierDefinition{}
	for _, block := range blocks {
		def := ResponseClassifierDefinition{
			ID:                     strings.TrimSpace(stringAny(block["id"])),
			HealthyBelow:           400,
			UnhealthyAtOrAbove:     500,
			ExpectedClientStatuses: []int{400, 401, 403},
		}
		if def.ID == "" {
			continue
		}
		body := bodyMap(block["body"])
		def.HealthyStatuses = intsFromAny(body["healthy_statuses"])
		def.UnhealthyStatuses = intsFromAny(body["unhealthy_statuses"])
		if statuses := intsFromAny(body["expected_client_statuses"]); len(statuses) > 0 {
			def.ExpectedClientStatuses = statuses
		}
		if n := intAny(body["healthy_below"]); n > 0 {
			def.HealthyBelow = n
		}
		if n := intAny(body["unhealthy_at_or_above"]); n > 0 {
			def.UnhealthyAtOrAbove = n
		}
		out[def.ID] = def
	}
	return out
}

func defaultResponseClassifier(program *bcl.DecisionProgram) ResponseClassifierDefinition {
	classifiers := responseClassifiers(program)
	if def, ok := classifiers["http"]; ok {
		return def
	}
	for _, def := range classifiers {
		return def
	}
	return ResponseClassifierDefinition{ID: "default", HealthyBelow: 400, UnhealthyAtOrAbove: 500, ExpectedClientStatuses: []int{400, 401, 403}}
}

func lifecycleResponseFacts(program *bcl.DecisionProgram, response map[string]any) map[string]any {
	facts := cloneMap(response)
	normalizeLifecycleEnvelope(facts)
	status := intAny(facts["status"])
	if status <= 0 {
		return facts
	}
	classifier := defaultResponseClassifier(program)
	expectedClient := intIn(status, classifier.ExpectedClientStatuses)
	healthy := intIn(status, classifier.HealthyStatuses) || status < classifier.HealthyBelow || expectedClient
	unhealthy := intIn(status, classifier.UnhealthyStatuses) || status >= classifier.UnhealthyAtOrAbove || (status >= 400 && status < 500 && !expectedClient)
	class := "healthy"
	switch {
	case status >= 500:
		class = "server_error"
	case status >= 400 && expectedClient:
		class = "expected_client_error"
	case status >= 400:
		class = "unexpected_client_error"
	}
	facts["class"] = class
	facts["healthy"] = healthy && !unhealthy
	facts["unhealthy"] = unhealthy
	facts["expected_client_error"] = expectedClient
	return facts
}

func lifecycleRequestFacts(request map[string]any, method, path string) map[string]any {
	facts := cloneMap(request)
	if method != "" {
		facts["method"] = method
	}
	if path != "" {
		facts["path"] = path
	}
	normalizeLifecycleEnvelope(facts)
	return facts
}

func normalizeLifecycleEnvelope(facts map[string]any) {
	if facts == nil {
		return
	}
	headers := mapFromAny(facts["headers"])
	contentType := first(stringAny(facts["content_type"]), stringAny(headers["content_type"]), stringAny(headers["content-type"]))
	if contentType != "" {
		facts["content_type"] = contentType
	}
	format := strings.ToLower(strings.TrimSpace(first(stringAny(facts["format"]), stringAny(facts["body_format"]), formatFromContentType(contentType))))
	if format == "" {
		format = "unknown"
	}
	facts["format"] = format
	facts["body_format"] = format
	body := facts["body"]
	switch format {
	case "json":
		facts["body_json"] = body
	case "form", "urlencoded", "multipart":
		facts["body_form"] = body
	case "text", "xml", "html":
		facts["body_text"] = body
	default:
		facts["body_raw"] = body
	}
}

func formatFromContentType(contentType string) string {
	contentType = strings.ToLower(contentType)
	switch {
	case strings.Contains(contentType, "json"):
		return "json"
	case strings.Contains(contentType, "x-www-form-urlencoded"):
		return "form"
	case strings.Contains(contentType, "multipart/form-data"):
		return "multipart"
	case strings.Contains(contentType, "xml"):
		return "xml"
	case strings.Contains(contentType, "html"):
		return "html"
	case strings.HasPrefix(contentType, "text/"):
		return "text"
	default:
		return ""
	}
}

func intsFromAny(v any) []int {
	switch x := literalValue(v).(type) {
	case []any:
		out := make([]int, 0, len(x))
		for _, item := range x {
			if n := intAny(item); n > 0 {
				out = append(out, n)
			}
		}
		return out
	default:
		if n := intAny(v); n > 0 {
			return []int{n}
		}
		return nil
	}
}

func intIn(want int, values []int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

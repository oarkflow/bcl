package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/oarkflow/bcl"
)

func threatModels(program *bcl.DecisionProgram) []map[string]any {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_threat_models"] != nil {
		collectBlocks(program.Governance["_condition_threat_models"], "threat_model", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "threat_model", &blocks)
	}
	out := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		model := map[string]any{"id": strings.TrimSpace(stringAny(block["id"]))}
		body := contentMapAny(block["body"])
		for _, key := range []string{"assets", "actors", "abuse_paths", "detection_signals", "controls", "assumptions"} {
			if values := stringsFromAny(body[key]); len(values) > 0 {
				model[key] = values
			}
		}
		if metadata := metadataFromChildBlocks(block["body"]); len(metadata) > 0 {
			model["metadata"] = metadata
		}
		if len(model) > 1 {
			out = append(out, model)
		}
	}
	return out
}

func (s *Service) ThreatModel(ctx context.Context, definition, id string) (map[string]any, error) {
	record, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	models := threatModels(record.Program)
	for _, model := range models {
		if id == "" || strings.EqualFold(stringAny(model["id"]), id) {
			return model, nil
		}
	}
	if id == "" {
		return nil, fmt.Errorf("definition %q has no threat_model", definition)
	}
	return nil, fmt.Errorf("definition %q has no threat_model %q", definition, id)
}

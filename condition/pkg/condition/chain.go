package condition

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/storage"
)

func chainDefinition(program *bcl.DecisionProgram, chainID string) (*ChainDefinition, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	var chains []map[string]any
	if raw := program.Governance["_condition_chains"]; raw != nil {
		collectBlocks(raw, "chain", &chains)
	}
	if len(chains) == 0 && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "chain", &chains)
	}
	if len(chains) == 0 {
		return nil, fmt.Errorf("definition has no chains")
	}
	for _, block := range chains {
		id := stringAny(block["id"])
		if chainID != "" && id != chainID {
			continue
		}
		def, err := chainFromBlock(block)
		if err != nil {
			return nil, err
		}
		return def, nil
	}
	return nil, fmt.Errorf("unknown chain %q", chainID)
}

func chainDefinitions(program *bcl.DecisionProgram) ([]ChainDefinition, []bcl.Diagnostic) {
	var chains []map[string]any
	if program != nil && program.Governance["_condition_chains"] != nil {
		collectBlocks(program.Governance["_condition_chains"], "chain", &chains)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "chain", &chains)
	}
	var out []ChainDefinition
	var diags []bcl.Diagnostic
	seen := map[string]bool{}
	for _, block := range chains {
		def, err := chainFromBlock(block)
		if err != nil {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: err.Error()})
			continue
		}
		if seen[def.ID] {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate chain %q", def.ID)})
			continue
		}
		seen[def.ID] = true
		out = append(out, *def)
	}
	return out, diags
}

func chainFromBlock(block map[string]any) (*ChainDefinition, error) {
	def := &ChainDefinition{ID: strings.TrimSpace(stringAny(block["id"]))}
	if def.ID == "" {
		return nil, fmt.Errorf("chain is missing id")
	}
	for _, item := range blockList(block["body"]) {
		switch stringAny(item["type"]) {
		case "watch":
			watch, err := chainWatchFromBlock(item)
			if err != nil {
				return nil, fmt.Errorf("chain %q: %w", def.ID, err)
			}
			def.Watches = append(def.Watches, *watch)
			continue
		}
		if stringAny(item["name"]) == "entity" || stringAny(item["name"]) == "entity_key" {
			def.EntityKeyPath = blockValueString(item["value"])
		}
		if stringAny(item["name"]) == "decision" {
			if decision := blockValueString(item["value"]); decision != "" {
				def.Decisions = append(def.Decisions, decision)
			}
		}
	}
	if def.EntityKeyPath == "" {
		return nil, fmt.Errorf("chain %q is missing entity_key", def.ID)
	}
	return def, nil
}

func chainWatchFromBlock(block map[string]any) (*ChainWatchDefinition, error) {
	watch := &ChainWatchDefinition{ID: strings.TrimSpace(stringAny(block["id"])), Metadata: metadataFromChildBlocks(block["body"])}
	if watch.ID == "" {
		return nil, fmt.Errorf("watch is missing id")
	}
	seenSteps := map[string]bool{}
	for _, item := range blockList(block["body"]) {
		if stringAny(item["type"]) == "step" {
			step, err := chainStepFromBlock(item)
			if err != nil {
				return nil, fmt.Errorf("watch %q: %w", watch.ID, err)
			}
			if seenSteps[step.ID] {
				return nil, fmt.Errorf("watch %q has duplicate step %q", watch.ID, step.ID)
			}
			seenSteps[step.ID] = true
			watch.Steps = append(watch.Steps, *step)
			continue
		}
		switch stringAny(item["name"]) {
		case "event":
			watch.Event = blockValueString(item["value"])
			if watch.Event != "" {
				watch.Events = append(watch.Events, watch.Event)
			}
		case "events":
			watch.Events = append(watch.Events, stringsFromAny(item["value"])...)
		case "window":
			watch.Window = blockValueString(item["value"])
			if watch.Window != "" {
				if _, err := chainDuration(watch.Window); err != nil {
					return nil, fmt.Errorf("watch %q has invalid window %q", watch.ID, watch.Window)
				}
			}
		case "distinct":
			watch.Distinct = blockValueString(item["value"])
		case "field":
			watch.Field = blockValueString(item["value"])
		case "metric":
			if metric := blockValueString(item["value"]); metric != "" {
				watch.Metrics = append(watch.Metrics, metric)
			}
		case "metrics":
			watch.Metrics = append(watch.Metrics, stringsFromAny(item["value"])...)
		case "suppress":
			watch.Suppress = truthyAny(item["value"])
		case "decay":
			watch.Decay = blockValueString(item["value"])
		case "cooldown":
			watch.Cooldown = blockValueString(item["value"])
		case "reset", "reset_event":
			watch.Reset = blockValueString(item["value"])
		}
	}
	body := bodyMap(block["body"])
	if watch.Distinct == "" {
		watch.Distinct = stringAny(body["distinct"])
	}
	if watch.Field == "" {
		watch.Field = stringAny(body["field"])
	}
	if len(watch.Metrics) == 0 {
		watch.Metrics = stringsFromAny(body["metrics"])
	}
	if len(watch.Events) == 0 {
		watch.Events = stringsFromAny(body["events"])
	}
	if !watch.Suppress {
		watch.Suppress = truthyAny(body["suppress"])
	}
	if watch.Decay == "" {
		watch.Decay = stringAny(body["decay"])
	}
	if watch.Decay != "" {
		if _, err := chainDuration(watch.Decay); err != nil {
			return nil, fmt.Errorf("watch %q has invalid decay %q", watch.ID, watch.Decay)
		}
	}
	if watch.Cooldown == "" {
		watch.Cooldown = stringAny(body["cooldown"])
	}
	if watch.Cooldown != "" {
		if _, err := chainDuration(watch.Cooldown); err != nil {
			return nil, fmt.Errorf("watch %q has invalid cooldown %q", watch.ID, watch.Cooldown)
		}
	}
	if watch.Reset == "" {
		watch.Reset = first(stringAny(body["reset"]), stringAny(body["reset_event"]))
	}
	if !watch.Suppress && watch.Metadata != nil {
		watch.Suppress = truthyAny(watch.Metadata["suppress"])
	}
	if watch.Event == "" {
		if len(watch.Events) > 0 {
			watch.Event = watch.Events[0]
		} else {
			return nil, fmt.Errorf("watch %q is missing event", watch.ID)
		}
	}
	if len(watch.Steps) == 0 {
		return nil, fmt.Errorf("watch %q is missing steps", watch.ID)
	}
	sort.Slice(watch.Steps, func(i, j int) bool { return watch.Steps[i].Threshold < watch.Steps[j].Threshold })
	return watch, nil
}

func chainStepFromBlock(block map[string]any) (*ChainStep, error) {
	step := &ChainStep{ID: strings.TrimSpace(stringAny(block["id"])), Attributes: map[string]any{}, Metadata: metadataFromChildBlocks(block["body"])}
	if step.ID == "" {
		return nil, fmt.Errorf("step is missing id")
	}
	body := bodyMap(block["body"])
	step.Threshold = intAny(body["threshold"])
	step.Action = stringAny(body["action"])
	step.Severity = stringAny(body["severity"])
	step.TTL = stringAny(body["ttl"])
	if step.Threshold <= 0 {
		return nil, fmt.Errorf("step %q has invalid threshold", step.ID)
	}
	if step.Action == "" {
		return nil, fmt.Errorf("step %q is missing action", step.ID)
	}
	if step.TTL != "" {
		if _, err := chainDuration(step.TTL); err != nil {
			return nil, fmt.Errorf("step %q has invalid ttl %q", step.ID, step.TTL)
		}
	}
	for key, value := range body {
		switch key {
		case "threshold", "action", "severity", "ttl", "metadata":
		default:
			step.Attributes[key] = literalValue(value)
		}
	}
	if len(step.Attributes) == 0 {
		step.Attributes = nil
	}
	if len(step.Metadata) == 0 {
		step.Metadata = nil
	}
	return step, nil
}

func metadataFromChildBlocks(v any) map[string]any {
	out := map[string]any{}
	for _, item := range blockList(v) {
		if stringAny(item["name"]) == "metadata" {
			mergeInto(out, metadataMap(item["value"]))
		}
	}
	for _, block := range childBlocks(v, "metadata") {
		mergeInto(out, metadataMap(block["body"]))
		for _, item := range blockList(block["body"]) {
			if name := stringAny(item["name"]); name != "" {
				out[name] = literalValue(item["value"])
				continue
			}
			if typ := stringAny(item["type"]); typ != "" {
				if id := stringAny(item["id"]); id != "" {
					out[typ] = id
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeInto(dst, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

func metadataMap(v any) map[string]any {
	out := map[string]any{}
	if m, ok := literalValue(v).(map[string]any); ok {
		if fields, ok := m["fields"].([]any); ok {
			for _, field := range fields {
				if item, ok := field.(map[string]any); ok {
					if name := stringAny(item["name"]); name != "" {
						out[name] = literalValue(item["value"])
					}
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		for key, value := range m {
			if key == "fields" || key == "span" {
				continue
			}
			out[key] = literalValue(value)
		}
	}
	for key, value := range bodyMap(v) {
		out[key] = literalValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func blockValueString(v any) string {
	if m, ok := v.(map[string]any); ok {
		for _, key := range []string{"data", "path", "raw"} {
			if value := stringAny(m[key]); value != "" {
				return value
			}
		}
	}
	return stringAny(literalValue(v))
}

func intAny(v any) int {
	switch x := literalValue(v).(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case jsonNumber:
		n, _ := strconv.Atoi(string(x))
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(x)))
		return n
	}
}

type jsonNumber string

func validateChains(program *bcl.DecisionProgram) []bcl.Diagnostic {
	chains, diags := chainDefinitions(program)
	for _, chain := range chains {
		for _, decision := range chain.Decisions {
			if program.Decisions[decision] == nil {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("chain %q references unknown decision %q", chain.ID, decision)})
			}
		}
	}
	return diags
}

func chainStateFacts(states []storage.ChainStateRecord) map[string]any {
	out := map[string]any{}
	for _, state := range states {
		out[state.Watch] = map[string]any{
			"counters":   state.Counters,
			"step":       state.Step,
			"action":     state.Action,
			"severity":   state.Severity,
			"attributes": state.Attributes,
			"metadata":   state.Metadata,
			"updated_at": state.UpdatedAt.Format(time.RFC3339Nano),
		}
	}
	return out
}

func chainEventFacts(events []storage.ChainEventRecord) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		out = append(out, map[string]any{
			"chain":           event.Chain,
			"watch":           event.Watch,
			"entity_key":      event.EntityKey,
			"event_type":      event.EventType,
			"source_decision": event.SourceDecision,
			"reason_code":     event.ReasonCode,
			"severity":        event.Severity,
			"attributes":      event.Attributes,
			"metadata":        event.Metadata,
			"created_at":      event.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	return out
}

func lookupInputPath(input map[string]any, path string) any {
	var cur any = input
	for _, part := range splitPath(path) {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func severityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func actionEffect(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "block", "ban", "suspend", "deny", "temporary_block":
		return "deny"
	case "rate_limit", "throttle", "review", "escalate", "notify", "log":
		return "require_review"
	case "warning", "warn", "soft_warning", "allow":
		return "allow"
	default:
		return ""
	}
}

func (s *Service) loadChainStates(ctx context.Context, chain *ChainDefinition, entityKey string) []storage.ChainStateRecord {
	if chain == nil {
		return nil
	}
	var states []storage.ChainStateRecord
	for _, watch := range chain.Watches {
		state, err := s.store.GetChainState(ctx, chain.ID, watch.ID, entityKey)
		if err == nil {
			states = append(states, state)
		}
	}
	return states
}

func (s *Service) newChainEvent(record storage.DefinitionRecord, chain, watch, entityKey, eventType, sourceDecision, reasonCode, severity string, attributes, metadata map[string]any, expiresAt *time.Time) storage.ChainEventRecord {
	return storage.ChainEventRecord{
		ID:             newID("chain-event"),
		TenantID:       record.TenantID,
		Definition:     record.Name,
		Version:        record.Version,
		Environment:    record.Environment,
		Chain:          chain,
		Watch:          watch,
		EntityKey:      entityKey,
		EventType:      eventType,
		SourceDecision: sourceDecision,
		ReasonCode:     reasonCode,
		Severity:       severity,
		Attributes:     attributes,
		Metadata:       metadata,
		CreatedAt:      s.now(),
		ExpiresAt:      expiresAt,
	}
}

func (s *Service) eventsFromDecision(record storage.DefinitionRecord, chain, entityKey string, decision *bcl.DecisionResult) []storage.ChainEventRecord {
	if decision == nil {
		return nil
	}
	attrs := cloneMap(decision.Attributes)
	metadata := cloneMap(decision.Metadata)
	severity := stringAny(metadata["severity"])
	var out []storage.ChainEventRecord
	for _, action := range decision.Events {
		if action.Name != "" {
			out = append(out, s.newChainEvent(record, chain, "", entityKey, action.Name, decision.DecisionID, decision.ReasonCode, severity, action.Params, metadata, nil))
		}
	}
	if event := strings.TrimSpace(fmt.Sprint(decision.Attributes["event"])); event != "" && event != "<nil>" {
		out = append(out, s.newChainEvent(record, chain, "", entityKey, event, decision.DecisionID, decision.ReasonCode, severity, attrs, metadata, nil))
		return out
	}
	if action := strings.TrimSpace(fmt.Sprint(decision.Attributes["action"])); action != "" && action != "<nil>" {
		out = append(out, s.newChainEvent(record, chain, "", entityKey, action, decision.DecisionID, decision.ReasonCode, severity, attrs, metadata, nil))
	}
	return out
}

func (s *Service) applyWatch(ctx context.Context, record storage.DefinitionRecord, chainID, entityKey string, watch ChainWatchDefinition, events []storage.ChainEventRecord, previous storage.ChainStateRecord, now time.Time) (storage.ChainStateRecord, []storage.ChainEventRecord) {
	window := durationOrZero(watch.Window)
	since := time.Time{}
	if window > 0 {
		since = now.Add(-window)
	}
	watchEvents := watch.Events
	if len(watchEvents) == 0 {
		watchEvents = []string{watch.Event}
	}
	watchEventSet := stringSet(watchEvents)
	matching := make([]storage.ChainEventRecord, 0, len(events))
	countsByEvent := map[string]int{}
	for _, event := range events {
		if event.Chain != chainID || event.EntityKey != entityKey || !watchEventSet[event.EventType] {
			continue
		}
		if !since.IsZero() && event.CreatedAt.Before(since) {
			continue
		}
		if event.ExpiresAt != nil && !event.ExpiresAt.After(now) {
			continue
		}
		matching = append(matching, event)
		countsByEvent[event.EventType]++
	}
	count := len(matching)
	if len(watchEvents) > 1 {
		count = minPositiveCount(countsByEvent, watchEvents)
	}
	counters := map[string]int{watch.Event: count}
	for eventType, eventCount := range countsByEvent {
		counters[eventType] = eventCount
	}
	attributes := watchAnalytics(watch, matching, window)
	if distinct := intAny(attributes["distinct_count"]); distinct > 0 {
		counters["distinct"] = distinct
	}
	if watch.Reset != "" && hasWatchEvent(events, chainID, entityKey, watch.Reset, since, now) {
		state := storage.ChainStateRecord{
			TenantID:    record.TenantID,
			Definition:  record.Name,
			Version:     record.Version,
			Environment: record.Environment,
			Chain:       chainID,
			Watch:       watch.ID,
			EntityKey:   entityKey,
			Counters:    map[string]int{},
			Attributes:  map[string]any{"reset": true},
			Metadata:    map[string]any{"risk_score": 0.0, "reset_event": watch.Reset},
			UpdatedAt:   now,
		}
		_ = ctx
		return state, nil
	}
	state := storage.ChainStateRecord{
		TenantID:    record.TenantID,
		Definition:  record.Name,
		Version:     record.Version,
		Environment: record.Environment,
		Chain:       chainID,
		Watch:       watch.ID,
		EntityKey:   entityKey,
		Counters:    counters,
		UpdatedAt:   now,
	}
	if previous.Attributes != nil {
		state.Attributes = cloneMap(previous.Attributes)
	}
	if previous.Metadata != nil {
		state.Metadata = cloneMap(previous.Metadata)
	}
	if len(attributes) > 0 {
		state.Attributes = mergeMaps(state.Attributes, attributes)
	}
	if watch.Decay != "" {
		state.Metadata = mergeMaps(state.Metadata, map[string]any{"risk_score": decayedRiskScore(previous, count, durationOrZero(watch.Decay), now)})
	}
	var matched *ChainStep
	for i := range watch.Steps {
		if count >= watch.Steps[i].Threshold {
			matched = &watch.Steps[i]
		}
	}
	if matched == nil {
		if window > 0 {
			expires := now.Add(window)
			state.ExpiresAt = &expires
		}
		_ = ctx
		return state, nil
	}
	suppressActive := watch.Suppress || truthyAny(previous.Metadata["suppress"]) || suppressibleAction(matched.Action)
	if suppressActive && existingGeneratedActive(events, chainID, watch.ID, entityKey, matched.Action, now) {
		state.Step = previous.Step
		if state.Step == "" {
			state.Step = matched.ID
		}
		state.Action = first(previous.Action, matched.Action)
		state.Severity = first(previous.Severity, matched.Severity)
		state.ExpiresAt = previous.ExpiresAt
		_ = ctx
		return state, nil
	}
	if cooldown := durationOrZero(watch.Cooldown); cooldown > 0 && previous.Action == matched.Action && !previous.UpdatedAt.IsZero() && previous.UpdatedAt.Add(cooldown).After(now) {
		state.Step = first(previous.Step, matched.ID)
		state.Action = previous.Action
		state.Severity = first(previous.Severity, matched.Severity)
		state.ExpiresAt = previous.ExpiresAt
		_ = ctx
		return state, nil
	}
	state.Step = matched.ID
	state.Action = matched.Action
	state.Severity = matched.Severity
	state.Attributes = mergeMaps(state.Attributes, matched.Attributes)
	state.Metadata = mergeMaps(state.Metadata, mergeMaps(watch.Metadata, matched.Metadata))
	if ttl := durationOrZero(matched.TTL); ttl > 0 {
		expires := now.Add(ttl)
		state.ExpiresAt = &expires
	} else if window > 0 {
		expires := now.Add(window)
		state.ExpiresAt = &expires
	}
	expiresAt := (*time.Time)(nil)
	if window > 0 {
		expires := now.Add(window)
		expiresAt = &expires
	}
	event := s.newChainEvent(record, chainID, watch.ID, entityKey, matched.Action, "", strings.ToUpper(strings.ReplaceAll(matched.Action, "-", "_")), matched.Severity, map[string]any{"step": matched.ID, "threshold": matched.Threshold, "count": count}, state.Metadata, expiresAt)
	event.CreatedAt = now
	return state, []storage.ChainEventRecord{event}
}

func watchAnalytics(watch ChainWatchDefinition, events []storage.ChainEventRecord, window time.Duration) map[string]any {
	out := map[string]any{"count": len(events)}
	if window > 0 {
		out["rate_per_minute"] = float64(len(events)) / window.Minutes()
	}
	if watch.Distinct != "" {
		seen := map[string]bool{}
		for _, event := range events {
			if value := strings.TrimSpace(fmt.Sprint(lookupAnyPath(map[string]any{"attributes": event.Attributes, "metadata": event.Metadata}, watch.Distinct))); value != "" && value != "<nil>" {
				seen[value] = true
			}
		}
		out["distinct_count"] = len(seen)
	}
	field := watch.Field
	if field == "" {
		field = firstNumericAttributePath(events)
	}
	if field == "" || len(events) == 0 {
		return out
	}
	values := make([]float64, 0, len(events))
	for _, event := range events {
		if value, ok := floatAny(lookupAnyPath(map[string]any{"attributes": event.Attributes, "metadata": event.Metadata}, field)); ok {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return out
	}
	sort.Float64s(values)
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	metrics := watch.Metrics
	if len(metrics) == 0 {
		metrics = []string{"min", "max", "avg", "p95", "p99"}
	}
	for _, metric := range metrics {
		switch strings.ToLower(strings.TrimSpace(metric)) {
		case "min":
			out["min"] = values[0]
		case "max":
			out["max"] = values[len(values)-1]
		case "avg":
			out["avg"] = sum / float64(len(values))
		case "p95":
			out["p95"] = percentile(values, 0.95)
		case "p99":
			out["p99"] = percentile(values, 0.99)
		case "count":
			out["count"] = len(events)
		case "rate":
			if window > 0 {
				out["rate_per_minute"] = float64(len(events)) / window.Minutes()
			}
		}
	}
	return out
}

func hasWatchEvent(events []storage.ChainEventRecord, chainID, entityKey, eventType string, since, now time.Time) bool {
	for _, event := range events {
		if event.Chain != chainID || event.EntityKey != entityKey || event.EventType != eventType {
			continue
		}
		if !since.IsZero() && event.CreatedAt.Before(since) {
			continue
		}
		if event.ExpiresAt != nil && !event.ExpiresAt.After(now) {
			continue
		}
		return true
	}
	return false
}

func decayedRiskScore(previous storage.ChainStateRecord, count int, halfLife time.Duration, now time.Time) float64 {
	score := 0.0
	if previous.Metadata != nil {
		if value, ok := floatAny(previous.Metadata["risk_score"]); ok {
			score = value
		}
	}
	if halfLife > 0 && score > 0 && !previous.UpdatedAt.IsZero() {
		if elapsed := now.Sub(previous.UpdatedAt); elapsed > 0 {
			score *= math.Pow(0.5, elapsed.Seconds()/halfLife.Seconds())
		}
	}
	return score + float64(count)
}

func firstNumericAttributePath(events []storage.ChainEventRecord) string {
	for _, event := range events {
		for key, value := range event.Attributes {
			if _, ok := floatAny(value); ok {
				return "attributes." + key
			}
		}
	}
	return ""
}

func minPositiveCount(counts map[string]int, keys []string) int {
	min := 0
	for _, key := range keys {
		count := counts[key]
		if count == 0 {
			return 0
		}
		if min == 0 || count < min {
			min = count
		}
	}
	return min
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(values)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}

func existingGeneratedActive(events []storage.ChainEventRecord, chain, watch, entityKey, action string, now time.Time) bool {
	for _, event := range events {
		if event.Chain == chain && event.Watch == watch && event.EntityKey == entityKey && event.EventType == action {
			if event.ExpiresAt == nil || event.ExpiresAt.After(now) {
				return true
			}
		}
	}
	return false
}

func suppressibleAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "log", "notify", "escalate", "ticket":
		return true
	default:
		return false
	}
}

func lookupAnyPath(input map[string]any, path string) any {
	var cur any = input
	for _, part := range splitPath(path) {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func floatAny(v any) (float64, bool) {
	switch x := literalValue(v).(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case jsonNumber:
		n, err := strconv.ParseFloat(string(x), 64)
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return n, err == nil
	default:
		n, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(x)), 64)
		return n, err == nil
	}
}

func durationOrZero(value string) time.Duration {
	if value == "" {
		return 0
	}
	d, err := chainDuration(value)
	if err != nil {
		return 0
	}
	return d
}

func chainDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func cloneChainStates(states []storage.ChainStateRecord) []storage.ChainStateRecord {
	out := make([]storage.ChainStateRecord, 0, len(states))
	for _, state := range states {
		state.Counters = cloneCounters(state.Counters)
		state.Attributes = cloneMap(state.Attributes)
		state.Metadata = cloneMap(state.Metadata)
		out = append(out, state)
	}
	return out
}

func cloneCounters(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func chainStateMap(states []storage.ChainStateRecord) map[string]storage.ChainStateRecord {
	out := map[string]storage.ChainStateRecord{}
	for _, state := range states {
		out[state.Watch] = state
	}
	return out
}

func chainStateSlice(states map[string]storage.ChainStateRecord) []storage.ChainStateRecord {
	out := make([]storage.ChainStateRecord, 0, len(states))
	for _, state := range states {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Watch < out[j].Watch })
	return out
}

func chooseFinalDecision(current, next *bcl.DecisionResult) *bcl.DecisionResult {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	if actionEffect(fmt.Sprint(next.Attributes["action"])) == "deny" || next.Effect == "deny" {
		return next
	}
	if current.Effect == "allow" && next.Effect != "allow" {
		return next
	}
	nextSeverity := severityRank(fmt.Sprint(next.Metadata["severity"]))
	currentSeverity := severityRank(fmt.Sprint(current.Metadata["severity"]))
	if nextSeverity >= currentSeverity {
		return next
	}
	return current
}

func chooseFinalAction(currentAction, currentEffect, currentReason, currentSeverity, nextAction, nextEffect, nextReason, nextSeverity string) (string, string, string, string) {
	if currentAction == "" {
		return nextAction, nextEffect, nextReason, nextSeverity
	}
	if nextEffect == "deny" && currentEffect != "deny" {
		return nextAction, nextEffect, nextReason, nextSeverity
	}
	if severityRank(nextSeverity) >= severityRank(currentSeverity) {
		return nextAction, nextEffect, nextReason, nextSeverity
	}
	return currentAction, currentEffect, currentReason, currentSeverity
}

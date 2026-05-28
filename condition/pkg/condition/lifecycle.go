package condition

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/routing"
	"github.com/oarkflow/condition/pkg/storage"
)

func routeCatalogs(program *bcl.DecisionProgram) (map[string][]routing.Route, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_routes"] != nil {
		collectBlocks(program.Governance["_condition_routes"], "routes", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "routes", &blocks)
	}
	out := map[string][]routing.Route{}
	var diags []bcl.Diagnostic
	for _, block := range blocks {
		id := strings.TrimSpace(stringAny(block["id"]))
		if id == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "routes block is missing id"})
			continue
		}
		var catalogRoutes []routing.Route
		seen := map[string]bool{}
		for _, routeBlock := range childBlocks(block["body"], "route") {
			route, err := routeFromBlock(routeBlock)
			if err != nil {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("routes %q: %v", id, err)})
				continue
			}
			catalogRoutes = append(catalogRoutes, route)
			key := route.Method + " " + route.Pattern
			if seen[key] {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("routes %q has duplicate route %s", id, key)})
				continue
			}
			seen[key] = true
			if _, err := routing.Normalize(route.Pattern); err != nil {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("route %q has invalid pattern %q: %v", route.ID, route.Pattern, err)})
				continue
			}
			out[id] = append(out[id], route)
		}
		for _, diag := range routing.Analyze(catalogRoutes) {
			diags = append(diags, bcl.Diagnostic{Severity: diag.Severity, Message: fmt.Sprintf("routes %q: %s", id, diag.Message)})
		}
	}
	return out, diags
}

func routeFromBlock(block map[string]any) (routing.Route, error) {
	route := routing.Route{ID: strings.TrimSpace(stringAny(block["id"])), Metadata: metadataFromChildBlocks(block["body"])}
	if route.ID == "" {
		return route, fmt.Errorf("route is missing id")
	}
	body := bodyMap(block["body"])
	route.Method = strings.ToUpper(first(stringAny(body["method"]), http.MethodGet))
	route.Pattern = stringAny(body["pattern"])
	if route.Pattern == "" {
		return route, fmt.Errorf("route %q is missing pattern", route.ID)
	}
	return route, nil
}

func lifecycleDefinition(program *bcl.DecisionProgram, lifecycleID string) (*LifecycleDefinition, error) {
	lifecycles, diags := lifecycleDefinitions(program)
	if len(diags) > 0 {
		return nil, fmt.Errorf("%s", diags[0].Message)
	}
	for i := range lifecycles {
		if lifecycleID == "" || lifecycles[i].ID == lifecycleID {
			return &lifecycles[i], nil
		}
	}
	return nil, fmt.Errorf("unknown lifecycle %q", lifecycleID)
}

func lifecycleDefinitions(program *bcl.DecisionProgram) ([]LifecycleDefinition, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_lifecycles"] != nil {
		collectBlocks(program.Governance["_condition_lifecycles"], "lifecycle", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "lifecycle", &blocks)
	}
	var out []LifecycleDefinition
	var diags []bcl.Diagnostic
	seen := map[string]bool{}
	for _, block := range blocks {
		def, err := lifecycleFromBlock(block)
		if err != nil {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: err.Error()})
			continue
		}
		if seen[def.ID] {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate lifecycle %q", def.ID)})
			continue
		}
		seen[def.ID] = true
		out = append(out, *def)
	}
	return out, diags
}

func lifecycleFromBlock(block map[string]any) (*LifecycleDefinition, error) {
	def := &LifecycleDefinition{ID: strings.TrimSpace(stringAny(block["id"]))}
	if def.ID == "" {
		return nil, fmt.Errorf("lifecycle is missing id")
	}
	seenPhases := map[string]bool{}
	for _, item := range blockList(block["body"]) {
		if stringAny(item["type"]) == "phase" {
			phase, err := lifecyclePhaseFromBlock(item)
			if err != nil {
				return nil, fmt.Errorf("lifecycle %q: %w", def.ID, err)
			}
			if seenPhases[phase.ID] {
				return nil, fmt.Errorf("lifecycle %q has duplicate phase %q", def.ID, phase.ID)
			}
			seenPhases[phase.ID] = true
			def.Phases = append(def.Phases, *phase)
			continue
		}
		switch stringAny(item["name"]) {
		case "entity", "entity_key":
			def.EntityKeyPath = blockValueString(item["value"])
		case "routes":
			def.Routes = blockValueString(item["value"])
		}
	}
	if def.EntityKeyPath == "" {
		return nil, fmt.Errorf("lifecycle %q is missing entity_key", def.ID)
	}
	if len(def.Phases) == 0 {
		return nil, fmt.Errorf("lifecycle %q is missing phases", def.ID)
	}
	return def, nil
}

func lifecyclePhaseFromBlock(block map[string]any) (*LifecyclePhaseDefinition, error) {
	phase := &LifecyclePhaseDefinition{ID: strings.TrimSpace(stringAny(block["id"]))}
	if phase.ID == "" {
		return nil, fmt.Errorf("phase is missing id")
	}
	for _, item := range blockList(block["body"]) {
		switch stringAny(item["name"]) {
		case "decision":
			if decision := blockValueString(item["value"]); decision != "" {
				phase.Decisions = append(phase.Decisions, decision)
			}
		case "chain":
			if chain := blockValueString(item["value"]); chain != "" {
				phase.Chains = append(phase.Chains, chain)
			}
		}
	}
	if len(phase.Decisions) == 0 && len(phase.Chains) == 0 {
		return nil, fmt.Errorf("phase %q is missing decisions or chains", phase.ID)
	}
	return phase, nil
}

func validateRoutesAndLifecycles(program *bcl.DecisionProgram) []bcl.Diagnostic {
	catalogs, diags := routeCatalogs(program)
	lifecycles, lifecycleDiags := lifecycleDefinitions(program)
	diags = append(diags, lifecycleDiags...)
	chainSet := map[string]bool{}
	chains, chainDiags := chainDefinitions(program)
	diags = append(diags, chainDiags...)
	for _, chain := range chains {
		chainSet[chain.ID] = true
	}
	for _, lifecycle := range lifecycles {
		if lifecycle.Routes != "" {
			if _, ok := catalogs[lifecycle.Routes]; !ok {
				diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("lifecycle %q references unknown routes %q", lifecycle.ID, lifecycle.Routes)})
			}
		}
		for _, phase := range lifecycle.Phases {
			for _, decision := range phase.Decisions {
				if program.Decisions[decision] == nil {
					diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("lifecycle %q phase %q references unknown decision %q", lifecycle.ID, phase.ID, decision)})
				}
			}
			for _, chain := range phase.Chains {
				if !chainSet[chain] {
					diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("lifecycle %q phase %q references unknown chain %q", lifecycle.ID, phase.ID, chain)})
				}
			}
		}
	}
	tests, testDiags := lifecycleScenarios(program)
	diags = append(diags, testDiags...)
	lifecycleByID := map[string]LifecycleDefinition{}
	for _, lifecycle := range lifecycles {
		lifecycleByID[lifecycle.ID] = lifecycle
	}
	for _, test := range tests {
		lifecycle, ok := lifecycleByID[test.Lifecycle]
		if !ok {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("lifecycle_test %q references unknown lifecycle %q", test.Name, test.Lifecycle)})
			continue
		}
		if lifecyclePhase(&lifecycle, test.Phase) == nil {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("lifecycle_test %q references unknown phase %q", test.Name, test.Phase)})
		}
	}
	return diags
}

func lifecyclePhase(def *LifecycleDefinition, phase string) *LifecyclePhaseDefinition {
	if def == nil {
		return nil
	}
	phase = strings.TrimSpace(phase)
	for i := range def.Phases {
		if def.Phases[i].ID == phase {
			return &def.Phases[i]
		}
	}
	return nil
}

func (s *Service) matchLifecycleRoute(program *bcl.DecisionProgram, lifecycle *LifecycleDefinition, method, path string) routing.Match {
	if strings.TrimSpace(method) == "" || strings.TrimSpace(path) == "" {
		return routing.Match{}
	}
	var routes []routing.Route
	if lifecycle != nil && lifecycle.Routes != "" {
		catalogs, _ := routeCatalogs(program)
		routes = append(routes, catalogs[lifecycle.Routes]...)
	}
	routes = append(routes, s.cfg.Routes...)
	if len(routes) == 0 {
		return routing.Match{}
	}
	matcher, err := routing.Compile(routes)
	if err != nil {
		return routing.Match{}
	}
	return matcher.Match(method, path)
}

func lifecycleRouteFacts(match routing.Match, method, path string) map[string]any {
	facts := map[string]any{
		"matched":            match.Matched,
		"id":                 match.ID,
		"method":             strings.ToUpper(strings.TrimSpace(method)),
		"path":               path,
		"pattern":            match.Pattern,
		"route_template":     match.NormalizedPattern,
		"route_pattern":      match.FiberPattern,
		"normalized_pattern": match.NormalizedPattern,
		"fiber_pattern":      match.FiberPattern,
		"params":             match.Params,
		"metadata":           match.Metadata,
	}
	for key, value := range match.Metadata {
		facts[key] = value
	}
	for key, value := range match.Params {
		facts[key+"_param"] = value
	}
	return facts
}

func applyPolicyOverlays(program *bcl.DecisionProgram, match routing.Match, tenant, environment, endpoint string) (routing.Match, map[string]any) {
	overlays, _ := policyOverlays(program)
	applied := []string(nil)
	metadata := cloneMap(match.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	for _, overlay := range overlays {
		if !overlayApplies(overlay, match, tenant, environment, endpoint) {
			continue
		}
		for key, value := range overlay.Metadata {
			metadata[key] = value
		}
		applied = append(applied, overlay.ID)
	}
	match.Metadata = metadata
	return match, map[string]any{
		"applied": applied,
		"count":   len(applied),
	}
}

func overlayApplies(overlay PolicyOverlayDefinition, match routing.Match, tenant, environment, endpoint string) bool {
	if overlay.Environment != "" && !overlayScopeMatches(overlay.Environment, environment) {
		return false
	}
	if overlay.TenantID != "" && !overlayScopeMatches(overlay.TenantID, tenant) {
		return false
	}
	switch overlay.Layer {
	case "global":
		return true
	case "environment":
		return overlayScopeMatches(overlay.Environment, environment)
	case "tenant":
		return overlayScopeMatches(overlay.TenantID, tenant)
	case "endpoint":
		return overlayScopeMatches(overlay.Endpoint, endpoint)
	case "route":
		return overlayScopeMatches(overlay.RouteID, match.ID)
	default:
		return false
	}
}

func overlayScopeMatches(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	return strings.EqualFold(pattern, strings.TrimSpace(value))
}

func mapFromAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return cloneMap(m)
	}
	return map[string]any{}
}

func (e *LifecycleEvaluation) FinalDecision(decision *bcl.DecisionResult) {
	if decision == nil {
		return
	}
	current := &bcl.DecisionResult{Effect: e.FinalEffect, Reason: e.FinalReason, Attributes: map[string]any{"action": e.FinalAction}}
	if e.FinalEffect == "" {
		current = nil
	}
	chosen := chooseFinalDecision(current, decision)
	if chosen == nil {
		return
	}
	e.FinalEffect = chosen.Effect
	e.FinalReason = chosen.Reason
	if action := strings.TrimSpace(fmt.Sprint(chosen.Attributes["action"])); action != "" && action != "<nil>" {
		e.FinalAction = action
	}
}

func (s *Service) lifecycleActionsFromDecision(ctx context.Context, record storage.DefinitionRecord, lifecycleID, entityKey string, decision *bcl.DecisionResult, dryRun bool) []LifecycleAction {
	if decision == nil {
		return nil
	}
	attrs := cloneMap(decision.Attributes)
	metadata := cloneMap(decision.Metadata)
	severity := stringAny(metadata["severity"])
	var actions []LifecycleAction
	for _, event := range decision.Events {
		if event.Name == "" {
			continue
		}
		actions = append(actions, s.lifecycleAction(ctx, record, lifecycleID, entityKey, event.Name, decision.ReasonCode, severity, event.Params, metadata, dryRun))
	}
	if action := strings.TrimSpace(fmt.Sprint(attrs["action"])); action != "" && action != "<nil>" {
		actions = append(actions, s.lifecycleAction(ctx, record, lifecycleID, entityKey, action, decision.ReasonCode, severity, attrs, metadata, dryRun))
	}
	return actions
}

func (s *Service) lifecycleAction(ctx context.Context, record storage.DefinitionRecord, lifecycleID, entityKey, name, reasonCode, severity string, attrs, metadata map[string]any, dryRun bool) LifecycleAction {
	action := LifecycleAction{Name: name, ReasonCode: reasonCode, Severity: severity, Attributes: cloneMap(attrs), Metadata: cloneMap(metadata)}
	if sink := strings.TrimSpace(fmt.Sprint(attrs["sink"])); sink != "" && sink != "<nil>" {
		action.Sink = sink
	} else if name == "log" {
		action.Sink = "log"
	}
	event := s.newChainEvent(record, lifecycleID, "", entityKey, name, "", reasonCode, severity, attrs, metadata, nil)
	_ = s.store.AppendChainEvent(ctx, event)
	handled, result := false, &ActionResult{Handled: false, Status: "dry_run"}
	if !dryRun {
		handled, result = s.dispatchLifecycleAction(ctx, record, action)
	}
	action.Handled = handled
	action.Result = result
	delivery := s.actionDeliveryRecord(record, lifecycleID, entityKey, action)
	if delivery.Status == "" {
		delivery.Status = "event_persisted"
	}
	if err := s.store.SaveActionDelivery(ctx, delivery); err == nil {
		action.DeliveryID = delivery.ID
	}
	if incident, ok := s.incidentFromAction(ctx, record, lifecycleID, entityKey, action); ok {
		if err := s.store.UpsertIncident(ctx, incident); err == nil {
			action.IncidentID = incident.ID
		}
	}
	return action
}

func (s *Service) actionDeliveryRecord(record storage.DefinitionRecord, lifecycleID, entityKey string, action LifecycleAction) storage.ActionDeliveryRecord {
	now := s.now()
	delivery := storage.ActionDeliveryRecord{
		ID:          newID("action-delivery"),
		TenantID:    record.TenantID,
		Definition:  record.Name,
		Version:     record.Version,
		Environment: record.Environment,
		Lifecycle:   lifecycleID,
		EntityKey:   entityKey,
		Action:      action.Name,
		Sink:        action.Sink,
		Handled:     action.Handled,
		Attempts:    1,
		MaxAttempts: 1,
		ReasonCode:  action.ReasonCode,
		Severity:    action.Severity,
		Attributes:  action.Attributes,
		Metadata:    action.Metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if action.Result != nil {
		delivery.Status = action.Result.Status
		delivery.Error = action.Result.Error
	}
	if def, ok := actionDefinition(record.Program, action.Name); ok && def.Retries > 0 {
		delivery.MaxAttempts = 1 + def.Retries
	}
	if delivery.Status == "action_not_allowlisted" {
		return delivery
	}
	if delivery.Error != "" || (!delivery.Handled && delivery.Sink != "" && delivery.Sink != "event") {
		if delivery.Attempts >= delivery.MaxAttempts {
			delivery.Status = "dead_letter"
		} else {
			next := now.Add(time.Duration(delivery.Attempts) * time.Minute)
			delivery.NextAttempt = &next
			delivery.Status = "retry_scheduled"
		}
	}
	return delivery
}

func (s *Service) incidentFromAction(ctx context.Context, record storage.DefinitionRecord, lifecycleID, entityKey string, action LifecycleAction) (storage.IncidentRecord, bool) {
	if !incidentAction(action.Name, action.Severity) {
		return storage.IncidentRecord{}, false
	}
	groupKey := strings.Join([]string{record.Name, record.Environment, lifecycleID, entityKey, action.Name, action.ReasonCode}, "|")
	id := "incident-" + stableID(groupKey)
	now := s.now()
	count := 1
	firstSeen := now
	if existing, err := s.store.ListIncidents(ctx, storage.IncidentQuery{Definition: record.Name, Environment: record.Environment, Action: action.Name}); err == nil {
		for _, item := range existing {
			if item.ID == id {
				count = item.Count + 1
				firstSeen = item.FirstSeen
				break
			}
		}
	}
	return storage.IncidentRecord{
		ID:          id,
		TenantID:    record.TenantID,
		Definition:  record.Name,
		Environment: record.Environment,
		GroupKey:    groupKey,
		Status:      "open",
		Action:      action.Name,
		Severity:    action.Severity,
		ReasonCode:  action.ReasonCode,
		Count:       count,
		FirstSeen:   firstSeen,
		LastSeen:    now,
		Metadata:    map[string]any{"lifecycle": lifecycleID, "entity_key": entityKey},
	}, true
}

func incidentAction(action, severity string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "notify", "escalate", "suspend", "ban", "block", "temporary_block", "deny":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "high", "critical":
		return true
	default:
		return false
	}
}

func stableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func (s *Service) dispatchLifecycleAction(ctx context.Context, record storage.DefinitionRecord, action LifecycleAction) (bool, *ActionResult) {
	if allowed, reason := s.actionRuntimeAllowed(ctx, record, action); !allowed {
		return false, &ActionResult{Handled: false, Status: "action_not_allowlisted", Error: reason}
	}
	if handler, key := s.actionHandler(action); handler != nil {
		result, err := handler(ctx, action)
		if err != nil {
			return false, &ActionResult{Handled: false, Status: key, Error: err.Error()}
		}
		if result.Status == "" {
			result.Status = key
		}
		return result.Handled, &result
	}
	switch strings.ToLower(strings.TrimSpace(action.Sink)) {
	case "", "event":
		return false, nil
	case "log":
		payload, _ := json.Marshal(action)
		fmt.Printf("condition lifecycle action: %s\n", payload)
		return true, &ActionResult{Handled: true, Status: "log"}
	case "webhook":
		handled := s.dispatchWebhook(ctx, action)
		status := "webhook_delivered"
		if !handled {
			status = "webhook_skipped"
		}
		return handled, &ActionResult{Handled: handled, Status: status}
	default:
		return false, nil
	}
}

func (s *Service) actionRuntimeAllowed(ctx context.Context, record storage.DefinitionRecord, action LifecycleAction) (bool, string) {
	if s == nil || len(s.cfg.Runtime.ActionAllowlists) == 0 {
		return true, ""
	}
	tenant := first(record.TenantID, TenantFromContext(ctx), s.cfg.DefaultTenant)
	env := first(record.Environment, s.cfg.Environment)
	sink := strings.ToLower(strings.TrimSpace(action.Sink))
	if sink == "" {
		sink = "event"
	}
	for _, allow := range s.cfg.Runtime.ActionAllowlists {
		if !matchRuntimeScope(allow.TenantID, tenant) || !matchRuntimeScope(allow.Environment, env) {
			continue
		}
		if stringAllowed(allow.Actions, action.Name) && stringAllowed(allow.Sinks, sink) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("action %q sink %q is not allowlisted for tenant %q environment %q", action.Name, sink, tenant, env)
}

func matchRuntimeScope(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	return strings.EqualFold(pattern, strings.TrimSpace(value))
}

func (s *Service) actionHandler(action LifecycleAction) (ActionHandler, string) {
	if s == nil || len(s.cfg.ActionHandlers) == 0 {
		return nil, ""
	}
	for _, key := range []string{action.Sink, action.Name} {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if handler := s.cfg.ActionHandlers[key]; handler != nil {
			return handler, key
		}
	}
	return nil, ""
}

func (s *Service) dispatchWebhook(ctx context.Context, action LifecycleAction) bool {
	if !stringAllowed(s.cfg.Runtime.AllowedActionSinks, "webhook") {
		return false
	}
	rawURL := strings.TrimSpace(fmt.Sprint(action.Attributes["url"]))
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" || !stringAllowed(s.cfg.Runtime.AllowedWebhookHosts, parsed.Hostname()) {
		return false
	}
	method := strings.ToUpper(first(strings.TrimSpace(fmt.Sprint(action.Attributes["method"])), http.MethodPost))
	if !stringAllowed(s.cfg.Runtime.AllowedWebhookMethods, method) {
		return false
	}
	timeout := s.cfg.Runtime.WebhookTimeout
	if timeout <= 0 {
		timeout = s.cfg.Runtime.ExternalTimeout
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	body, _ := json.Marshal(action)
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func stringAllowed(values []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == want || strings.TrimSpace(value) == "*" {
			return true
		}
	}
	return false
}

package condition

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/oarkflow/condition/pkg/storage"
)

func (s *Service) RouteCoverage(ctx context.Context, definition string) (*RouteCoverageReport, error) {
	ctx = s.requestContext(ctx, "")
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	catalogs, routeDiags := routeCatalogs(record.Program)
	lifecycles, lifecycleDiags := lifecycleDefinitions(record.Program)
	report := RouteCoverageReport{Definition: definition, Passed: true}
	if len(routeDiags) > 0 || len(lifecycleDiags) > 0 {
		report.Passed = false
	}
	type coverageRef struct {
		lifecycle string
		phase     string
	}
	coverage := map[string][]coverageRef{}
	for _, lifecycle := range lifecycles {
		if lifecycle.Routes == "" {
			continue
		}
		for _, phase := range lifecycle.Phases {
			if len(phase.Decisions) == 0 && len(phase.Chains) == 0 {
				continue
			}
			coverage[lifecycle.Routes] = append(coverage[lifecycle.Routes], coverageRef{lifecycle: lifecycle.ID, phase: phase.ID})
		}
	}
	for catalogID, routes := range catalogs {
		for _, route := range routes {
			refs := coverage[catalogID]
			item := RouteCoverageItem{Catalog: catalogID, RouteID: route.ID, Method: route.Method, Pattern: route.Pattern, Covered: len(refs) > 0}
			seenLifecycle := map[string]bool{}
			seenPhase := map[string]bool{}
			for _, ref := range refs {
				if !seenLifecycle[ref.lifecycle] {
					item.Lifecycles = append(item.Lifecycles, ref.lifecycle)
					seenLifecycle[ref.lifecycle] = true
				}
				key := ref.lifecycle + "." + ref.phase
				if !seenPhase[key] {
					item.Phases = append(item.Phases, key)
					seenPhase[key] = true
				}
			}
			sort.Strings(item.Lifecycles)
			sort.Strings(item.Phases)
			if !item.Covered {
				report.Passed = false
				report.Uncovered = append(report.Uncovered, fmt.Sprintf("%s.%s", catalogID, route.ID))
			}
			report.Routes = append(report.Routes, item)
		}
	}
	sort.Slice(report.Routes, func(i, j int) bool {
		if report.Routes[i].Catalog == report.Routes[j].Catalog {
			return report.Routes[i].RouteID < report.Routes[j].RouteID
		}
		return report.Routes[i].Catalog < report.Routes[j].Catalog
	})
	sort.Strings(report.Uncovered)
	envelope, err := s.audit(ctx, "route_coverage", definition, record.Version, record.Environment, record.Digest, map[string]any{"definition": definition}, report, start, nil)
	if err != nil {
		return nil, err
	}
	report.Audit = envelope
	if err := s.store.SaveReport(ctx, storage.ReportRecord{ID: envelope.ID, Kind: "route_coverage", Definition: definition, CreatedAt: s.now(), Payload: map[string]any{"report": report}}); err != nil {
		return nil, err
	}
	return &report, nil
}

package condition

import (
	"fmt"

	"github.com/oarkflow/bcl"
)

func packageExplain(definition, baseVersion string, base, candidate *bcl.DecisionProgram) PackageExplainReport {
	report := PackageExplainReport{
		Definition:  definition,
		BaseVersion: baseVersion,
		Decisions:   diffStrings(decisionIDs(base), decisionIDs(candidate)),
		Chains:      diffStrings(chainIDs(base), chainIDs(candidate)),
		Routes:      diffStrings(routeIDs(base), routeIDs(candidate)),
		Lifecycles:  diffStrings(lifecycleIDs(base), lifecycleIDs(candidate)),
		Actions:     diffStrings(declaredActionNames(base), declaredActionNames(candidate)),
	}
	for label, diff := range map[string]PackageDiff[string]{
		"decision":  report.Decisions,
		"chain":     report.Chains,
		"route":     report.Routes,
		"lifecycle": report.Lifecycles,
		"action":    report.Actions,
	} {
		for _, item := range diff.Added {
			report.Summary = append(report.Summary, fmt.Sprintf("added %s %q", label, item))
		}
		for _, item := range diff.Removed {
			report.Summary = append(report.Summary, fmt.Sprintf("removed %s %q", label, item))
		}
	}
	if candidate != nil {
		report.Diagnostics = append(report.Diagnostics, candidate.Diagnostics...)
	}
	return report
}

func diffStrings(base, candidate []string) PackageDiff[string] {
	baseSet := stringSet(base)
	candidateSet := stringSet(candidate)
	diff := PackageDiff[string]{}
	for _, item := range candidate {
		if !baseSet[item] {
			diff.Added = append(diff.Added, item)
		} else {
			diff.Common = append(diff.Common, item)
		}
	}
	for _, item := range base {
		if !candidateSet[item] {
			diff.Removed = append(diff.Removed, item)
		}
	}
	return diff
}

func decisionIDs(program *bcl.DecisionProgram) []string {
	set := map[string]bool{}
	if program != nil {
		for id := range program.Decisions {
			set[id] = true
		}
	}
	return sortedKeys(set)
}

func chainIDs(program *bcl.DecisionProgram) []string {
	set := map[string]bool{}
	chains, _ := chainDefinitions(program)
	for _, chain := range chains {
		set[chain.ID] = true
	}
	return sortedKeys(set)
}

func routeIDs(program *bcl.DecisionProgram) []string {
	set := map[string]bool{}
	catalogs, _ := routeCatalogs(program)
	for catalogID, routes := range catalogs {
		for _, route := range routes {
			set[catalogID+"."+route.ID] = true
		}
	}
	return sortedKeys(set)
}

func lifecycleIDs(program *bcl.DecisionProgram) []string {
	set := map[string]bool{}
	lifecycles, _ := lifecycleDefinitions(program)
	for _, lifecycle := range lifecycles {
		set[lifecycle.ID] = true
	}
	return sortedKeys(set)
}

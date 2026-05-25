package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Lead struct {
	ID      string
	Country string
	Lat     float64
	Lon     float64
}

type Territory struct {
	Name            string
	Country         string
	CenterLat       float64
	CenterLon       float64
	CapacityPercent int
	Licensed        bool
	DataRegion      string
}

type TerritoryCase struct {
	Name        string
	Lead        Lead
	Territories []Territory
}

type AssignmentPlan struct {
	Territory       Territory
	DistanceMiles   int
	DataResidencyOK bool
	Facts           map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "geo-territory-assignment", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range territoryCases() {
		plan := assignTerritory(c)
		resp, err := svc.Evaluate(ctx, "geo-territory-assignment", condition.EvaluateRequest{Decision: "geo_territory_assignment", Input: plan.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Lead.ID, c.Name)
		fmt.Printf("  territory: best=%s distance=%dmi capacity=%d%% residency=%v\n", plan.Territory.Name, plan.DistanceMiles, plan.Territory.CapacityPercent, plan.DataResidencyOK)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
	}
}

func territoryCases() []TerritoryCase {
	territories := []Territory{
		{Name: "us-west-enterprise", Country: "US", CenterLat: 37.7749, CenterLon: -122.4194, CapacityPercent: 72, Licensed: true, DataRegion: "US"},
		{Name: "eu-central", Country: "DE", CenterLat: 52.52, CenterLon: 13.405, CapacityPercent: 94, Licensed: true, DataRegion: "EU"},
		{Name: "partner-mena", Country: "AE", CenterLat: 25.2048, CenterLon: 55.2708, CapacityPercent: 48, Licensed: false, DataRegion: "EU"},
	}
	return []TerritoryCase{
		{Name: "bay area expansion lead", Lead: Lead{ID: "lead-1", Country: "US", Lat: 37.33, Lon: -121.89}, Territories: territories},
		{Name: "overloaded eu territory", Lead: Lead{ID: "lead-2", Country: "DE", Lat: 48.1351, Lon: 11.582}, Territories: territories},
		{Name: "blocked country inquiry", Lead: Lead{ID: "lead-3", Country: "IR", Lat: 35.6892, Lon: 51.389}, Territories: territories},
	}
}

func assignTerritory(c TerritoryCase) AssignmentPlan {
	best := c.Territories[0]
	bestDistance := 1 << 30
	for _, territory := range c.Territories {
		distance := distanceMiles(c.Lead.Lat, c.Lead.Lon, territory.CenterLat, territory.CenterLon)
		if territory.Country == c.Lead.Country && distance < bestDistance {
			best = territory
			bestDistance = distance
		}
	}
	residencyOK := best.DataRegion == "US" && c.Lead.Country == "US" || best.DataRegion == "EU" && c.Lead.Country != "US"
	return AssignmentPlan{
		Territory:       best,
		DistanceMiles:   bestDistance,
		DataResidencyOK: residencyOK,
		Facts: map[string]any{
			"lead":      map[string]any{"id": c.Lead.ID, "country": c.Lead.Country},
			"territory": map[string]any{"name": best.Name, "capacity_percent": best.CapacityPercent, "distance_miles": bestDistance, "licensed": best.Licensed, "data_residency_ok": residencyOK},
		},
	}
}

func distanceMiles(lat1, lon1, lat2, lon2 float64) int {
	const earthMiles = 3958.8
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return int(earthMiles * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a)))
}

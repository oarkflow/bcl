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

type IntersectionSensors struct {
	ID              string
	QueueLength     int
	AverageSpeedKPH int
	PedestrianCount int
	SensorFault     bool
}

type IncidentFeed struct {
	EmergencyVehicle bool
	CrowdEvent       bool
	BlockedLane      bool
}

type TrafficCase struct {
	Name     string
	Sensors  IntersectionSensors
	Incident IncidentFeed
}

type CorridorState struct {
	CongestionIndex   int
	ClearancePossible bool
	Facts             map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "smart-city-traffic", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range trafficFeed() {
		state := computeCorridorState(c)
		resp, err := svc.Evaluate(ctx, "smart-city-traffic", condition.EvaluateRequest{Decision: "traffic_priority", Input: state.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Sensors.ID, c.Name)
		fmt.Printf("  corridor: congestion=%d clearance=%v sensor_fault=%v\n", state.CongestionIndex, state.ClearancePossible, c.Sensors.SensorFault)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		applySignalPlan(c, state, decision.Effect)
	}
}

func trafficFeed() []TrafficCase {
	return []TrafficCase{
		{Name: "normal morning flow", Sensors: IntersectionSensors{ID: "int-1", QueueLength: 8, AverageSpeedKPH: 42, PedestrianCount: 10}, Incident: IncidentFeed{}},
		{Name: "stadium release", Sensors: IntersectionSensors{ID: "int-2", QueueLength: 80, AverageSpeedKPH: 8, PedestrianCount: 450}, Incident: IncidentFeed{CrowdEvent: true}},
		{Name: "ambulance approach", Sensors: IntersectionSensors{ID: "int-3", QueueLength: 20, AverageSpeedKPH: 30, PedestrianCount: 20}, Incident: IncidentFeed{EmergencyVehicle: true}},
	}
}

func computeCorridorState(c TrafficCase) CorridorState {
	congestion := int(math.Min(100, float64(c.Sensors.QueueLength)+float64(c.Sensors.PedestrianCount)/20+float64(50-c.Sensors.AverageSpeedKPH)))
	if congestion < 0 {
		congestion = 0
	}
	clearance := !c.Incident.BlockedLane && !c.Sensors.SensorFault && c.Sensors.PedestrianCount < 200
	return CorridorState{
		CongestionIndex:   congestion,
		ClearancePossible: clearance,
		Facts: map[string]any{
			"corridor": map[string]any{"id": c.Sensors.ID, "congestion_index": congestion, "sensor_fault": c.Sensors.SensorFault, "clearance_possible": clearance},
			"incident": map[string]any{"emergency_vehicle": c.Incident.EmergencyVehicle, "crowd_event": c.Incident.CrowdEvent},
		},
	}
}

func applySignalPlan(c TrafficCase, s CorridorState, effect string) {
	switch effect {
	case "allow":
		if c.Incident.EmergencyVehicle {
			fmt.Printf("  preempt lights for emergency corridor and notify downstream intersections\n")
			return
		}
		fmt.Printf("  keep normal timing plan active\n")
	case "require_review":
		fmt.Printf("  open operator console and recommend cycle extension for congestion=%d\n", s.CongestionIndex)
	default:
		fmt.Printf("  hold automated change and alert traffic engineer\n")
	}
}

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type SensorSample struct {
	At           time.Time
	TemperatureC float64
	VibrationMMS float64
	GuardOpen    bool
}

type Machine struct {
	ID                string
	Line              string
	HoursSinceService int
	RatedUnitsPerHour int
}

type QualityWindow struct {
	UnitsInspected int
	Defects        int
}

type MachineCase struct {
	Name    string
	Machine Machine
	Samples []SensorSample
	Quality QualityWindow
}

type MachineTelemetry struct {
	LatestTemperature float64
	VibrationRMS      float64
	GuardOpen         bool
	DefectRate        float64
	Facts             map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "manufacturing-iot", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range productionLine() {
		telemetry := aggregateTelemetry(c)
		resp, err := svc.Evaluate(ctx, "manufacturing-iot", condition.EvaluateRequest{Decision: "machine_safety", Input: telemetry.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Machine.ID, c.Name)
		fmt.Printf("  telemetry: temp=%.1fC vib_rms=%.1f defects=%.1f%% service_hours=%d\n", telemetry.LatestTemperature, telemetry.VibrationRMS, telemetry.DefectRate, c.Machine.HoursSinceService)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		operateMachine(c, telemetry, decision.Effect)
	}
}

func productionLine() []MachineCase {
	now := time.Date(2026, 5, 24, 6, 0, 0, 0, time.UTC)
	return []MachineCase{
		{Name: "normal production", Machine: Machine{ID: "press-1", Line: "A", HoursSinceService: 120, RatedUnitsPerHour: 180}, Samples: samples(now, []float64{70, 72, 73}, []float64{3, 4, 4}, false), Quality: QualityWindow{UnitsInspected: 500, Defects: 5}},
		{Name: "rising vibration", Machine: Machine{ID: "press-2", Line: "A", HoursSinceService: 420, RatedUnitsPerHour: 160}, Samples: samples(now, []float64{74, 76, 77}, []float64{10, 13, 14}, false), Quality: QualityWindow{UnitsInspected: 480, Defects: 8}},
		{Name: "open safety guard", Machine: Machine{ID: "press-3", Line: "B", HoursSinceService: 90, RatedUnitsPerHour: 140}, Samples: samples(now, []float64{70, 70, 71}, []float64{3, 3, 4}, true), Quality: QualityWindow{UnitsInspected: 300, Defects: 2}},
	}
}

func aggregateTelemetry(c MachineCase) MachineTelemetry {
	last := c.Samples[len(c.Samples)-1]
	vib := rmsVibration(c.Samples)
	defectRate := 0.0
	if c.Quality.UnitsInspected > 0 {
		defectRate = float64(c.Quality.Defects) / float64(c.Quality.UnitsInspected) * 100
	}
	return MachineTelemetry{
		LatestTemperature: last.TemperatureC,
		VibrationRMS:      vib,
		GuardOpen:         last.GuardOpen,
		DefectRate:        defectRate,
		Facts: map[string]any{
			"sensor": map[string]any{
				"temperature_c":  last.TemperatureC,
				"vibration_mm_s": vib,
				"guard_open":     last.GuardOpen,
			},
			"machine": map[string]any{
				"id":                  c.Machine.ID,
				"hours_since_service": c.Machine.HoursSinceService,
			},
			"quality": map[string]any{"defect_rate_percent": defectRate},
		},
	}
}

func operateMachine(c MachineCase, t MachineTelemetry, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  keep line %s running, expected hourly output=%d\n", c.Machine.Line, c.Machine.RatedUnitsPerHour)
	case "require_review":
		fmt.Printf("  create vibration inspection work order, attach RMS %.1f\n", t.VibrationRMS)
	default:
		fmt.Printf("  execute controlled stop and lock out %s\n", c.Machine.ID)
	}
}

func samples(now time.Time, temps, vibrations []float64, guardOpen bool) []SensorSample {
	out := make([]SensorSample, 0, len(temps))
	for i := range temps {
		out = append(out, SensorSample{At: now.Add(time.Duration(i) * time.Minute), TemperatureC: temps[i], VibrationMMS: vibrations[i], GuardOpen: guardOpen})
	}
	return out
}

func rmsVibration(samples []SensorSample) float64 {
	var sum float64
	for _, s := range samples {
		sum += s.VibrationMMS * s.VibrationMMS
	}
	return math.Sqrt(sum / float64(len(samples)))
}

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type LoginEvent struct {
	SessionID string
	UserID    string
	At        time.Time
	Lat       float64
	Lon       float64
	IP        string
	UserAgent string
}

type DeviceInventory struct {
	DeviceID string
	Trusted  bool
	LastSeen time.Time
}

type ThreatIntel struct {
	TorExitNode        bool
	CredentialStuffing bool
	MITMSignal         bool
}

type SessionCase struct {
	Name     string
	Current  LoginEvent
	Previous LoginEvent
	Device   DeviceInventory
	Threat   ThreatIntel
}

type SessionAnalysis struct {
	RiskScore        int
	ImpossibleTravel bool
	VelocityKPH      float64
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "cybersecurity-session-risk", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range sessions() {
		analysis := analyzeSession(c)
		resp, err := svc.Evaluate(ctx, "cybersecurity-session-risk", condition.EvaluateRequest{Decision: "session_risk", Input: analysis.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Current.SessionID, c.Name)
		fmt.Printf("  analysis: risk=%d velocity=%.0fkm/h impossible=%v trusted=%v\n", analysis.RiskScore, analysis.VelocityKPH, analysis.ImpossibleTravel, c.Device.Trusted)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		enforceSession(c, analysis, decision.Effect)
	}
}

func sessions() []SessionCase {
	now := time.Date(2026, 5, 24, 15, 0, 0, 0, time.UTC)
	return []SessionCase{
		{Name: "trusted employee session", Current: LoginEvent{SessionID: "sess-1", UserID: "u-1", At: now, Lat: 37.7749, Lon: -122.4194, IP: "203.0.113.10"}, Previous: LoginEvent{At: now.Add(-8 * time.Hour), Lat: 37.7749, Lon: -122.4194}, Device: DeviceInventory{DeviceID: "dev-1", Trusted: true, LastSeen: now.Add(-24 * time.Hour)}},
		{Name: "unknown device via tor", Current: LoginEvent{SessionID: "sess-2", UserID: "u-2", At: now, Lat: 48.8566, Lon: 2.3522, IP: "198.51.100.12"}, Previous: LoginEvent{At: now.Add(-20 * time.Hour), Lat: 40.7128, Lon: -74.0060}, Device: DeviceInventory{DeviceID: "dev-2"}, Threat: ThreatIntel{TorExitNode: true}},
		{Name: "impossible travel", Current: LoginEvent{SessionID: "sess-3", UserID: "u-3", At: now, Lat: 35.6762, Lon: 139.6503, IP: "192.0.2.15"}, Previous: LoginEvent{At: now.Add(-2 * time.Hour), Lat: 40.7128, Lon: -74.0060}, Device: DeviceInventory{DeviceID: "dev-3", Trusted: true}, Threat: ThreatIntel{CredentialStuffing: true}},
	}
}

func analyzeSession(c SessionCase) SessionAnalysis {
	velocity := travelVelocity(c.Previous, c.Current)
	impossible := velocity > 900
	risk := 10
	if impossible {
		risk += 80
	}
	if !c.Device.Trusted {
		risk += 35
	}
	if c.Threat.TorExitNode {
		risk += 25
	}
	if c.Threat.CredentialStuffing || c.Threat.MITMSignal {
		risk += 50
	}
	if risk > 99 {
		risk = 99
	}
	return SessionAnalysis{
		RiskScore:        risk,
		ImpossibleTravel: impossible,
		VelocityKPH:      velocity,
		Facts: map[string]any{
			"session": map[string]any{"id": c.Current.SessionID, "risk_score": risk, "impossible_travel": impossible},
			"device":  map[string]any{"id": c.Device.DeviceID, "trusted": c.Device.Trusted},
			"network": map[string]any{"ip": c.Current.IP, "tor_exit_node": c.Threat.TorExitNode},
			"threat":  map[string]any{"credential_stuffing": c.Threat.CredentialStuffing, "mitm_signal": c.Threat.MITMSignal},
		},
	}
}

func enforceSession(c SessionCase, a SessionAnalysis, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  continue session and refresh risk token\n")
	case "require_review":
		fmt.Printf("  step-up MFA; device=%s velocity=%.0fkm/h\n", c.Device.DeviceID, a.VelocityKPH)
	default:
		fmt.Printf("  revoke session, reset password, open SOC case for %s\n", c.Current.UserID)
	}
}

func travelVelocity(prev, cur LoginEvent) float64 {
	hours := cur.At.Sub(prev.At).Hours()
	if hours <= 0 {
		return math.Inf(1)
	}
	return haversine(prev.Lat, prev.Lon, cur.Lat, cur.Lon) / hours
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earthKM = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	lat1 *= math.Pi / 180
	lat2 *= math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

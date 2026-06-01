package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Creator struct {
	ID              string
	ReputationScore int
	Strikes         int
}

type MediaAsset struct {
	ID      string
	Kind    string
	Caption string
	AudioFP string
	ImageFP string
	Creator Creator
}

type ClassifierScores struct {
	NSFWScore      float64
	ToxicityScore  float64
	CopyrightMatch bool
	Signals        []string
}

type ModerationCase struct {
	Name  string
	Asset MediaAsset
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "content-moderation", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range moderationQueue() {
		scores := runClassifiers(c.Asset)
		resp, err := svc.Evaluate(ctx, "content-moderation", condition.EvaluateRequest{Decision: "content_moderation", Input: moderationDecisionFacts(c.Asset, scores)})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Asset.ID, c.Name)
		fmt.Printf("  classifiers: nsfw=%.2f toxic=%.2f copyright=%v signals=%v\n", scores.NSFWScore, scores.ToxicityScore, scores.CopyrightMatch, scores.Signals)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		applyModeration(c.Asset, scores, decision.Effect)
	}
}

func moderationQueue() []ModerationCase {
	return []ModerationCase{
		{Name: "trusted tutorial", Asset: MediaAsset{ID: "post-1", Kind: "video", Caption: "How to make sourdough starter", Creator: Creator{ID: "creator-1", ReputationScore: 82}}},
		{Name: "borderline comment", Asset: MediaAsset{ID: "post-2", Kind: "comment", Caption: "This is stupid and you are all awful", Creator: Creator{ID: "creator-2", ReputationScore: 55, Strikes: 1}}},
		{Name: "copyright match", Asset: MediaAsset{ID: "post-3", Kind: "video", Caption: "Full match replay", AudioFP: "fp-known-league", Creator: Creator{ID: "creator-3", ReputationScore: 74}}},
	}
}

func runClassifiers(asset MediaAsset) ClassifierScores {
	text := strings.ToLower(asset.Caption)
	var signals []string
	nsfw := 0.05
	toxicity := 0.08
	if strings.Contains(text, "stupid") || strings.Contains(text, "awful") {
		toxicity += 0.58
		signals = append(signals, "abusive-language")
	}
	if asset.Creator.Strikes > 0 {
		toxicity += float64(asset.Creator.Strikes) * 0.05
		signals = append(signals, "creator-strike-history")
	}
	copyright := asset.AudioFP == "fp-known-league" || asset.ImageFP == "fp-known-studio"
	if copyright {
		signals = append(signals, "fingerprint-match")
	}
	sort.Strings(signals)
	return ClassifierScores{NSFWScore: round(nsfw), ToxicityScore: round(toxicity), CopyrightMatch: copyright, Signals: signals}
}

func moderationDecisionFacts(asset MediaAsset, scores ClassifierScores) map[string]any {
	return map[string]any{
		"classifier": map[string]any{
			"nsfw_score":      scores.NSFWScore,
			"toxicity_score":  scores.ToxicityScore,
			"copyright_match": scores.CopyrightMatch,
		},
		"creator": map[string]any{"id": asset.Creator.ID, "reputation_score": asset.Creator.ReputationScore},
		"content": map[string]any{"id": asset.ID, "kind": asset.Kind},
	}
}

func applyModeration(asset MediaAsset, scores ClassifierScores, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  publish %s and add to recommendation candidate set\n", asset.ID)
	case "require_review":
		fmt.Printf("  hold %s, show moderator signals=%v\n", asset.ID, scores.Signals)
	default:
		fmt.Printf("  remove %s, create creator strike and rights-holder audit\n", asset.ID)
	}
}

func round(v float64) float64 {
	return math.Round(v*100) / 100
}

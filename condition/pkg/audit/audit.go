package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type Envelope struct {
	ID               string         `json:"id"`
	Operation        string         `json:"operation"`
	Definition       string         `json:"definition,omitempty"`
	Version          string         `json:"version,omitempty"`
	Environment      string         `json:"environment,omitempty"`
	Subject          string         `json:"subject,omitempty"`
	DefinitionDigest string         `json:"definition_digest,omitempty"`
	RequestHash      string         `json:"request_hash,omitempty"`
	ResultHash       string         `json:"result_hash,omitempty"`
	StartedAt        time.Time      `json:"started_at"`
	CompletedAt      time.Time      `json:"completed_at"`
	DurationMS       int64          `json:"duration_ms"`
	TraceSummary     []string       `json:"trace_summary,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	PreviousHash     string         `json:"previous_hash,omitempty"`
	Hash             string         `json:"hash"`
}

func Fingerprint(v any) string {
	if v == nil {
		return ""
	}
	payload, _ := json.Marshal(v)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func DigestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func Seal(e Envelope, previous string) Envelope {
	e.PreviousHash = previous
	e.Hash = ""
	payload, _ := json.Marshal(e)
	sum := sha256.Sum256(payload)
	e.Hash = hex.EncodeToString(sum[:])
	return e
}

func VerifyChain(records []Envelope) error {
	var previous string
	for i, record := range records {
		want := record.Hash
		check := Seal(record, previous)
		if check.Hash != want {
			return fmt.Errorf("audit record %q at index %d has invalid hash", record.ID, i)
		}
		if record.PreviousHash != previous {
			return fmt.Errorf("audit record %q at index %d has invalid previous hash", record.ID, i)
		}
		previous = want
	}
	return nil
}

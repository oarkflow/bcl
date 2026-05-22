package audit

import (
	"testing"
	"time"
)

func TestVerifyChainDetectsTampering(t *testing.T) {
	first := Seal(Envelope{ID: "1", Operation: "publish", StartedAt: time.Now(), CompletedAt: time.Now()}, "")
	second := Seal(Envelope{ID: "2", Operation: "evaluate", StartedAt: time.Now(), CompletedAt: time.Now()}, first.Hash)
	if err := VerifyChain([]Envelope{first, second}); err != nil {
		t.Fatal(err)
	}
	second.Operation = "changed"
	if err := VerifyChain([]Envelope{first, second}); err == nil {
		t.Fatal("expected tampering to be detected")
	}
}

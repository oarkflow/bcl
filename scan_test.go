package bcl

import (
	"strings"
	"testing"
)

func TestScanValidAndInvalid(t *testing.T) {
	if err := Scan([]byte(`server { port env.int("PORT", 8080) }`)); err != nil {
		t.Fatal(err)
	}
	err := Scan([]byte(`server { port 8080 ]`))
	if err == nil {
		t.Fatal("expected scan error")
	}
	if !strings.Contains(err.Error(), "expected }") {
		t.Fatalf("unexpected error: %v", err)
	}
}

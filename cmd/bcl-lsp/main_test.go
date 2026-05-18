package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/oarkflow/bcl"
)

func TestLSPRangeConvertsOneBasedBCLSpans(t *testing.T) {
	got := lspRange(bcl.Span{Start: bcl.Position{Line: 2, Column: 3}, End: bcl.Position{Line: 2, Column: 8}})
	if got.Start.Line != 1 || got.Start.Character != 2 || got.End.Line != 1 || got.End.Character != 7 {
		t.Fatalf("unexpected range: %+v", got)
	}
}

func TestReadMessageParsesContentLengthFrame(t *testing.T) {
	body := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"shutdown\",\"params\":{}}"
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	msg, err := readMessage(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "shutdown" || msg.ID.(float64) != 1 {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestRespondIncludesNullResult(t *testing.T) {
	var out bytes.Buffer
	s := &server{out: &out}
	s.respond(float64(7), nil)

	raw := out.String()
	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("invalid frame: %q", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["result"]; !ok {
		t.Fatalf("expected explicit null result, got %s", parts[1])
	}
	if payload["error"] != nil {
		t.Fatalf("unexpected error field: %s", parts[1])
	}
}

package bcl

import "testing"

// Regression tests: the parser previously accepted a block missing its
// closing '}' (silently stopping at EOF) and an assignment like `name =`
// with no value (treating it as an empty reference). Both should now be
// parse errors.
func TestParseUnclosedBlockIsError(t *testing.T) {
	_, err := Parse([]byte("server \"api\" {\n  port = 8080\n"))
	if err == nil {
		t.Fatal("expected error for unclosed block, got nil")
	}
}

func TestParseMissingAssignmentValueIsError(t *testing.T) {
	_, err := Parse([]byte("name =\n"))
	if err == nil {
		t.Fatal("expected error for assignment with no value, got nil")
	}
}

func TestParseBareIdentifierStillAllowed(t *testing.T) {
	// A bare identifier with no '=' at all is unrelated to the missing-value
	// bug and must keep parsing as before.
	if _, err := Parse([]byte("foo\n")); err != nil {
		t.Fatalf("unexpected error for bare identifier: %v", err)
	}
}

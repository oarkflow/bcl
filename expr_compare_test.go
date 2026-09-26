package bcl

import "testing"

// Regression test: nil (missing/undefined) values must never compare as
// greater than a number. compare() previously fell back to string
// comparison of fmt.Sprint output for incomparable operands, which made
// nil > 1000 evaluate to true because "<nil>" > "1000" lexically.
func TestCompareNilNeverOrdered(t *testing.T) {
	cases := []struct {
		op       string
		a, b     any
		expected bool
	}{
		{">", nil, 1000, false},
		{">=", nil, 1000, false},
		{"<", nil, 1000, false},
		{"<=", nil, 1000, false},
		{">", "abc", 1000, false},
		{">", 1000, nil, false},
	}
	for _, c := range cases {
		got, err := evalOp(c.op, c.a, c.b)
		if err != nil {
			t.Fatalf("evalOp(%q, %v, %v) returned error: %v", c.op, c.a, c.b, err)
		}
		if got != c.expected {
			t.Errorf("evalOp(%q, %v, %v) = %v, want %v", c.op, c.a, c.b, got, c.expected)
		}
	}
}

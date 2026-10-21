package bcl

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestSimpleUnmarshalMatchesCompiler(t *testing.T) {
	type config struct {
		Name    string `bcl:"name"`
		Enabled bool   `bcl:"enabled"`
		Workers int    `bcl:"workers"`
	}
	cases := []string{
		"name \"ProcessGate\"\nenabled true\nworkers 8\n",
		"\n workers -8\r\nname \"hello world\"\r\nenabled false\n",
		"name \"a\\nb\"\nworkers 8\n",            // escaped string: compiler fallback
		"name \"${MISSING}\"\n",                  // interpolation: compiler fallback
		"name \"first\"\nname \"second\"\n",      // duplicate: compiler fallback
		"name \"first\"\nworkers nope\n",         // reference: compiler fallback
		"name \"first\"\nworkers 8 extra\n",      // syntax outside the fast subset
		"name \"first\"\nworkers 8\ninvalid {\n", // invalid document must not partially set fields
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			got := config{Name: "original", Workers: 5}
			want := got
			gotErr := Unmarshal([]byte(input), &got)
			wantErr := UnmarshalWithOptions([]byte(input), &want, &Options{AllowEnv: true})
			if (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("errors differ: fast=%v compiler=%v", gotErr, wantErr)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded values differ: fast=%+v compiler=%+v", got, want)
			}
		})
	}
}

func TestUnmarshalSimpleOwnsStringsAndRejectsUnsupportedInput(t *testing.T) {
	type config struct {
		Name    string `bcl:"name"`
		Workers int    `bcl:"workers"`
	}
	data := []byte("name \"ProcessGate\"\nworkers 8\n")
	var got config
	if err := UnmarshalSimple(data, &got); err != nil {
		t.Fatal(err)
	}
	data[7] = 'x'
	if got.Name != "ProcessGate" || got.Workers != 8 {
		t.Fatalf("decoded value changed with input: %+v", got)
	}
	before := got
	if err := UnmarshalSimple([]byte("name \"changed\"\nworkers env.int(\"N\", 8)\n"), &got); !errors.Is(err, ErrSimpleUnmarshalUnsupported) {
		t.Fatalf("unsupported input error = %v", err)
	}
	if got != before {
		t.Fatalf("unsupported input changed destination: %+v", got)
	}
}

func TestSimpleMarshalMatchesGeneric(t *testing.T) {
	type config struct {
		Name    string  `bcl:"name"`
		Enabled bool    `bcl:"enabled"`
		Workers int     `bcl:"workers,omitempty"`
		Ratio   float64 `bcl:"ratio"`
		Secret  string  `bcl:"secret,sensitive"`
	}
	for _, input := range []config{
		{Name: "ProcessGate", Enabled: true, Workers: 8, Ratio: 0.25, Secret: "hidden"},
		{Name: "precision", Ratio: 1.23456789},
		{Name: "tab\there", Ratio: -1.5},
		{Name: "has \"quote\"", Workers: -8},
		{Name: "two\nlines"},
	} {
		got, err := Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := writeGoValue(&b, reflect.ValueOf(input), 0, ""); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, b.Bytes()) {
			t.Fatalf("marshal mismatch for %+v: got %q, want %q", input, got, b.Bytes())
		}
		if input.Name == "precision" && !bytes.Contains(got, []byte("ratio 1.23457\n")) {
			t.Fatalf("float format changed: %q", got)
		}
	}
	type empty struct {
		Name string `bcl:"name,omitempty"`
	}
	data, err := Marshal(empty{})
	if err != nil || data != nil {
		t.Fatalf("omitted-only struct = %q, %v; want nil bytes", data, err)
	}
}

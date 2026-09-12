package bcl

import (
	"os"
	"reflect"
	"testing"
)

func TestFoldingRangesCoverBlocksCommentsAndImports(t *testing.T) {
	src := []byte(`import "a.bcl"
import "b.bcl"

# a note
# continued

server {
  address ":8080"
  tls {
    enabled true
  }
}

inline { a 1 }
`)
	got := FoldingRanges(src)
	want := []FoldRange{
		{Start: 4, End: 5, Kind: FoldComment},
		{Start: 9, End: 10, Kind: FoldRegion},
		{Start: 7, End: 11, Kind: FoldRegion},
		{Start: 1, End: 2, Kind: FoldImports},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folding ranges =\n%#v\nwant\n%#v", got, want)
	}
}

func TestFoldingRangesIgnoreSingleLineAndTrailingComments(t *testing.T) {
	src := []byte("a { b 1 } # trailing\nc 2 # another\n")
	if got := FoldingRanges(src); len(got) != 0 {
		t.Fatalf("expected nothing foldable, got %#v", got)
	}
}

// An editor asks for folds while the document is still being typed, so an
// unclosed block must not cost the folds around it or crash.
func TestFoldingRangesHandleIncompleteDocuments(t *testing.T) {
	for _, src := range []string{
		"a {\n  b 1\n",
		"a {\n  b \"unterminated\n}\n",
		"}\n}\n",
		"",
	} {
		FoldingRanges([]byte(src))
	}
}

func TestFoldingRangesOnRealPipeline(t *testing.T) {
	src, err := os.ReadFile(benchFixture)
	if err != nil {
		t.Skip(err)
	}
	got := FoldingRanges(src)
	if len(got) < 40 {
		t.Fatalf("expected a large pipeline to be richly foldable, got %d ranges", len(got))
	}
	for _, r := range got {
		if r.Start >= r.End || r.Start < 1 {
			t.Fatalf("invalid range %#v", r)
		}
	}
}

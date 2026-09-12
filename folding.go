package bcl

// FoldKind describes why a range can be folded, matching the kinds an editor
// understands.
type FoldKind string

const (
	// FoldRegion is a block, list, or call that spans lines.
	FoldRegion FoldKind = "region"
	// FoldComment is a run of consecutive comment lines.
	FoldComment FoldKind = "comment"
	// FoldImports is the run of import declarations at the top of a document.
	FoldImports FoldKind = "imports"
)

// FoldRange is one foldable region, with 1-based inclusive line numbers.
type FoldRange struct {
	Start int      `json:"start"`
	End   int      `json:"end"`
	Kind  FoldKind `json:"kind"`
}

// FoldingRanges reports the regions an editor can collapse: every multi-line
// bracket pair, every run of comment lines, and the leading import block. It
// works lexically, so it folds a document correctly while it is still being
// typed and the parser would reject it.
func FoldingRanges(src []byte) []FoldRange {
	toks, err := scanFormatTokens(string(src))
	if err != nil {
		// A broken literal further down should not cost the folds above it, and the
		// scanner returns what it read before failing.
		if len(toks) == 0 {
			return nil
		}
	}
	out := make([]FoldRange, 0, 16)
	var open []int // line each unclosed bracket was opened on

	commentStart, commentEnd := 0, 0
	importStart, importEnd := 0, 0
	flushComment := func() {
		if commentStart > 0 && commentEnd > commentStart {
			out = append(out, FoldRange{Start: commentStart, End: commentEnd, Kind: FoldComment})
		}
		commentStart, commentEnd = 0, 0
	}

	for i, tok := range toks {
		switch {
		case tok.kind == fmtLineComment:
			// Only a comment that owns its line starts a run; a trailing comment
			// belongs to the code before it.
			if i > 0 && toks[i-1].endLine == tok.line {
				flushComment()
				continue
			}
			if commentStart != 0 && tok.line == commentEnd+1 {
				commentEnd = tok.line
			} else {
				flushComment()
				commentStart, commentEnd = tok.line, tok.line
			}
			continue
		case tok.kind == fmtBlockComment:
			flushComment()
			if tok.endLine > tok.line {
				out = append(out, FoldRange{Start: tok.line, End: tok.endLine, Kind: FoldComment})
			}
			continue
		case tok.kind == fmtHeredoc:
			flushComment()
			if tok.endLine > tok.line {
				out = append(out, FoldRange{Start: tok.line, End: tok.endLine, Kind: FoldRegion})
			}
			continue
		}

		flushComment()

		if tok.kind == fmtIdent && tok.text == "import" {
			if importStart == 0 {
				importStart = tok.line
			}
			importEnd = tok.line
		}

		switch {
		case isFmtOpener(tok):
			open = append(open, tok.line)
		case isFmtCloser(tok):
			if len(open) == 0 {
				continue
			}
			start := open[len(open)-1]
			open = open[:len(open)-1]
			// Fold up to the line before the closer so the closing bracket stays
			// visible, which is what makes a collapsed block readable.
			if tok.line > start+1 {
				out = append(out, FoldRange{Start: start, End: tok.line - 1, Kind: FoldRegion})
			}
		}
	}
	flushComment()
	if importEnd > importStart {
		out = append(out, FoldRange{Start: importStart, End: importEnd, Kind: FoldImports})
	}
	return out
}

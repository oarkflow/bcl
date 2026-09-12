package bcl

import (
	"bytes"
	"fmt"
	"strings"
)

// FormatOptions controls how Format renders BCL source.
type FormatOptions struct {
	// IndentWidth is the number of spaces per nesting level. Zero means 2.
	IndentWidth int
	// UseTabs indents with one tab per nesting level instead of spaces.
	UseTabs bool
	// KeepBlankLines keeps every blank line the author wrote instead of
	// collapsing runs of blank lines to one and trimming the blank lines that
	// hug a block's braces.
	KeepBlankLines bool
}

func (o FormatOptions) indentUnit() string {
	if o.UseTabs {
		return "\t"
	}
	width := o.IndentWidth
	if width <= 0 {
		width = 2
	}
	return strings.Repeat(" ", width)
}

// FormattedLine is one rendered output line together with the 1-based source
// line span it was produced from, so callers (the language server's range
// formatting, for example) can format part of a document.
type FormattedLine struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type fmtTokKind uint8

const (
	fmtIdent fmtTokKind = iota
	fmtString
	fmtNumber
	fmtPunct
	fmtLineComment
	fmtBlockComment
	fmtHeredoc
)

// fmtTok is one lexical token carrying its exact source text, so formatting
// only ever changes the whitespace between tokens.
type fmtTok struct {
	kind    fmtTokKind
	text    string
	line    int
	endLine int
	// adjacent reports that no whitespace separated this token from the previous
	// one in the source. It keeps ambiguous punctuation such as the "<" of
	// list<object> glued together instead of being spaced like a comparison.
	adjacent bool
}

// FormatLines formats src and returns the rendered lines with their source
// spans. Declaration order, token text, and comments are preserved; only the
// whitespace between tokens changes.
func FormatLines(src []byte, opts FormatOptions) ([]FormattedLine, error) {
	toks, err := scanFormatTokens(string(src))
	if err != nil {
		return nil, err
	}
	unit := opts.indentUnit()
	lines := make([]FormattedLine, 0, len(toks)/3+8)
	var stack []int
	indent := 0
	prevEndLine := 0
	prevLineOpens := false
	for i := 0; i < len(toks); {
		j := i + 1
		for j < len(toks) && toks[j].line == toks[j-1].endLine {
			j++
		}
		line := toks[i:j]
		i = j

		blanks := 0
		if prevEndLine > 0 {
			blanks = line[0].line - prevEndLine - 1
		}

		// Leading closers unwind to the indent of the line that opened them, so
		// a "] }" line lands beside its opener instead of one level in.
		lineIndent := indent
		k := 0
		for k < len(line) && isFmtCloser(line[k]) {
			lineIndent = popIndent(&stack)
			k++
		}
		indent = lineIndent
		for ; k < len(line); k++ {
			switch {
			case isFmtOpener(line[k]):
				// Every group opened on this line shares the line's own indent,
				// so "lookup "x" { options = [" keeps its body one level in.
				stack = append(stack, lineIndent)
				indent = lineIndent + 1
			case isFmtCloser(line[k]):
				indent = popIndent(&stack)
			}
		}

		if blanks > 0 && len(lines) > 0 {
			keep := blanks
			if !opts.KeepBlankLines {
				keep = 1
				if prevLineOpens || isFmtCloser(line[0]) {
					keep = 0
				}
			}
			for n := 0; n < keep; n++ {
				lines = append(lines, FormattedLine{})
			}
		}
		lines = append(lines, FormattedLine{
			Text:  strings.Repeat(unit, lineIndent) + joinFmtTokens(line),
			Start: line[0].line,
			End:   line[len(line)-1].endLine,
		})
		prevEndLine = line[len(line)-1].endLine
		prevLineOpens = isFmtOpener(line[len(line)-1])
	}
	attachBlankLineSpans(lines)
	return lines, nil
}

// attachBlankLineSpans gives each blank separator the source line of the code
// line it precedes, so a range selection that covers that code line also covers
// the blank line printed above it.
func attachBlankLineSpans(lines []FormattedLine) {
	for idx := range lines {
		if lines[idx].Start != 0 {
			continue
		}
		for n := idx + 1; n < len(lines); n++ {
			if lines[n].Start != 0 {
				lines[idx].Start, lines[idx].End = lines[n].Start, lines[n].Start
				break
			}
		}
	}
}

func popIndent(stack *[]int) int {
	s := *stack
	if len(s) == 0 {
		return 0
	}
	top := s[len(s)-1]
	*stack = s[:len(s)-1]
	return top
}

func joinFmtTokens(line []fmtTok) string {
	var b strings.Builder
	for idx, t := range line {
		if idx > 0 && (fmtNeedsSpace(line[idx-1], t) || fmtTokenWouldMerge(b.String(), t)) {
			b.WriteByte(' ')
		}
		b.WriteString(t.text)
	}
	return b.String()
}

// fmtTokenWouldMerge reports whether appending cur directly to what has been
// written so far would re-lex as something else: "!" then "==" running together
// as "!==", three "." tokens collapsing into the "..." operator, or an identifier
// ending in "/" absorbing a following "/" as a comment. Formatting must never
// change the token stream, so such a token keeps its space whatever the style
// rules say.
//
// The check looks at the written text rather than just the previous token because
// merging is not always pairwise - "." + "." is two tokens, but a third one makes
// "...". Only self-delimiting kinds (strings, comments, heredocs) are exempt:
// they cannot absorb a neighbour.
func fmtTokenWouldMerge(written string, cur fmtTok) bool {
	if !isFmtMergeable(cur) {
		return false
	}
	suffix := fmtMergeSuffix(written)
	if suffix == "" {
		return false
	}
	before, errBefore := scanFormatTokens(suffix)
	after, errAfter := scanFormatTokens(suffix + cur.text)
	if errBefore != nil || errAfter != nil {
		return true
	}
	if len(after) != len(before)+1 {
		return true
	}
	for i := range before {
		if after[i].text != before[i].text {
			return true
		}
	}
	return after[len(after)-1].text != cur.text
}

// maxMergeSuffix bounds how far back the merge check looks. Only characters that
// could join the next token matter, and a truncated run still reports a merge, so
// a cap keeps long single-line lists linear.
const maxMergeSuffix = 32

// fmtMergeSuffix returns the trailing run of characters that could lex as part of
// one token together with whatever comes next. It stops at whitespace and at
// string delimiters, so the returned suffix always scans cleanly on its own.
func fmtMergeSuffix(written string) string {
	i := len(written)
	limit := max(0, len(written)-maxMergeSuffix)
	for i > limit && isFmtTokenByte(written[i-1]) {
		i--
	}
	return written[i:]
}

// isFmtTokenByte reports whether a byte can appear inside a non-delimited token
// (identifier, number, or operator) as opposed to separating one.
func isFmtTokenByte(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '"', '\'', '`':
		return false
	}
	return true
}

func isFmtMergeable(t fmtTok) bool {
	switch t.kind {
	case fmtPunct, fmtIdent, fmtNumber:
		return true
	}
	return false
}

func fmtNeedsSpace(prev, cur fmtTok) bool {
	if cur.kind == fmtLineComment || cur.kind == fmtBlockComment || prev.kind == fmtBlockComment {
		return true
	}
	// "<" and ">" are both comparisons and generic-type brackets (list<object>),
	// so keep whatever the author wrote rather than guessing.
	if isFmtAngle(cur) || isFmtAngle(prev) {
		return !cur.adjacent
	}
	if prev.kind == fmtPunct {
		switch prev.text {
		case "(", "[", ".", "!", "...", "&":
			// Prefix and opening punctuation hugs what follows it.
			return false
		case "{":
			// Inline blocks and object literals keep the author's padding, so both
			// `{ a "b" }` and `{a: "b"}` survive a format pass unchanged.
			return !cur.adjacent
		}
	}
	if cur.kind == fmtPunct {
		switch cur.text {
		case ",", ")", "]", ".":
			return false
		case "}":
			return !cur.adjacent
		case "(":
			// A call hugs its callee: uuid(), lower(m), env("HOME").
			return !(prev.kind == fmtIdent || prev.kind == fmtPunct && (prev.text == ")" || prev.text == "]"))
		case "[":
			// "key ["a"]" (a list value) and "tail[0]" (an index) are spelled the
			// same way here, so follow the source.
			switch {
			case prev.kind == fmtIdent, prev.kind == fmtString, prev.kind == fmtNumber,
				prev.kind == fmtPunct && (prev.text == ")" || prev.text == "]"):
				return !cur.adjacent
			}
			return true
		}
	}
	return true
}

func isFmtAngle(t fmtTok) bool {
	return t.kind == fmtPunct && (t.text == "<" || t.text == ">")
}

func isFmtOpener(t fmtTok) bool {
	return t.kind == fmtPunct && (t.text == "{" || t.text == "[" || t.text == "(")
}

func isFmtCloser(t fmtTok) bool {
	return t.kind == fmtPunct && (t.text == "}" || t.text == "]" || t.text == ")")
}

// fmtOperators are the multi-character operators the parser reassembles from
// single characters; the formatter keeps them intact so they are never split by
// a space. Longest first.
var fmtOperators = []string{"...", "==", "!=", "<=", ">=", "&&", "||", "=>", "->", "??", "+=", "-=", "*=", "/=", "%="}

// scanFormatTokens tokenizes src the way the parser's lexer does, except that
// comments are kept as tokens instead of skipped.
func scanFormatTokens(src string) ([]fmtTok, error) {
	toks := make([]fmtTok, 0, len(src)/5+8)
	line := 1
	prevEnd := -1
	add := func(kind fmtTokKind, text string, startLine, start, end int) {
		toks = append(toks, fmtTok{
			kind:     kind,
			text:     text,
			line:     startLine,
			endLine:  startLine + strings.Count(text, "\n"),
			adjacent: start == prevEnd,
		})
		line = startLine + strings.Count(text, "\n")
		prevEnd = end
	}
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		start := i
		startLine := line
		switch {
		case c == '#', c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			add(fmtLineComment, strings.TrimRight(src[start:i], " \t\r"), startLine, start, i)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("bcl: unterminated block comment on line %d", startLine)
			}
			i = i + 2 + end + 2
			add(fmtBlockComment, src[start:i], startLine, start, i)
		case strings.HasPrefix(src[i:], `"""`):
			end := strings.Index(src[i+3:], `"""`)
			if end < 0 {
				return nil, fmt.Errorf("bcl: unterminated multiline string on line %d", startLine)
			}
			i = i + 3 + end + 3
			add(fmtString, src[start:i], startLine, start, i)
		case c == '"' || c == '\'':
			i++
			for {
				if i >= len(src) || src[i] == '\n' {
					return nil, fmt.Errorf("bcl: unterminated string on line %d", startLine)
				}
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == c {
					i++
					break
				}
				i++
			}
			add(fmtString, src[start:i], startLine, start, i)
		case c == '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return nil, fmt.Errorf("bcl: unterminated raw string on line %d", startLine)
			}
			i = i + 1 + end + 1
			add(fmtString, src[start:i], startLine, start, i)
		case c == '<' && i+1 < len(src) && src[i+1] == '<':
			text, err := scanFormatHeredoc(src, i, startLine)
			if err != nil {
				return nil, err
			}
			i += len(text)
			add(fmtHeredoc, text, startLine, start, i)
		case isFmtIdentStart(c):
			for i < len(src) && isFmtIdentPart(src[i]) {
				i++
			}
			add(fmtIdent, src[start:i], startLine, start, i)
		case isFmtDigit(c), c == '-' && i+1 < len(src) && isFmtDigit(src[i+1]):
			i++
			for i < len(src) && isFmtNumberPart(src[i]) {
				i++
			}
			add(fmtNumber, src[start:i], startLine, start, i)
		default:
			matched := false
			for _, op := range fmtOperators {
				if strings.HasPrefix(src[i:], op) {
					i += len(op)
					add(fmtPunct, op, startLine, start, i)
					matched = true
					break
				}
			}
			if !matched {
				i++
				add(fmtPunct, src[start:i], startLine, start, i)
			}
		}
	}
	return toks, nil
}

func scanFormatHeredoc(src string, i, startLine int) (string, error) {
	nl := strings.IndexByte(src[i:], '\n')
	if nl < 0 {
		return "", fmt.Errorf("bcl: unterminated heredoc on line %d", startLine)
	}
	marker := strings.TrimSpace(src[i+2 : i+nl])
	if marker == "" {
		return "", fmt.Errorf("bcl: missing heredoc marker on line %d", startLine)
	}
	end := strings.Index(src[i+nl:], "\n"+marker)
	if end < 0 {
		return "", fmt.Errorf("bcl: unterminated heredoc %q on line %d", marker, startLine)
	}
	return src[i : i+nl+end+1+len(marker)], nil
}

func isFmtIdentStart(c byte) bool {
	return c == '_' || c == '*' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isFmtIdentPart(c byte) bool {
	return isFmtIdentStart(c) || isFmtDigit(c) || c == ':' || c == '-' || c == '/'
}

func isFmtDigit(c byte) bool { return c >= '0' && c <= '9' }

func isFmtNumberPart(c byte) bool {
	return isFmtDigit(c) || c == '.' || c == '-' || c == ':' ||
		c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func renderFormattedLines(lines []FormattedLine, eol string, capacity int) []byte {
	var b bytes.Buffer
	if capacity > 0 {
		b.Grow(capacity)
	}
	for _, ln := range lines {
		b.WriteString(ln.Text)
		b.WriteString(eol)
	}
	return b.Bytes()
}

// LineEnding reports the line ending src already uses, so formatting a CRLF
// document does not leave it with mixed endings.
func LineEnding(src []byte) string {
	if bytes.Contains(src, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

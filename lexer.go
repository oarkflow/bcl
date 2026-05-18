package bcl

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokNumber
	tokLBrace
	tokRBrace
	tokLBracket
	tokRBracket
	tokLParen
	tokRParen
	tokComma
	tokDot
	tokEqual
	tokNewline
	tokOperator
	tokHeredoc
)

type token struct {
	kind tokenKind
	text string
	span Span
}

type lexer struct {
	file string
	src  string
	pos  int
	line int
	col  int
}

func lex(file string, src []byte) ([]token, ErrorList) {
	return lexString(file, string(src))
}

func lexString(file string, src string) ([]token, ErrorList) {
	return lexStringInto(file, src, make([]token, 0, estimatedTokenCount(len(src))))
}

func lexStringPooled(file string, src string) ([]token, ErrorList) {
	return lexStringInto(file, src, getTokenScratch(estimatedTokenCount(len(src))))
}

func lexStringInto(file string, src string, toks []token) ([]token, ErrorList) {
	l := &lexer{file: file, src: src, line: 1, col: 1}
	var errs ErrorList
	for {
		t, err := l.next()
		if err != nil {
			errs = append(errs, *err)
		}
		toks = append(toks, t)
		if t.kind == tokEOF {
			break
		}
	}
	if len(errs) > 0 {
		return toks, errs
	}
	return toks, nil
}

var tokenScratchPool sync.Pool

func getTokenScratch(capHint int) []token {
	if v := tokenScratchPool.Get(); v != nil {
		toks := v.([]token)
		if cap(toks) >= capHint && cap(toks) <= capHint*8+64 {
			return toks[:0]
		}
	}
	return make([]token, 0, capHint)
}

func putTokenScratch(toks []token) {
	if cap(toks) == 0 || cap(toks) > 4096 {
		return
	}
	clear(toks)
	tokenScratchPool.Put(toks[:0])
}

func estimatedTokenCount(n int) int {
	if n <= 0 {
		return 1
	}
	est := n / 4
	if n < 256 {
		est = n / 8
	}
	if est < 8 {
		est = 8
	}
	return est
}

func (l *lexer) next() (token, *Diagnostic) {
	for {
		r := l.peek()
		switch {
		case r == 0:
			return token{kind: tokEOF, span: l.spanAt()}, nil
		case r == ' ' || r == '\t' || r == '\r':
			l.advance()
		case r == '\n':
			sp := l.spanAt()
			l.advance()
			sp.End = l.posn()
			return token{kind: tokNewline, text: "\n", span: sp}, nil
		case r == '#':
			l.skipLine()
		case r == '/' && l.peekN(1) == '/':
			l.skipLine()
		case r == '/' && l.peekN(1) == '*':
			if err := l.skipBlockComment(); err != nil {
				return token{kind: tokEOF, span: l.spanAt()}, err
			}
		default:
			goto done
		}
	}
done:
	start := l.spanAt()
	r := l.peek()
	switch r {
	case '{':
		l.advance()
		return l.tok(tokLBrace, "{", start), nil
	case '}':
		l.advance()
		return l.tok(tokRBrace, "}", start), nil
	case '[':
		l.advance()
		return l.tok(tokLBracket, "[", start), nil
	case ']':
		l.advance()
		return l.tok(tokRBracket, "]", start), nil
	case '(':
		l.advance()
		return l.tok(tokLParen, "(", start), nil
	case ')':
		l.advance()
		return l.tok(tokRParen, ")", start), nil
	case ',':
		l.advance()
		return l.tok(tokComma, ",", start), nil
	case '.':
		l.advance()
		return l.tok(tokDot, ".", start), nil
	case '=':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return l.tok(tokOperator, "==", start), nil
		}
		return l.tok(tokEqual, "=", start), nil
	case '<':
		if l.peekN(1) == '<' {
			return l.heredoc(start)
		}
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return l.tok(tokOperator, "<=", start), nil
		}
		return l.tok(tokOperator, "<", start), nil
	case '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return l.tok(tokOperator, "!=", start), nil
		}
		return l.tok(tokOperator, "!", start), nil
	case '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return l.tok(tokOperator, ">=", start), nil
		}
		return l.tok(tokOperator, ">", start), nil
	case '"', '\'':
		return l.string(start, r)
	case '`':
		return l.rawString(start)
	}
	if isIdentStart(r) || r == '*' {
		return l.ident(start), nil
	}
	if unicode.IsDigit(r) || (r == '-' && unicode.IsDigit(l.peekN(1))) {
		return l.number(start), nil
	}
	l.advance()
	return l.tok(tokOperator, string(r), start), nil
}

func (l *lexer) tok(k tokenKind, text string, start Span) token {
	start.End = l.posn()
	return token{kind: k, text: text, span: start}
}

func (l *lexer) ident(start Span) token {
	startOff := start.Start.Offset
	for {
		r := l.peek()
		if r == 0 || !(isIdentPart(r) || r == '*' || r == ':' || r == '-' || r == '/') {
			break
		}
		l.advance()
	}
	return l.tok(tokIdent, l.src[startOff:l.pos], start)
}

func (l *lexer) number(start Span) token {
	startOff := start.Start.Offset
	if l.peek() == '-' {
		l.advance()
	}
	for {
		r := l.peek()
		if r == 0 || !(unicode.IsDigit(r) || r == '.' || unicode.IsLetter(r) || r == '-' || r == ':' || r == 'T' || r == 'Z') {
			break
		}
		l.advance()
	}
	return l.tok(tokNumber, l.src[startOff:l.pos], start)
}

func (l *lexer) string(start Span, quote rune) (token, *Diagnostic) {
	if quote == '"' && strings.HasPrefix(l.src[l.pos:], `"""`) {
		l.advance()
		l.advance()
		l.advance()
		i := strings.Index(l.src[l.pos:], `"""`)
		if i < 0 {
			return token{kind: tokString, span: start}, &Diagnostic{Severity: "error", Message: "unterminated multiline string", Span: start}
		}
		text := l.src[l.pos : l.pos+i]
		for range text {
			l.advance()
		}
		l.advance()
		l.advance()
		l.advance()
		return l.tok(tokString, text, start), nil
	}
	l.advance()
	contentStart := l.pos
	var b strings.Builder
	decoded := false
	for {
		r := l.peek()
		if r == 0 || r == '\n' {
			return token{kind: tokString, span: start}, &Diagnostic{Severity: "error", Message: "unterminated string", Span: start}
		}
		l.advance()
		if r == quote {
			if !decoded {
				return l.tok(tokString, l.src[contentStart:l.pos-1], start), nil
			}
			return l.tok(tokString, b.String(), start), nil
		}
		if r == '\\' {
			if !decoded && l.pos-1 > contentStart {
				b.WriteString(l.src[contentStart : l.pos-1])
			}
			decoded = true
			esc := l.peek()
			l.advance()
			switch esc {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"', '\'', '\\':
				b.WriteRune(esc)
			default:
				b.WriteRune(esc)
			}
			continue
		}
		if decoded {
			b.WriteRune(r)
		}
	}
}

func (l *lexer) rawString(start Span) (token, *Diagnostic) {
	l.advance()
	contentStart := l.pos
	for {
		r := l.peek()
		if r == 0 {
			return token{kind: tokString, span: start}, &Diagnostic{Severity: "error", Message: "unterminated raw string", Span: start}
		}
		l.advance()
		if r == '`' {
			return l.tok(tokString, l.src[contentStart:l.pos-1], start), nil
		}
	}
}

func (l *lexer) heredoc(start Span) (token, *Diagnostic) {
	l.advance()
	l.advance()
	for l.peek() == ' ' || l.peek() == '\t' {
		l.advance()
	}
	var marker strings.Builder
	for r := l.peek(); r != 0 && r != '\n'; r = l.peek() {
		marker.WriteRune(r)
		l.advance()
	}
	if l.peek() == '\n' {
		l.advance()
	}
	end := marker.String()
	if end == "" {
		return token{kind: tokHeredoc, span: start}, &Diagnostic{Severity: "error", Message: "missing heredoc marker", Span: start}
	}
	search := "\n" + end
	i := strings.Index(l.src[l.pos:], search)
	if i < 0 {
		return token{kind: tokHeredoc, span: start}, &Diagnostic{Severity: "error", Message: fmt.Sprintf("unterminated heredoc %q", end), Span: start}
	}
	text := l.src[l.pos : l.pos+i]
	for range text {
		l.advance()
	}
	l.advance()
	for range end {
		l.advance()
	}
	return l.tok(tokHeredoc, text, start), nil
}

func (l *lexer) skipLine() {
	for r := l.peek(); r != 0 && r != '\n'; r = l.peek() {
		l.advance()
	}
}

func (l *lexer) skipBlockComment() *Diagnostic {
	start := l.spanAt()
	l.advance()
	l.advance()
	for {
		if l.peek() == 0 {
			return &Diagnostic{Severity: "error", Message: "unterminated block comment", Span: start}
		}
		if l.peek() == '*' && l.peekN(1) == '/' {
			l.advance()
			l.advance()
			return nil
		}
		l.advance()
	}
}

func (l *lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	return r
}

func (l *lexer) peekN(n int) rune {
	p := l.pos
	for i := 0; i < n && p < len(l.src); i++ {
		_, sz := utf8.DecodeRuneInString(l.src[p:])
		p += sz
	}
	if p >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[p:])
	return r
}

func (l *lexer) advance() rune {
	r, sz := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += sz
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

func (l *lexer) spanAt() Span {
	return Span{File: l.file, Start: l.posn(), End: l.posn()}
}

func (l *lexer) posn() Position {
	return Position{Line: l.line, Column: l.col, Offset: l.pos}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

package bcl

import "unsafe"

// Scan checks BCL lexical structure without materializing an AST.
//
// It is intended for hot paths that need a fast syntax gate before deciding
// whether to run the full AST parser. Valid input performs no heap allocation
// on the common path.
func Scan(src []byte) error {
	return ScanFile("<input>", src)
}

func ScanFile(name string, src []byte) error {
	return ScanString(name, unsafeString(src))
}

func ScanString(name, src string) error {
	l := lexer{file: name, src: src, line: 1, col: 1}
	var stack [64]tokenKind
	sp := 0
	for {
		tok, err := l.next()
		if err != nil {
			return ErrorList{*err}
		}
		switch tok.kind {
		case tokEOF:
			if sp != 0 {
				return ErrorList{{Severity: "error", Message: "unclosed " + tokenName(stack[sp-1]), Span: tok.span}}
			}
			return nil
		case tokLBrace, tokLBracket, tokLParen:
			if sp >= len(stack) {
				return scanDeep(name, src, tok, stack[:], sp)
			}
			stack[sp] = tok.kind
			sp++
		case tokRBrace, tokRBracket, tokRParen:
			if sp == 0 {
				return ErrorList{{Severity: "error", Message: "unexpected " + tokenName(tok.kind), Span: tok.span}}
			}
			open := stack[sp-1]
			if !tokensMatch(open, tok.kind) {
				return ErrorList{{Severity: "error", Message: "expected " + tokenName(closeToken(open)) + ", got " + tokenName(tok.kind), Span: tok.span}}
			}
			sp--
		}
	}
}

func scanDeep(name, src string, first token, prefix []tokenKind, sp int) error {
	l := lexer{file: name, src: src, pos: first.span.End.Offset, line: first.span.End.Line, col: first.span.End.Column}
	stack := append([]tokenKind(nil), prefix[:sp]...)
	stack = append(stack, first.kind)
	for {
		tok, err := l.next()
		if err != nil {
			return ErrorList{*err}
		}
		switch tok.kind {
		case tokEOF:
			if len(stack) != 0 {
				return ErrorList{{Severity: "error", Message: "unclosed " + tokenName(stack[len(stack)-1]), Span: tok.span}}
			}
			return nil
		case tokLBrace, tokLBracket, tokLParen:
			stack = append(stack, tok.kind)
		case tokRBrace, tokRBracket, tokRParen:
			if len(stack) == 0 {
				return ErrorList{{Severity: "error", Message: "unexpected " + tokenName(tok.kind), Span: tok.span}}
			}
			open := stack[len(stack)-1]
			if !tokensMatch(open, tok.kind) {
				return ErrorList{{Severity: "error", Message: "expected " + tokenName(closeToken(open)) + ", got " + tokenName(tok.kind), Span: tok.span}}
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func unsafeString(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(src), len(src))
}

func tokensMatch(open, close tokenKind) bool {
	return closeToken(open) == close
}

func closeToken(open tokenKind) tokenKind {
	switch open {
	case tokLBrace:
		return tokRBrace
	case tokLBracket:
		return tokRBracket
	case tokLParen:
		return tokRParen
	default:
		return tokEOF
	}
}

func tokenName(kind tokenKind) string {
	switch kind {
	case tokLBrace:
		return "{"
	case tokRBrace:
		return "}"
	case tokLBracket:
		return "["
	case tokRBracket:
		return "]"
	case tokLParen:
		return "("
	case tokRParen:
		return ")"
	default:
		return "token"
	}
}

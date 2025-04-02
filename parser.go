package bcl

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
)

type ParserConfig struct {
	Filename   string
	Whitespace uint64
	Mode       uint
}

type Parser struct {
	scanner  scanner.Scanner
	curr     tokenInfo
	input    string
	offset   int
	lastLine int
}

func NewParser(input string) *Parser {
	var s scanner.Scanner
	s.Init(strings.NewReader(input))
	s.Filename = "input.bcl"
	s.Whitespace = 1<<' ' | 1<<'\t' | 1<<'\r' | 1<<'\n'
	s.Mode |= scanner.ScanComments
	p := &Parser{
		scanner:  s,
		input:    input,
		offset:   0,
		lastLine: 1,
	}
	p.nextToken()
	return p
}

func NewParserWithConfig(input string, cfg ParserConfig) *Parser {
	var s scanner.Scanner
	s.Init(strings.NewReader(input))
	s.Filename = cfg.Filename
	s.Whitespace = cfg.Whitespace
	s.Mode = cfg.Mode
	p := &Parser{
		scanner:  s,
		input:    input,
		offset:   0,
		lastLine: 1,
	}
	p.nextToken()
	return p
}

func (p *Parser) nextToken() {
	r := p.scanner.Scan()
	text := p.scanner.TokenText()
	p.offset = int(p.scanner.Pos().Offset)
	switch text {
	case "&", "|", "^":
		p.curr = tokenInfo{typ: OPERATOR, value: text}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if text == "<" && p.scanner.Peek() == '<' {
		p.scanner.Next()
		p.curr = tokenInfo{typ: OPERATOR, value: "<<"}
		p.offset = int(p.scanner.Pos().Offset)
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if len(text) > 0 && text[0] == '\'' {
		if len(text) >= 2 && text[len(text)-1] == '\'' {
			inner := text[1 : len(text)-1]
			inner = strings.ReplaceAll(inner, "\\'", "'")
			p.curr = tokenInfo{typ: STRING, value: inner}
			p.lastLine = p.scanner.Pos().Line
			return
		}
		p.curr = tokenInfo{typ: STRING, value: text}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if len(text) > 0 && text[0] == '`' {
		if len(text) >= 2 && text[len(text)-1] == '`' {
			p.curr = tokenInfo{typ: STRING, value: text[1 : len(text)-1]}
			p.lastLine = p.scanner.Pos().Line
			return
		}
		p.curr = tokenInfo{typ: STRING, value: text}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if r == scanner.Comment {
		p.curr = tokenInfo{typ: COMMENT, value: text}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if r == '#' {
		var b strings.Builder
		b.WriteString("#")
		for ch := p.scanner.Peek(); ch != '\n' && ch != scanner.EOF; ch = p.scanner.Peek() {
			b.WriteByte(byte(p.scanner.Next()))
		}
		p.curr = tokenInfo{typ: COMMENT, value: b.String()}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	switch r {
	case scanner.EOF:
		p.curr = tokenInfo{typ: EOF, value: ""}
	case scanner.Ident:
		upper := strings.ToUpper(text)
		if upper == "IF" || upper == "ELSEIF" || upper == "ELSE" {
			p.curr = tokenInfo{typ: KEYWORD, value: upper}
		} else if text == "true" || text == "false" {
			p.curr = tokenInfo{typ: BOOL, value: text}
		} else {
			p.curr = tokenInfo{typ: IDENT, value: text}
		}
	case scanner.String:
		unquoted, err := strconv.Unquote(text)
		if err != nil {
			unquoted = text
		}
		p.curr = tokenInfo{typ: STRING, value: unquoted}
	case scanner.Int, scanner.Float:
		p.curr = tokenInfo{typ: NUMBER, value: text}
	default:
		switch text {
		case "=":
			// Check for equality operator "=="
			if p.scanner.Peek() == '=' {
				p.scanner.Next()
				p.curr = tokenInfo{typ: OPERATOR, value: "=="}
			} else {
				p.curr = tokenInfo{typ: ASSIGN, value: "="}
			}
		case "{":
			p.curr = tokenInfo{typ: LBRACE, value: text}
		case "}":
			p.curr = tokenInfo{typ: RBRACE, value: text}
		case "[":
			p.curr = tokenInfo{typ: LBRACKET, value: text}
		case "]":
			p.curr = tokenInfo{typ: RBRACKET, value: text}
		case "(":
			p.curr = tokenInfo{typ: LPAREN, value: text}
		case ")":
			p.curr = tokenInfo{typ: RPAREN, value: text}
		case ",":
			p.curr = tokenInfo{typ: COMMA, value: text}
		case "+", "-", "*", "/", "!", "%", "?", ":":
			p.curr = tokenInfo{typ: OPERATOR, value: text}
		case "@":
			p.curr = tokenInfo{typ: AT, value: text}
		case "<":
			p.curr = tokenInfo{typ: LANGLE, value: text}
		case ">":
			p.curr = tokenInfo{typ: RANGLE, value: text}
		case ".":
			p.curr = tokenInfo{typ: DOT, value: text}
		default:
			p.curr = tokenInfo{typ: IDENT, value: text}
		}
	}
	p.lastLine = p.scanner.Pos().Line
}

func (p *Parser) expect(typ Token) (tokenInfo, error) {
	if p.curr.typ != typ {
		return tokenInfo{}, fmt.Errorf("expected token type %d but got %d (%v) at offset %d in file %s", typ, p.curr.typ, p.curr.value, p.offset, p.scanner.Filename)
	}
	tok := p.curr
	p.nextToken()
	return tok, nil
}

func (p *Parser) Parse() ([]Node, error) {
	var nodes []Node
	for p.curr.typ != EOF {
		node, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (p *Parser) parseComment() (Node, error) {
	node := &CommentNode{Text: p.curr.value}
	p.nextToken()
	return node, nil
}

func (p *Parser) parseStatement() (Node, error) {
	if p.curr.typ == COMMENT {
		return p.parseComment()
	}
	if p.curr.typ == AT {
		return p.parseInclude()
	}
	if p.curr.typ == KEYWORD {
		return p.parseControl()
	}
	if p.curr.typ == IDENT {
		typeName := p.curr.value
		p.nextToken()
		if p.curr.typ == LBRACE {
			return p.parseBlock(typeName, "")
		}
		if p.curr.typ == IDENT || p.curr.typ == STRING {
			label := p.curr.value
			p.nextToken()
			return p.parseBlock(typeName, label)
		}
		return p.parseAssignment(typeName)
	}
	return nil, fmt.Errorf("unexpected token: %v", p.curr.value)
}

func (p *Parser) parseInclude() (Node, error) {
	p.nextToken()
	if p.curr.typ != IDENT || p.curr.value != "include" {
		return nil, fmt.Errorf("expected 'include' after '@', got %v", p.curr.value)
	}
	p.nextToken()
	fileName, err := p.parseFileName()
	if err != nil {
		return nil, err
	}
	var content []byte
	if strings.HasPrefix(fileName, "http://") || strings.HasPrefix(fileName, "https://") {
		resp, err := http.Get(fileName)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch URL %s: %v", fileName, err)
		}
		defer resp.Body.Close()
		content, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read content from URL %s: %v", fileName, err)
		}
	} else {
		if !filepath.IsAbs(fileName) {
			baseDir := filepath.Dir(p.scanner.Filename)
			fileName = filepath.Join(baseDir, fileName)
		}
		content, err = os.ReadFile(fileName)
		if err != nil {
			return nil, fmt.Errorf("failed to read include file %s: %v", fileName, err)
		}
	}
	subParser := NewParser(string(content))
	nodes, err := subParser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse include source %s: %v", fileName, err)
	}
	return &IncludeNode{FileName: fileName, Nodes: nodes}, nil
}

func (p *Parser) parseFileName() (string, error) {
	var name string
	if p.curr.typ == STRING {
		name = p.curr.value
		p.nextToken()
		return name, nil
	} else if p.curr.typ == IDENT {
		name = p.curr.value
		p.nextToken()
		for (p.curr.typ == OPERATOR && p.curr.value == ".") || p.curr.typ == IDENT {
			if p.curr.typ == OPERATOR && p.curr.value == "." {
				name += "."
				p.nextToken()
				if p.curr.typ != IDENT {
					return "", fmt.Errorf("expected identifier after dot in file name, got %v", p.curr.value)
				}
				name += p.curr.value
				p.nextToken()
			} else if p.curr.typ == IDENT {
				name += p.curr.value
				p.nextToken()
			} else {
				break
			}
		}
		return name, nil
	}
	return "", fmt.Errorf("expected file name, got %v", p.curr.value)
}

func (p *Parser) parseAssignment(varName string) (Node, error) {
	if p.curr.typ != ASSIGN {
		return nil, fmt.Errorf("expected '=', got %v", p.curr.value)
	}
	p.nextToken()
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	return &AssignmentNode{VarName: varName, Value: expr}, nil
}

func (p *Parser) parseBlock(typ, label string) (Node, error) {
	_, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	var props []Node
	for p.curr.typ != RBRACE && p.curr.typ != EOF {
		if p.curr.typ == COMMENT {
			comment, err := p.parseComment()
			if err != nil {
				return nil, err
			}
			props = append(props, comment)
			continue
		}
		if p.curr.typ != IDENT {
			return nil, fmt.Errorf("expected property name, got %v", p.curr.value)
		}
		propName := p.curr.value
		p.nextToken()
		propNode, err := p.parseAssignment(propName)
		if err != nil {
			return nil, err
		}
		props = append(props, propNode)
		if p.curr.typ == COMMA {
			p.nextToken()
		}
	}
	_, err = p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	return &BlockNode{Type: typ, Label: label, Props: props}, nil
}

// NEW: Modified parseExpression to support ternary operator
func (p *Parser) parseExpression() (Node, error) {
	expr, err := p.parseBinaryExpression(0)
	if err != nil {
		return nil, err
	}
	if p.curr.typ == OPERATOR && p.curr.value == "?" {
		p.nextToken()
		trueExpr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.curr.typ != OPERATOR || p.curr.value != ":" {
			return nil, fmt.Errorf("expected ':' in ternary operator, got %v", p.curr.value)
		}
		p.nextToken()
		falseExpr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		return &TernaryNode{Condition: expr, TrueExpr: trueExpr, FalseExpr: falseExpr}, nil
	}
	return expr, nil
}

func (p *Parser) parseUnary() (Node, error) {
	if p.curr.typ == OPERATOR && (p.curr.value == "-" || p.curr.value == "!") {
		op := p.curr.value
		p.nextToken()
		child, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryNode{Op: op, Child: child}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parseBinaryExpression(minPrec int) (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		opToken := p.curr
		op, isOp := p.getOperator(opToken)
		if !isOp {
			break
		}
		prec := getPrecedence(op)
		if prec < minPrec {
			break
		}
		p.nextToken()
		right, err := p.parseBinaryExpression(prec + 1)
		if err != nil {
			return nil, err
		}
		if op == "." {
			switch r := right.(type) {
			case *IdentifierNode:
				left = &DotAccessNode{Left: left, Right: r.Name}
			case *PrimitiveNode:
				if s, ok := r.Value.(string); ok {
					left = &DotAccessNode{Left: left, Right: s}
				} else {
					return nil, fmt.Errorf("dot operator right operand must be string or identifier")
				}
			default:
				return nil, fmt.Errorf("invalid right operand for dot operator")
			}
		} else {
			left = &ArithmeticNode{Op: op, Left: left, Right: right}
		}
	}
	return left, nil
}

type FunctionNode struct {
	FuncName string
	Args     []Node
}

func (f *FunctionNode) Eval(env *Environment) (any, error) {
	var args []any
	for _, arg := range f.Args {
		val, err := arg.Eval(env)
		if err != nil {
			return nil, err
		}
		args = append(args, val)
	}
	fn, ok := lookupFunction(f.FuncName)
	if !ok {
		return nil, fmt.Errorf("unknown function %s", f.FuncName)
	}
	return fn(args...)
}

func (f *FunctionNode) ToBCL(indent string) string {
	var argsStr []string
	for _, a := range f.Args {
		argsStr = append(argsStr, a.ToBCL(""))
	}
	return fmt.Sprintf("%s%s(%s)", indent, f.FuncName, strings.Join(argsStr, ", "))
}

// NEW: parseEnvLookup to handle dynamic env lookup expressions
func (p *Parser) parseEnvLookup() (Node, error) {
	// p.curr is "env"
	envNode := &IdentifierNode{Name: "env"}
	p.nextToken() // consume "env"
	// If no dot follows, just return the identifier.
	if p.curr.typ != DOT {
		return envNode, nil
	}
	p.nextToken() // consume "."
	// Now, combine tokens (IDENT, COLON, DOT, OPERATOR) into one string.
	var parts []string
	// Require first part to be an identifier.
	if p.curr.typ != IDENT {
		return nil, fmt.Errorf("expected identifier after 'env.' but got %v", p.curr.value)
	}
	parts = append(parts, p.curr.value)
	p.nextToken()
	// Consume subsequent tokens that can form the full lookup (e.g. "SHELL:/bin/bash").
	for p.curr.typ == DOT || p.curr.typ == IDENT || p.curr.typ == OPERATOR {
		parts = append(parts, p.curr.value)
		p.nextToken()
	}
	return &DotAccessNode{Left: envNode, Right: strings.Join(parts, "")}, nil
}

func (p *Parser) parsePrimary() (Node, error) {
	if p.curr.typ == OPERATOR && p.curr.value == "<<" {
		return p.parseHeredoc()
	}
	switch p.curr.typ {
	case STRING:
		val := p.curr.value
		p.nextToken()
		return &PrimitiveNode{Value: val}, nil
	case NUMBER:
		text := p.curr.value
		p.nextToken()
		if strings.Contains(text, ".") {
			f, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, err
			}
			return &PrimitiveNode{Value: f}, nil
		}
		i, err := strconv.Atoi(text)
		if err != nil {
			return nil, err
		}
		return &PrimitiveNode{Value: i}, nil
	case BOOL:
		b, err := strconv.ParseBool(p.curr.value)
		if err != nil {
			return nil, err
		}
		p.nextToken()
		return &PrimitiveNode{Value: b}, nil
	case IDENT:
		// NEW: Special handling for env lookup
		if p.curr.value == "env" {
			return p.parseEnvLookup()
		}
		ident := p.curr.value
		p.nextToken()
		if p.curr.typ == LPAREN {
			p.nextToken()
			var args []Node
			if p.curr.typ != RPAREN {
				for {
					arg, err := p.parseExpression()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if p.curr.typ == COMMA {
						p.nextToken()
					} else {
						break
					}
				}
			}
			_, err := p.expect(RPAREN)
			if err != nil {
				return nil, err
			}
			return &FunctionNode{FuncName: ident, Args: args}, nil
		}
		return &IdentifierNode{Name: ident}, nil
	case LPAREN:
		p.nextToken()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		_, err = p.expect(RPAREN)
		if err != nil {
			return nil, err
		}
		return expr, nil
	case LBRACE:
		return p.parseMap()
	case LBRACKET:
		return p.parseSlice()
	case AT:
		return p.parseInclude()
	default:
		return nil, fmt.Errorf("unexpected token in expression: %v", p.curr.value)
	}
}

func (p *Parser) getOperator(tok tokenInfo) (string, bool) {
	if tok.typ == OPERATOR {
		// Exclude ternary operator symbols from binary expressions.
		if tok.value == "?" || tok.value == ":" {
			return "", false
		}
		return tok.value, true
	}
	if tok.typ == LANGLE {
		return "<", true
	}
	if tok.typ == DOT {
		return ".", true
	}
	if tok.typ == IDENT {
		upper := strings.ToUpper(tok.value)
		if upper == "OR" || upper == "AND" || upper == "MOD" || upper == "ADD" || upper == "SUBTRACT" ||
			upper == "MULTIPLY" || upper == "DIVIDE" {
			return strings.ToLower(tok.value), true
		}
	}
	return "", false
}

func (p *Parser) parseHeredoc() (Node, error) {
	heredocStartOffset := p.offset
	p.nextToken()
	delimTok, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	delimiter := delimTok.value
	markerLine := p.input[heredocStartOffset:p.offset]
	newlineIdx := strings.Index(markerLine, "\n")
	if newlineIdx == -1 {
		return nil, fmt.Errorf("expected newline after heredoc marker, got marker: %q", markerLine)
	}
	contentStart := heredocStartOffset + newlineIdx + 1
	remaining := p.input[contentStart:]
	lines := strings.Split(remaining, "\n")
	var contentLines []string
	var consumedLines int
	for i, line := range lines {
		if strings.TrimSpace(line) == delimiter {
			consumedLines = i + 1
			break
		}
		contentLines = append(contentLines, line)
	}
	if consumedLines == 0 {
		return nil, fmt.Errorf("heredoc delimiter %s not found", delimiter)
	}
	content := strings.Join(contentLines, "\n")
	newOffset := contentStart
	for i := 0; i < consumedLines; i++ {
		newOffset += len(lines[i]) + 1
	}
	p.offset = newOffset
	var s scanner.Scanner
	s.Init(strings.NewReader(p.input[newOffset:]))
	s.Filename = p.scanner.Filename
	s.Whitespace = 1<<' ' | 1<<'\t' | 1<<'\r' | 1<<'\n'
	s.Mode |= scanner.ScanComments
	p.scanner = s
	p.nextToken()
	return &PrimitiveNode{Value: content}, nil
}

func (p *Parser) parseMap() (Node, error) {
	_, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	var entries []*AssignmentNode
	var blocks []Node
	for p.curr.typ != RBRACE && p.curr.typ != EOF {
		if p.curr.typ == COMMENT {
			comment, err := p.parseComment()
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, comment)
			continue
		}
		if p.curr.typ != IDENT {
			return nil, fmt.Errorf("expected key in map, got %v", p.curr.value)
		}
		key := p.curr.value
		p.nextToken()
		if p.curr.typ == ASSIGN {
			assignment, err := p.parseAssignment(key)
			if err != nil {
				return nil, err
			}
			if assign, ok := assignment.(*AssignmentNode); ok {
				entries = append(entries, assign)
			} else {
				return nil, fmt.Errorf("expected assignment node")
			}
		} else {
			var label string
			if p.curr.typ == IDENT || p.curr.typ == STRING {
				label = p.curr.value
				p.nextToken()
			}
			block, err := p.parseBlock(key, label)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
		if p.curr.typ == COMMA {
			p.nextToken()
		}
	}
	_, err = p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	return &CombinedMapNode{Entries: entries, Blocks: blocks}, nil
}

func (p *Parser) parseSlice() (Node, error) {
	_, err := p.expect(LBRACKET)
	if err != nil {
		return nil, err
	}
	var elems []Node
	for p.curr.typ != RBRACKET && p.curr.typ != EOF {
		elem, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		if p.curr.typ == COMMA {
			p.nextToken()
		}
	}
	_, err = p.expect(RBRACKET)
	if err != nil {
		return nil, err
	}
	return &SliceNode{Elements: elems}, nil
}

func (p *Parser) parseControl() (Node, error) {
	keyword := p.curr.value
	p.nextToken()
	var condition Node
	if keyword != "ELSE" {
		// If a left parenthesis is present, use it to enclose the condition,
		// otherwise parse the expression directly.
		if p.curr.typ == LPAREN {
			_, err := p.expect(LPAREN)
			if err != nil {
				return nil, err
			}
			cond, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			condition = cond
			_, err = p.expect(RPAREN)
			if err != nil {
				return nil, err
			}
		} else {
			cond, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			condition = cond
		}
	}

	// Allow the block to start with either LBRACE or LPAREN.
	var blockStart Token
	if p.curr.typ == LBRACE {
		blockStart = LBRACE
	} else if p.curr.typ == LPAREN {
		blockStart = LPAREN
	} else {
		return nil, fmt.Errorf("expected block start token (\"{\" or \"(\") but got %v", p.curr.value)
	}
	_, err := p.expect(blockStart)
	if err != nil {
		return nil, err
	}

	// Set the corresponding block end token.
	var blockEnd Token
	if blockStart == LBRACE {
		blockEnd = RBRACE
	} else {
		blockEnd = RPAREN
	}

	var body []Node
	for p.curr.typ != blockEnd && p.curr.typ != EOF {
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		body = append(body, stmt)
	}
	_, err = p.expect(blockEnd)
	if err != nil {
		return nil, err
	}
	control := &ControlNode{Condition: condition, Body: body}
	if p.curr.typ == KEYWORD && (p.curr.value == "ELSEIF" || p.curr.value == "ELSE") {
		elseNode, err := p.parseControl()
		if err != nil {
			return nil, err
		}
		if ctrlElse, ok := elseNode.(*ControlNode); ok {
			control.Else = ctrlElse
		}
	}
	return control, nil
}

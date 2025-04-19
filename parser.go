package bcl

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
	r := p.scanner.Scan() // <--- call Scan() at start
	text := p.scanner.TokenText()
	pos := p.scanner.Pos()
	p.offset = int(pos.Offset)

	// Handle "??" operator.
	if text == "?" && p.scanner.Peek() == '?' {
		p.scanner.Next() // consume second '?'
		pos = p.scanner.Pos()
		p.curr = tokenInfo{typ: OPERATOR, value: "??"}
		p.offset = int(pos.Offset)
		p.lastLine = pos.Line
		return
	}
	// Handle "!=" operator.
	if text == "!" && p.scanner.Peek() == '=' {
		p.scanner.Next() // consume '='
		pos = p.scanner.Pos()
		p.curr = tokenInfo{typ: OPERATOR, value: "!="}
		p.offset = int(pos.Offset)
		p.lastLine = pos.Line
		return
	}

	text = p.scanner.TokenText()
	pos = p.scanner.Pos()
	p.offset = int(pos.Offset)
	if text == "?" && p.scanner.Peek() == '?' {
		p.scanner.Next()
		pos = p.scanner.Pos()
		p.curr = tokenInfo{typ: OPERATOR, value: "??"}
		p.offset = int(pos.Offset)
		p.lastLine = pos.Line
		return
	}
	if (text == "|" || text == "&") && p.scanner.Peek() == rune(text[0]) {
		p.scanner.Next()
		if text == "|" {
			p.curr = tokenInfo{typ: OPERATOR, value: "||"}
		} else {
			p.curr = tokenInfo{typ: OPERATOR, value: "&&"}
		}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	switch text {
	case "&", "|", "^":
		p.curr = tokenInfo{typ: OPERATOR, value: text}
		p.lastLine = pos.Line
		return
	}
	if text == "<" && p.scanner.Peek() == '<' {
		p.scanner.Next()
		p.curr = tokenInfo{typ: OPERATOR, value: "<<"}
		p.offset = int(p.scanner.Pos().Offset)
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if text == "<" && p.scanner.Peek() == '=' {
		p.scanner.Next()
		p.curr = tokenInfo{typ: OPERATOR, value: "<="}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if text == ">" && p.scanner.Peek() == '=' {
		p.scanner.Next()
		p.curr = tokenInfo{typ: OPERATOR, value: ">="}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	if text == "-" && p.scanner.Peek() == '>' {
		p.scanner.Next()
		p.curr = tokenInfo{typ: OPERATOR, value: "->"}
		p.lastLine = p.scanner.Pos().Line
		return
	}
	switch text {
	case "<":
		p.curr = tokenInfo{typ: OPERATOR, value: text}
	case ">":
		p.curr = tokenInfo{typ: OPERATOR, value: text}
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
		sb := getBuilder(16)
		sb.WriteString("#")
		for ch := p.scanner.Peek(); ch != '\n' && ch != scanner.EOF; ch = p.scanner.Peek() {
			sb.WriteByte(byte(p.scanner.Next()))
		}
		p.curr = tokenInfo{typ: COMMENT, value: sb.String()}
		putBuilder(sb)
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
		case ".", "<", ">":
			p.curr = tokenInfo{typ: OPERATOR, value: text}
		default:
			p.curr = tokenInfo{typ: IDENT, value: text}
		}
	}
	p.lastLine = p.scanner.Pos().Line
}

func (p *Parser) parseError(msg string) error {
	pos := p.scanner.Pos()
	lines := strings.Split(p.input, "\n")
	var errorLine string
	if pos.Line-1 < len(lines) {
		errorLine = lines[pos.Line-1]
	}
	return fmt.Errorf("%s at %s:%d:%d\nError line: %s\nHint: check your syntax near this token", msg, p.scanner.Filename, pos.Line, pos.Column, errorLine)
}

func (p *Parser) expect(typ Token) (tokenInfo, error) {
	if p.curr.typ != typ {
		return tokenInfo{}, p.parseError(fmt.Sprintf("expected token type %d but got %d (%v)", typ, p.curr.typ, p.curr.value))
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

var edgeTypes = []string{
	"Arrow",
	"Edge",
	"Connection",
	"Link",
	"Relation",
	"Relationship",
	"Association",
	"Dependency",
	"Flow",
	"Path",
	"Route",
	"Linkage",
	"Bridge",
	"Connector",
	"Channel",
	"Network",
	"Pipeline",
	"Stream",
	"Nexus",
	"Tie",
	"Bond",
	"Interface",
	"Conduit",
	"Join",
	"Interconnection",
	"Coupling",
	"Union",
	"Intersection",
	"Convergence",
	"Confluence",
	"Adjacency",
	"Contact",
	"Junction",
	"Merge",
	"Overlay",
	"Attachment",
	"Interlock",
	"Correlation",
	"Affinity",
	"Liaison",
	"Integration",
	"Synthesis",
	"Binding",
	"Chain",
	"Concatenation",
	"Sequence",
	"Thread",
	"Continuum",
	"Series",
	"Linkup",
	"Tie-in",
	"Interweave",
	"Mesh",
	"Grid",
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
	if p.curr.typ == IDENT && slices.Contains(edgeTypes, p.curr.value) {
		arrowType := p.curr.value
		p.nextToken()
		// Look ahead for "->"
		rem := strings.TrimLeft(p.input[p.offset:], " \t")
		if strings.HasPrefix(rem, "->") {
			return p.parseArrow(arrowType)
		}
		// Otherwise, fall through to normal processing
		// (e.g., if type token is followed by a regular assignment)
		// Here we treat it as a normal identifier.
	}
	// If no arrow type token, check if the next token is an arrow operator.
	if p.curr.typ == IDENT || p.curr.typ == STRING {
		rem := strings.TrimLeft(p.input[p.offset:], " \t")
		if strings.HasPrefix(rem, "->") {
			// Default arrow type to "Edge"
			return p.parseArrow("Edge")
		}
		// Normal processing for blocks or assignments.
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
	return nil, p.parseError(fmt.Sprintf("unexpected token: %v", p.curr.value))
}

func (p *Parser) parseArrow(arrowType string) (Node, error) {
	// If the type token was provided (e.g. "Edge" or "Arrow"), use it;
	// otherwise, arrowType may be "Edge" by default.
	// Now, current token is the source.
	source := p.curr.value
	p.nextToken()
	if p.curr.typ != OPERATOR || p.curr.value != "->" {
		return nil, p.parseError(fmt.Sprintf("expected '->' operator after source, got %v", p.curr.value))
	}
	p.nextToken() // consume "->"
	if p.curr.typ != IDENT && p.curr.typ != STRING {
		return nil, p.parseError(fmt.Sprintf("expected target after '->', got %v", p.curr.value))
	}
	target := p.curr.value
	p.nextToken() // consume target
	if p.curr.typ != LBRACE {
		return nil, p.parseError(fmt.Sprintf("expected '{' to start arrow block, got %v", p.curr.value))
	}
	_, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	var nodes []Node
	for p.curr.typ != RBRACE && p.curr.typ != EOF {
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, stmt)
	}
	_, err = p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	return &ArrowNode{Type: arrowType, Source: source, Target: target, Props: nodes}, nil
}

func (p *Parser) parseInclude() (Node, error) {
	p.nextToken()
	if p.curr.typ != IDENT || p.curr.value != "include" {
		return nil, p.parseError(fmt.Sprintf("expected 'include' after '@', got %v", p.curr.value))
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
			return nil, p.parseError(fmt.Sprintf("failed to fetch URL %s: %v", fileName, err))
		}
		defer resp.Body.Close()
		content, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, p.parseError(fmt.Sprintf("failed to read content from URL %s: %v", fileName, err))
		}
	} else {
		if !filepath.IsAbs(fileName) {
			baseDir := filepath.Dir(p.scanner.Filename)
			fileName = filepath.Join(baseDir, fileName)
		}
		content, err = os.ReadFile(fileName)
		if err != nil {
			return nil, p.parseError(fmt.Sprintf("failed to read include file %s: %v", fileName, err))
		}
	}
	subParser := NewParser(string(content))
	nodes, err := subParser.Parse()
	if err != nil {
		return nil, p.parseError(fmt.Sprintf("failed to parse include source %s: %v", fileName, err))
	}
	return &IncludeNode{FileName: fileName, Nodes: nodes}, nil
}

func (p *Parser) parseFileName() (string, error) {
	sb := getBuilder(16)
	if p.curr.typ == STRING {
		sb.WriteString(p.curr.value)
		p.nextToken()
		result := sb.String()
		putBuilder(sb)
		return result, nil
	} else if p.curr.typ == IDENT {
		sb.WriteString(p.curr.value)
		p.nextToken()
		for (p.curr.typ == OPERATOR && p.curr.value == ".") || p.curr.typ == IDENT {
			if p.curr.typ == OPERATOR && p.curr.value == "." {
				sb.WriteByte('.')
				p.nextToken()
				if p.curr.typ != IDENT {
					putBuilder(sb)
					return "", p.parseError(fmt.Sprintf("expected identifier after dot in file name, got %v", p.curr.value))
				}
				sb.WriteString(p.curr.value)
				p.nextToken()
			} else if p.curr.typ == IDENT {
				sb.WriteString(p.curr.value)
				p.nextToken()
			} else {
				break
			}
		}
		result := sb.String()
		putBuilder(sb)
		return result, nil
	}
	return "", p.parseError(fmt.Sprintf("expected file name, got %v", p.curr.value))
}

func (p *Parser) parseAssignment(varName string) (Node, error) {
	if p.curr.typ == COMMA {
		lhs := []string{varName}
		for p.curr.typ == COMMA {
			p.nextToken()
			if p.curr.typ != IDENT {
				return nil, p.parseError(fmt.Sprintf("expected identifier in multiple assignment, got %v", p.curr.value))
			}
			lhs = append(lhs, p.curr.value)
			p.nextToken()
		}
		if p.curr.typ != ASSIGN && !(p.curr.typ == OPERATOR && p.curr.value == ":") {
			var assignments []*AssignmentNode
			for _, variable := range lhs {
				assignments = append(assignments, &AssignmentNode{VarName: variable, Value: &PrimitiveNode{Value: true}})
			}
			return &MultiAssignNode{Assignments: assignments}, nil
		} else {
			p.nextToken()
		}
		var rhs []Node
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		rhs = append(rhs, expr)
		for p.curr.typ == COMMA {
			p.nextToken()
			expr, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			rhs = append(rhs, expr)
		}
		if len(lhs) != len(rhs) {
			return nil, p.parseError("number of variables and values do not match in multiple assignment")
		}
		var assignments []*AssignmentNode
		for i, variable := range lhs {
			assignments = append(assignments, &AssignmentNode{VarName: variable, Value: rhs[i]})
		}
		return &MultiAssignNode{Assignments: assignments}, nil
	}

	if p.curr.typ != ASSIGN && !(p.curr.typ == OPERATOR && p.curr.value == ":") {
		return &AssignmentNode{VarName: varName, Value: &PrimitiveNode{Value: true}}, nil
	} else {
		p.nextToken()
	}
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	firstAssign := &AssignmentNode{VarName: varName, Value: expr}
	assignments := []*AssignmentNode{firstAssign}
	for p.curr.typ == COMMA {
		p.nextToken()
		if p.curr.typ != IDENT {
			break
		}
		nextVar := p.curr.value
		p.nextToken()
		if p.curr.typ != ASSIGN && !(p.curr.typ == OPERATOR && p.curr.value == ":") {
			assignments = append(assignments, &AssignmentNode{VarName: nextVar, Value: &PrimitiveNode{Value: true}})
			continue
		}
		p.nextToken()
		nextExpr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, &AssignmentNode{VarName: nextVar, Value: nextExpr})
	}
	if len(assignments) == 1 {
		return firstAssign, nil
	}
	return &MultiAssignNode{Assignments: assignments}, nil
}

func (p *Parser) parseBlock(typ, label string) (Node, error) {
	_, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	var nodes []Node
	for p.curr.typ != RBRACE && p.curr.typ != EOF {
		if p.curr.typ == COMMENT {
			comment, err := p.parseComment()
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, comment)
			continue
		}
		if p.curr.typ != IDENT {
			return nil, p.parseError(fmt.Sprintf("expected property name, got %v", p.curr.value))
		}
		propName := p.curr.value
		p.nextToken()
		var node Node
		if p.curr.typ == ASSIGN || (p.curr.typ == OPERATOR && p.curr.value == ":") {
			node, err = p.parseAssignment(propName)
			if err != nil {
				return nil, err
			}
		} else if p.curr.typ == LBRACE {
			node, err = p.parseBlock(propName, "")
			if err != nil {
				return nil, err
			}
		} else if p.curr.typ == IDENT || p.curr.typ == STRING {
			lbl := p.curr.value
			p.nextToken()
			if p.curr.typ == LBRACE {
				node, err = p.parseBlock(propName, lbl)
				if err != nil {
					return nil, err
				}
			} else {
				node = &AssignmentNode{VarName: propName, Value: &PrimitiveNode{Value: true}}
			}
		} else if p.curr.typ == COMMA || p.curr.typ == RBRACE {
			node = &AssignmentNode{VarName: propName, Value: &PrimitiveNode{Value: true}}
		} else {
			return nil, p.parseError(fmt.Sprintf("unexpected token %v after property key %s", p.curr.value, propName))
		}
		nodes = append(nodes, node)
		if p.curr.typ == COMMA {
			p.nextToken()
		}
	}
	_, err = p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	return &BlockNode{Type: typ, Label: label, Props: nodes}, nil
}

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
			return nil, p.parseError(fmt.Sprintf("expected ':' in ternary operator, got %v", p.curr.value))
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
					return nil, p.parseError(fmt.Sprintf("dot operator right operand must be string or identifier"))
				}
			default:
				return nil, p.parseError(fmt.Sprintf("invalid right operand for dot operator"))
			}
		} else {
			left = &ArithmeticNode{Op: op, Left: left, Right: right}
		}
	}
	return left, nil
}

func (p *Parser) parseEnvLookup() (Node, error) {
	envNode := &IdentifierNode{Name: "env"}
	p.nextToken()
	if p.curr.typ != DOT {
		return envNode, nil
	}
	p.nextToken()
	var parts []string
	if p.curr.typ != IDENT {
		return nil, p.parseError(fmt.Sprintf("expected identifier after 'env.' but got %v", p.curr.value))
	}
	parts = append(parts, p.curr.value)
	p.nextToken()
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
		value := p.curr.value
		if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
			inner := value[2 : len(value)-1]
			parts := strings.SplitN(inner, ":", 2)
			if len(parts) == 2 {
				envVar := strings.TrimSpace(parts[0])
				defaultValue := strings.ReplaceAll(strings.TrimSpace(parts[1]), "'", "")
				p.nextToken()
				return &EnvInterpolationNode{EnvVar: envVar, DefaultValue: defaultValue}, nil
			}
		}
		if len(value) > 0 && (value[0] == '"' || value[0] == '`') {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return nil, p.parseError(fmt.Sprintf("malformed quoted string: %v", value))
			}
			value = unquoted
		}
		p.nextToken()
		return &PrimitiveNode{Value: value}, nil
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
		if p.curr.value == "null" {
			p.nextToken()
			return &PrimitiveNode{Value: nil}, nil
		}
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
		return &GroupNode{Child: expr}, nil
	case LBRACE:
		return p.parseMap()
	case LBRACKET:
		return p.parseSlice()
	case AT:
		return p.parseInclude()
	default:
		return nil, p.parseError(fmt.Sprintf("unexpected token in expression: %v", p.curr.value))
	}
}

func (p *Parser) getOperator(tok tokenInfo) (string, bool) {
	if tok.typ == OPERATOR {
		if tok.value == "?" || tok.value == ":" {
			return "", false
		}
		return tok.value, true
	}
	if tok.typ == DOT {
		return ".", true
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
	newlineIdx := strings.IndexByte(p.input[heredocStartOffset:], '\n')
	if newlineIdx == -1 {
		return nil, p.parseError(fmt.Sprintf("expected newline after heredoc marker, got marker: %q", p.input[heredocStartOffset:]))
	}
	contentStart := heredocStartOffset + newlineIdx + 1
	pos := contentStart
	var delimPos int = -1
	for {
		nextNewline := strings.IndexByte(p.input[pos:], '\n')
		if nextNewline == -1 {
			break
		}
		line := p.input[pos : pos+nextNewline]
		if strings.TrimSpace(line) == delimiter {
			delimPos = pos
			break
		}
		pos += nextNewline + 1
	}
	if delimPos == -1 {
		return nil, p.parseError(fmt.Sprintf("heredoc delimiter %s not found", delimiter))
	}
	content := p.input[contentStart:delimPos]
	newOffset := delimPos + len(delimiter)
	if newOffset < len(p.input) && p.input[newOffset] == '\n' {
		newOffset++
	}
	p.input = p.input[newOffset:]
	p.offset = 0
	var s scanner.Scanner
	s.Init(strings.NewReader(p.input))
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
			return nil, p.parseError(fmt.Sprintf("expected key in map, got %v", p.curr.value))
		}
		key := p.curr.value
		p.nextToken()
		if p.curr.typ == ASSIGN {
			assignment, err := p.parseAssignment(key)
			if err != nil {
				return nil, err
			}
			switch v := assignment.(type) {
			case *AssignmentNode:
				entries = append(entries, v)
			case *MultiAssignNode:
				if len(v.Assignments) == 1 {
					entries = append(entries, v.Assignments[0])
				} else {
					entries = append(entries, v.Assignments...)
				}
			}
		} else if p.curr.typ == LBRACE {
			block, err := p.parseBlock(key, "")
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		} else if p.curr.typ == IDENT || p.curr.typ == STRING {
			label := p.curr.value
			p.nextToken()
			if p.curr.typ != LBRACE {
				return nil, p.parseError(fmt.Sprintf("expected '{' after label %s, got %v", label, p.curr.value))
			}
			block, err := p.parseBlock(key, label)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		} else {
			return nil, p.parseError(fmt.Sprintf("unexpected token %v after key %s", p.curr.value, key))
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
	var blockStart Token
	if p.curr.typ == LBRACE {
		blockStart = LBRACE
	} else if p.curr.typ == LPAREN {
		blockStart = LPAREN
	} else {
		return nil, p.parseError(fmt.Sprintf("expected block start token (\"{\" or \"(\") but got %v", p.curr.value))
	}
	_, err := p.expect(blockStart)
	if err != nil {
		return nil, err
	}
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

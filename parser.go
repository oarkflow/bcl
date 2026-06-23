package bcl

import (
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func Parse(src []byte) (*Document, error) {
	return ParseFile("<input>", src)
}

func ParseFile(name string, src []byte) (*Document, error) {
	source := string(src)
	toks, errs := lexStringPooled(name, source)
	defer putTokenScratch(toks)
	if len(errs) > 0 {
		return nil, errs
	}
	p := &parser{file: name, source: source, toks: toks}
	doc := &Document{File: name}
	doc.Items = p.parseNodes(tokEOF)
	if len(p.errs) > 0 {
		return nil, p.errs
	}
	if len(doc.Items) > 0 {
		doc.Span.Start = doc.Items[0].GetSpan().Start
		doc.Span.End = doc.Items[len(doc.Items)-1].GetSpan().End
		doc.Span.File = name
	}
	return doc, nil
}

type TriviaDocument struct {
	Document *Document `json:"document"`
	Comments []Trivia  `json:"comments,omitempty"`
}

type Trivia struct {
	Text string `json:"text"`
	Span Span   `json:"span"`
}

func ParseFileWithTrivia(name string, src []byte) (*TriviaDocument, error) {
	doc, err := ParseFile(name, src)
	if err != nil {
		return nil, err
	}
	return &TriviaDocument{Document: doc, Comments: collectCommentTrivia(name, string(src))}, nil
}

func collectCommentTrivia(file, src string) []Trivia {
	var out []Trivia
	l := &lexer{file: file, src: src, line: 1, col: 1}
	for {
		r := l.peek()
		if r == 0 {
			return out
		}
		if r == '#' || r == '/' && l.peekN(1) == '/' {
			sp := l.spanAt()
			start := l.pos
			l.skipLine()
			sp.End = l.posn()
			out = append(out, Trivia{Text: src[start:l.pos], Span: sp})
			continue
		}
		if r == '/' && l.peekN(1) == '*' {
			sp := l.spanAt()
			start := l.pos
			_ = l.skipBlockComment()
			sp.End = l.posn()
			out = append(out, Trivia{Text: src[start:l.pos], Span: sp})
			continue
		}
		l.advance()
	}
}

func ParsePath(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseFile(path, b)
}

type parser struct {
	file   string
	source string
	toks   []token
	pos    int
	errs   ErrorList
}

func (p *parser) parseNodes(until tokenKind) []Node {
	nodes := make([]Node, 0, p.nodeCapacity(until))
	for {
		p.skipNodeSeparators()
		if p.peek().kind == until || p.peek().kind == tokEOF {
			if until != tokEOF && p.peek().kind == until {
				p.next()
			}
			return nodes
		}
		n := p.parseNode()
		if n != nil {
			nodes = append(nodes, n)
		} else {
			p.recoverLine()
		}
	}
}

func (p *parser) nodeCapacity(until tokenKind) int {
	remaining := len(p.toks) - p.pos
	if remaining <= 0 {
		return 0
	}
	if until == tokEOF {
		capHint := remaining / 5
		if capHint < 4 {
			return 4
		}
		if capHint > 32 {
			return 32
		}
		return capHint
	}
	if remaining < 16 {
		return 2
	}
	return 4
}

func (p *parser) parseNode() Node {
	t := p.peek()
	if t.kind == tokOperator && t.text == "&" {
		return p.parseSpread()
	}
	if t.kind == tokLBrace {
		lb := p.next()
		body := p.parseNodes(tokRBrace)
		return &Object{Fields: body, Span: lb.span}
	}
	if t.kind != tokIdent && t.kind != tokString && t.kind != tokNumber {
		p.error(t, "expected declaration, assignment, or block")
		return nil
	}
	if t.kind == tokNumber {
		name := p.next()
		if p.peek().kind == tokNewline || p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
			return &Assignment{Name: name.text, Value: &Reference{Path: "", Span: name.span}, Span: name.span}
		}
		v := p.parseValueUntilLine()
		return &Assignment{Name: name.text, Value: v, Span: spanJoin(name.span, v.GetSpan())}
	}
	if t.kind == tokString {
		name := p.next()
		if p.peek().kind == tokLBrace {
			lb := p.next()
			return &Assignment{Name: name.text, Value: &Object{Fields: p.parseNodes(tokRBrace), Span: spanJoin(name.span, lb.span)}, Span: name.span}
		}
		if p.peek().kind == tokNewline || p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
			return &Assignment{Name: name.text, Value: &Literal{Type: "string", Data: name.text, Span: name.span}, Span: name.span}
		}
		v := p.parseValueUntilLine()
		return &Assignment{Name: name.text, Value: v, Span: spanJoin(name.span, v.GetSpan())}
	}
	switch t.text {
	case "import":
		return p.parseImport()
	case "param":
		if p.peekN(1).kind == tokIdent {
			return p.parseParam()
		}
	case "const":
		return p.parseConst()
	case "schema":
		if p.peekN(1).kind == tokIdent && p.peekN(2).kind == tokLBrace {
			return p.parseSchema()
		}
	case "type":
		if p.peekN(1).kind == tokIdent && p.peekN(2).kind == tokEqual {
			return p.parseTypeDecl()
		}
		if p.peekN(1).kind == tokIdent {
			return p.parseLineStringAssignment(p.next())
		}
	}
	name := p.next()
	name.text = strings.TrimSuffix(name.text, ":")
	if name.text == "override" && p.peek().kind == tokIdent && p.peekN(1).kind == tokString && p.peekN(2).kind == tokLBrace {
		targetType := p.next()
		targetID := p.next()
		bodyStart := p.next()
		return &Block{Type: "override", ID: targetType.text + "." + targetID.text, Body: p.parseNodes(tokRBrace), Span: spanJoin(name.span, bodyStart.span)}
	}
	if name.text == "use" && p.peek().kind == tokIdent && p.peekN(1).kind == tokString && p.peekN(2).kind == tokLBrace {
		targetType := p.next()
		targetID := p.next()
		bodyStart := p.next()
		return &Block{Type: "use", ID: targetType.text + "." + targetID.text, Body: p.parseNodes(tokRBrace), Span: spanJoin(name.span, bodyStart.span)}
	}
	if name.text == "when" && p.peek().kind != tokLBrace {
		return p.parseConditionalBlock(name)
	}
	if p.peek().kind == tokLParen {
		return p.parseExprNode(name)
	}
	if isCommandStatement(name.text) {
		return p.parseExprNode(name)
	}
	if p.peek().kind == tokDot && p.dottedAssignmentAhead() {
		return p.parseDottedAssignment(name)
	}
	if name.text == "override" && p.peek().kind == tokIdent && p.peekN(1).kind == tokLBrace {
		target := p.next()
		bodyStart := p.next()
		return &Block{Type: "override", ID: target.text, Body: p.parseNodes(tokRBrace), Span: spanJoin(name.span, bodyStart.span)}
	}
	if p.peek().kind == tokString && p.peekN(1).kind == tokLBrace {
		id := p.next()
		bodyStart := p.next()
		return &Block{Type: name.text, ID: id.text, Body: p.parseNodes(tokRBrace), Span: spanJoin(name.span, bodyStart.span)}
	}
	if p.peek().kind == tokNumber && p.peekN(1).kind == tokLBrace {
		id := p.next()
		bodyStart := p.next()
		return &Block{Type: name.text, ID: id.text, Body: p.parseNodes(tokRBrace), Span: spanJoin(name.span, bodyStart.span)}
	}
	if p.peek().kind == tokIdent && p.peekN(1).kind == tokLBrace {
		id := p.next()
		bodyStart := p.next()
		return &Block{Type: name.text, ID: id.text, Body: p.parseNodes(tokRBrace), Span: spanJoin(name.span, bodyStart.span)}
	}
	if p.peek().kind == tokIdent && (p.peekN(1).kind == tokString || p.peekN(1).kind == tokHeredoc) {
		kind := p.next()
		val := p.parseValue()
		obj := &Object{Fields: []Node{
			&Assignment{Name: "type", Value: &Literal{Type: "identifier", Data: kind.text, Span: kind.span}, Span: kind.span},
			&Assignment{Name: "value", Value: val, Span: val.GetSpan()},
		}, Span: spanJoin(kind.span, val.GetSpan())}
		return &Assignment{Name: name.text, Value: obj, Span: spanJoin(name.span, val.GetSpan())}
	}
	if p.peek().kind == tokLBrace {
		lb := p.next()
		body := p.parseNodes(tokRBrace)
		if isKnownBlock(name.text) || isCapitalizedBlockName(name.text) {
			return &Block{Type: name.text, Body: body, Span: spanJoin(name.span, lb.span)}
		}
		return &Assignment{Name: name.text, Value: blockValue(name.text, body, spanJoin(name.span, lb.span)), Span: name.span}
	}
	if p.startsExpressionAfterName() {
		return p.parseExprNode(name)
	}
	if p.peek().kind == tokEqual {
		p.next()
	}
	if p.peek().kind == tokNewline || p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
		return &Assignment{Name: name.text, Value: &Reference{Path: "", Span: name.span}, Span: name.span}
	}
	v := p.parseValueUntilLine()
	return &Assignment{Name: name.text, Value: v, Span: spanJoin(name.span, v.GetSpan())}
}

func (p *parser) parseSpread() Node {
	start := p.next()
	target := p.parseSpreadTarget()
	sp := spanJoin(start.span, target.span)
	var body []Node
	if p.peek().kind == tokLBrace {
		lb := p.next()
		sp = spanJoin(sp, lb.span)
		body = p.parseNodes(tokRBrace)
		if len(body) > 0 {
			sp = spanJoin(sp, body[len(body)-1].GetSpan())
		}
	}
	return &Spread{Target: target.text, Body: body, Span: sp}
}

func (p *parser) parseSpreadTarget() token {
	t := p.peek()
	if t.kind != tokIdent && t.kind != tokString && t.kind != tokNumber {
		p.error(t, "expected spread target")
		return token{text: "", span: t.span}
	}
	first := p.next()
	parts := []string{first.text}
	sp := first.span
	for p.peek().kind == tokDot {
		p.next()
		next := p.peek()
		if next.kind != tokIdent && next.kind != tokString && next.kind != tokNumber {
			p.error(next, "expected spread target path segment")
			break
		}
		part := p.next()
		parts = append(parts, part.text)
		sp = spanJoin(sp, part.span)
	}
	return token{text: strings.Join(parts, "."), span: sp}
}

func (p *parser) parseConditionalBlock(first token) Node {
	start := first.span
	depth := 0
	rawStart := first.span.End.Offset
	rawEnd := rawStart
	for {
		t := p.peek()
		if t.kind == tokEOF || (depth == 0 && t.kind == tokLBrace) {
			break
		}
		if t.kind == tokLBracket || t.kind == tokLParen {
			depth++
		}
		if t.kind == tokRBracket || t.kind == tokRParen {
			depth--
		}
		rawEnd = p.next().span.End.Offset
	}
	bodyStart := p.expect(tokLBrace, "expected conditional block body")
	body := p.parseNodes(tokRBrace)
	cond := &Expr{Raw: p.rawExpr(rawStart, rawEnd), Span: spanJoin(start, bodyStart.span)}
	return &Block{Type: first.text, ID: cond.Raw, Body: body, Span: spanJoin(start, bodyStart.span)}
}

func (p *parser) startsExpressionAfterName() bool {
	t := p.peek()
	return t.kind == tokDot || t.kind == tokOperator || isExprOperator(t.text)
}

func (p *parser) dottedAssignmentAhead() bool {
	i := p.pos
	for i+1 < len(p.toks) && p.toks[i].kind == tokDot && p.toks[i+1].kind == tokIdent {
		i += 2
	}
	if i >= len(p.toks) {
		return false
	}
	t := p.toks[i]
	if t.kind == tokNewline || t.kind == tokRBrace || t.kind == tokEOF {
		return false
	}
	if t.kind == tokLParen {
		return false
	}
	if t.kind == tokOperator || isExprOperator(t.text) {
		return false
	}
	return true
}

func (p *parser) parseDottedAssignment(first token) Node {
	parts := []string{first.text}
	for p.peek().kind == tokDot {
		p.next()
		parts = append(parts, p.expect(tokIdent, "expected assignment path segment").text)
	}
	if p.peek().kind == tokEqual {
		p.next()
	}
	v := p.parseValueUntilLine()
	return &Assignment{Name: strings.Join(parts, "."), Value: v, Span: spanJoin(first.span, v.GetSpan())}
}

func (p *parser) parseExprNode(first token) Node {
	start := first.span
	depth := 0
	rawStart := first.span.Start.Offset
	rawEnd := first.span.End.Offset
	for {
		t := p.peek()
		if t.kind == tokEOF || (depth == 0 && (t.kind == tokNewline || t.kind == tokRBrace)) {
			break
		}
		if t.kind == tokLBrace || t.kind == tokLBracket || t.kind == tokLParen {
			depth++
		}
		if t.kind == tokRBrace || t.kind == tokRBracket || t.kind == tokRParen {
			depth--
		}
		rawEnd = p.next().span.End.Offset
	}
	end := start
	if p.pos > 0 {
		end = p.toks[p.pos-1].span
	}
	out := &Expr{Raw: p.rawExpr(rawStart, rawEnd), Span: spanJoin(start, end)}
	return &Assignment{Name: "$expr", Value: out, Span: out.Span}
}

func (p *parser) parseLineStringAssignment(name token) Node {
	rawStart := p.peek().span.Start.Offset
	rawEnd := rawStart
	depth := 0
	for {
		t := p.peek()
		if t.kind == tokEOF || (depth == 0 && (t.kind == tokNewline || t.kind == tokRBrace)) {
			break
		}
		if t.kind == tokLBrace || t.kind == tokLBracket || t.kind == tokLParen {
			depth++
		}
		if t.kind == tokRBrace || t.kind == tokRBracket || t.kind == tokRParen {
			depth--
		}
		rawEnd = p.next().span.End.Offset
	}
	sp := name.span
	if p.pos > 0 {
		sp = spanJoin(sp, p.toks[p.pos-1].span)
	}
	return &Assignment{
		Name:  name.text,
		Value: &Literal{Type: "string", Data: p.rawExpr(rawStart, rawEnd), Span: sp},
		Span:  sp,
	}
}

func (p *parser) parseImport() Node {
	start := p.next()
	path := p.expect(tokString, "expected import path string")
	imp := &ImportDecl{Path: path.text, Span: spanJoin(start.span, path.span)}
	if p.peek().kind == tokIdent && p.peek().text == "as" {
		p.next()
		alias := p.expect(tokIdent, "expected import alias")
		imp.Alias = alias.text
		imp.Span = spanJoin(imp.Span, alias.span)
	}
	return imp
}

func (p *parser) parseParam() Node {
	start := p.next()
	name := p.expect(tokIdent, "expected param name")
	typ := p.parseSchemaType()
	param := &ParamDecl{Name: name.text, Type: typ, Span: spanJoin(start.span, name.span)}
	if p.peek().kind == tokLBrace {
		lb := p.next()
		param.Span = spanJoin(param.Span, lb.span)
		for {
			p.skipNewlines()
			if p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
				if p.peek().kind == tokRBrace {
					param.Span = spanJoin(param.Span, p.next().span)
				}
				return param
			}
			key := p.expect(tokIdent, "expected param option")
			switch key.text {
			case "required":
				v := p.parseValueUntilLine()
				if lit, ok := v.(*Literal); ok {
					if b, ok := lit.Data.(bool); ok {
						param.Required = b
					}
				}
			case "default":
				param.Default = p.parseValueUntilLine()
			case "description", "doc":
				param.Description = p.schemaStringClause()
			default:
				p.skipLineTail()
			}
		}
	}
	return param
}

func (p *parser) parseTypeDecl() Node {
	start := p.next()
	name := p.expect(tokIdent, "expected type alias name")
	if p.peek().kind == tokEqual {
		p.next()
	}
	typ := p.parseSchemaType()
	return &TypeDecl{Name: name.text, Type: typ, Span: spanJoin(start.span, name.span)}
}

func (p *parser) parseConst() Node {
	start := p.next()
	name := p.expect(tokIdent, "expected constant name")
	if p.peek().kind == tokEqual {
		p.next()
	}
	v := p.parseValueUntilLine()
	return &ConstDecl{Name: name.text, Value: v, Span: spanJoin(start.span, v.GetSpan())}
}

func (p *parser) parseSchema() Node {
	start := p.next()
	name := p.expect(tokIdent, "expected schema name")
	p.expect(tokLBrace, "expected schema body")
	s := &SchemaDecl{Name: name.text, Span: spanJoin(start.span, name.span)}
	fieldsByName := map[string]int{}
	for {
		p.skipNewlines()
		if p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
			if p.peek().kind == tokRBrace {
				s.Span = spanJoin(s.Span, p.next().span)
			}
			return s
		}
		if p.peek().kind == tokIdent && p.peek().text == "type" {
			p.parseLegacySchemaTypeClause(s, fieldsByName)
			continue
		}
		if p.peek().kind == tokIdent && p.peekN(1).kind == tokLBrace {
			name := p.next()
			p.next()
			switch name.text {
			case "options":
				s.Options = p.parseSchemaOptionsBlock()
			case "fields":
				for _, field := range p.parseSchemaFieldsSection() {
					if i, ok := fieldsByName[field.Name]; ok {
						s.Fields[i] = mergeSchemaField(s.Fields[i], field)
						continue
					}
					fieldsByName[field.Name] = len(s.Fields)
					s.Fields = append(s.Fields, field)
				}
			default:
				if s.Sections == nil {
					s.Sections = map[string]Value{}
				}
				s.Sections[name.text] = &Object{Fields: p.parseNodes(tokRBrace), Span: name.span}
			}
			continue
		}
		field := p.parseSchemaField()
		if i, ok := fieldsByName[field.Name]; ok {
			s.Fields[i] = mergeSchemaField(s.Fields[i], field)
			continue
		}
		fieldsByName[field.Name] = len(s.Fields)
		s.Fields = append(s.Fields, field)
	}
}

func (p *parser) parseSchemaOptionsBlock() map[string]Value {
	options := map[string]Value{}
	for {
		p.skipNewlines()
		if p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
			if p.peek().kind == tokRBrace {
				p.next()
			}
			return options
		}
		name := p.expect(tokIdent, "expected schema option name")
		if p.peek().kind == tokEqual {
			p.next()
		}
		if p.peek().kind == tokNewline || p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
			options[name.text] = &Literal{Type: "bool", Data: true, Span: name.span}
			continue
		}
		options[name.text] = p.parseValueUntilLine()
	}
}

func (p *parser) parseSchemaFieldsSection() []SchemaField {
	var fields []SchemaField
	for {
		p.skipNewlines()
		if p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
			if p.peek().kind == tokRBrace {
				p.next()
			}
			return fields
		}
		fields = append(fields, p.parseSchemaSectionField())
	}
}

func (p *parser) parseSchemaSectionField() SchemaField {
	name := p.parseSchemaFieldName()
	field := SchemaField{Name: name.text, Required: false, Span: name.span}
	field.Type = p.parseSchemaType()
	for p.peek().kind != tokNewline && p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
		if p.peek().kind == tokLBrace {
			p.next()
			for {
				p.skipNewlines()
				if p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
					if p.peek().kind == tokRBrace {
						field.Span = spanJoin(field.Span, p.next().span)
					}
					break
				}
				if p.peek().kind == tokIdent && (p.peek().text == "required" || p.peek().text == "optional") {
					field.Fields = append(field.Fields, p.parseSchemaField())
					continue
				}
				if p.peek().kind == tokIdent && p.parseSchemaFieldBlockClause(&field) {
					continue
				}
				p.skipLineTail()
			}
			continue
		}
		if p.peek().kind != tokIdent {
			p.next()
			continue
		}
		switch p.peek().text {
		case "required":
			p.next()
			field.Required = true
		case "optional":
			p.next()
			field.Required = false
		default:
			if !p.parseSchemaFieldBlockClause(&field) {
				p.skipLineTail()
			}
		}
	}
	return field
}

func (p *parser) parseLegacySchemaTypeClause(s *SchemaDecl, fieldsByName map[string]int) {
	p.next()
	name := p.parseSchemaFieldName()
	typ := p.parseSchemaType()
	if typ == "" {
		typ = "any"
	}
	field := SchemaField{Name: name.text, Type: typ, Span: name.span}
	if i, ok := fieldsByName[field.Name]; ok {
		s.Fields[i] = mergeSchemaField(s.Fields[i], field)
		return
	}
	fieldsByName[field.Name] = len(s.Fields)
	s.Fields = append(s.Fields, field)
}

func mergeSchemaField(base, next SchemaField) SchemaField {
	if next.Required {
		base.Required = true
	}
	if base.Type == "" || base.Type == "any" {
		base.Type = next.Type
	}
	if next.Ref != "" {
		base.Ref = next.Ref
	}
	if next.Const != nil {
		base.Const = next.Const
	}
	if next.Default != nil {
		base.Default = next.Default
	}
	if len(next.Enum) > 0 {
		base.Enum = next.Enum
	}
	if len(next.Fields) > 0 {
		base.Fields = next.Fields
	}
	if next.Items != "" {
		base.Items = next.Items
	}
	if len(next.PrefixItems) > 0 {
		base.PrefixItems = next.PrefixItems
	}
	if next.Contains != "" {
		base.Contains = next.Contains
	}
	if next.Description != "" {
		base.Description = next.Description
	}
	if next.Title != "" {
		base.Title = next.Title
	}
	if next.Deprecated != "" {
		base.Deprecated = next.Deprecated
	}
	base.Sensitive = base.Sensitive || next.Sensitive
	base.Generated = base.Generated || next.Generated
	base.Derived = base.Derived || next.Derived
	base.ReadOnly = base.ReadOnly || next.ReadOnly
	base.WriteOnly = base.WriteOnly || next.WriteOnly
	base.Nullable = base.Nullable || next.Nullable
	base.UniqueItems = base.UniqueItems || next.UniqueItems
	if next.ClosedSet {
		base.ClosedSet = true
		base.Closed = next.Closed
	}
	if next.AdditionalProperties != nil {
		base.AdditionalProperties = next.AdditionalProperties
	}
	if next.Min != nil {
		base.Min = next.Min
	}
	if next.Max != nil {
		base.Max = next.Max
	}
	if next.ExclusiveMin != nil {
		base.ExclusiveMin = next.ExclusiveMin
	}
	if next.ExclusiveMax != nil {
		base.ExclusiveMax = next.ExclusiveMax
	}
	if next.MultipleOf != nil {
		base.MultipleOf = next.MultipleOf
	}
	if next.MinLen != nil {
		base.MinLen = next.MinLen
	}
	if next.MaxLen != nil {
		base.MaxLen = next.MaxLen
	}
	if next.MinItems != nil {
		base.MinItems = next.MinItems
	}
	if next.MaxItems != nil {
		base.MaxItems = next.MaxItems
	}
	if next.MinProps != nil {
		base.MinProps = next.MinProps
	}
	if next.MaxProps != nil {
		base.MaxProps = next.MaxProps
	}
	if next.Pattern != "" {
		base.Pattern = next.Pattern
	}
	if next.Format != "" {
		base.Format = next.Format
	}
	if next.ContentEncoding != "" {
		base.ContentEncoding = next.ContentEncoding
	}
	if next.ContentMediaType != "" {
		base.ContentMediaType = next.ContentMediaType
	}
	if len(next.Examples) > 0 {
		base.Examples = next.Examples
	}
	if next.PatternProperties != nil {
		base.PatternProperties = next.PatternProperties
	}
	if next.DependentRequired != nil {
		base.DependentRequired = next.DependentRequired
	}
	if next.LTField != "" {
		base.LTField = next.LTField
	}
	if next.LTEField != "" {
		base.LTEField = next.LTEField
	}
	if next.GTField != "" {
		base.GTField = next.GTField
	}
	if next.GTEField != "" {
		base.GTEField = next.GTEField
	}
	if next.EqField != "" {
		base.EqField = next.EqField
	}
	if len(next.AllOf) > 0 {
		base.AllOf = next.AllOf
	}
	if len(next.AnyOf) > 0 {
		base.AnyOf = next.AnyOf
	}
	if len(next.OneOf) > 0 {
		base.OneOf = next.OneOf
	}
	if next.Not != "" {
		base.Not = next.Not
	}
	if next.If != "" {
		base.If = next.If
	}
	if next.Then != "" {
		base.Then = next.Then
	}
	if next.Else != "" {
		base.Else = next.Else
	}
	if next.Classification != "" {
		base.Classification = next.Classification
	}
	if next.Audit != "" {
		base.Audit = next.Audit
	}
	if next.Explain != "" {
		base.Explain = next.Explain
	}
	if next.PII != "" {
		base.PII = next.PII
	}
	if next.PolicyTag != "" {
		base.PolicyTag = next.PolicyTag
	}
	if next.Owner != "" {
		base.Owner = next.Owner
	}
	if next.Severity != "" {
		base.Severity = next.Severity
	}
	if len(next.Extensions) > 0 {
		if base.Extensions == nil {
			base.Extensions = map[string]Value{}
		}
		for k, v := range next.Extensions {
			base.Extensions[k] = v
		}
	}
	return base
}

func (p *parser) parseSchemaField() SchemaField {
	req := p.expect(tokIdent, "expected required or optional")
	field := SchemaField{Required: req.text == "required", Span: req.span}
	name := p.parseSchemaFieldName()
	field.Name = name.text
	field.Span = spanJoin(field.Span, name.span)
	field.Type = p.parseSchemaType()
	if field.Type == "" && p.peek().kind == tokIdent && p.peek().text == "enum" {
		field.Type = "enum"
	}
	for p.peek().kind != tokNewline && p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
		if p.peek().kind == tokLBrace {
			p.next()
			for {
				p.skipNewlines()
				if p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
					if p.peek().kind == tokRBrace {
						field.Span = spanJoin(field.Span, p.next().span)
					}
					break
				}
				if p.peek().kind == tokIdent && (p.peek().text == "required" || p.peek().text == "optional") {
					field.Fields = append(field.Fields, p.parseSchemaField())
					continue
				}
				if p.peek().kind == tokIdent && p.parseSchemaFieldBlockClause(&field) {
					continue
				}
				p.skipLineTail()
			}
			continue
		}
		if p.peek().kind != tokIdent {
			p.next()
			continue
		}
		switch p.peek().text {
		case "ref":
			p.next()
			field.Ref = p.schemaStringClause()
		case "const":
			p.next()
			field.Const = p.parseValue()
		case "enum":
			p.next()
			if l, ok := p.parseValue().(*List); ok {
				field.Enum = l.Items
			}
		case "default":
			p.next()
			field.Default = p.parseValue()
		case "description", "doc":
			p.next()
			field.Description = p.schemaStringClause()
		case "title":
			p.next()
			field.Title = p.schemaStringClause()
		case "deprecated":
			p.next()
			if p.peek().kind == tokNewline || p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
				field.Deprecated = "true"
			} else {
				field.Deprecated = p.schemaStringClause()
			}
		case "sensitive":
			p.next()
			field.Sensitive = true
		case "generated":
			p.next()
			field.Generated = true
		case "derived":
			p.next()
			field.Derived = true
		case "read_only":
			p.next()
			field.ReadOnly = true
		case "write_only":
			p.next()
			field.WriteOnly = true
		case "nullable":
			p.next()
			field.Nullable = true
		case "unique_items":
			p.next()
			field.UniqueItems = true
		case "closed":
			p.next()
			field.ClosedSet = true
			field.Closed = p.schemaBoolClause(true)
		case "additional_properties":
			p.next()
			v := p.schemaBoolClause(true)
			field.AdditionalProperties = &v
		case "min":
			p.next()
			field.Min = p.parseValue()
		case "max":
			p.next()
			field.Max = p.parseValue()
		case "exclusive_min":
			p.next()
			field.ExclusiveMin = p.parseValue()
		case "exclusive_max":
			p.next()
			field.ExclusiveMax = p.parseValue()
		case "multiple_of":
			p.next()
			field.MultipleOf = p.parseValue()
		case "min_len":
			p.next()
			field.MinLen = p.parseValue()
		case "max_len":
			p.next()
			field.MaxLen = p.parseValue()
		case "min_items":
			p.next()
			field.MinItems = p.parseValue()
		case "max_items":
			p.next()
			field.MaxItems = p.parseValue()
		case "min_props":
			p.next()
			field.MinProps = p.parseValue()
		case "max_props":
			p.next()
			field.MaxProps = p.parseValue()
		case "pattern":
			p.next()
			field.Pattern = p.schemaStringClause()
		case "format":
			p.next()
			field.Format = p.schemaStringClause()
		case "content_encoding":
			p.next()
			field.ContentEncoding = p.schemaStringClause()
		case "content_media_type":
			p.next()
			field.ContentMediaType = p.schemaStringClause()
		case "examples":
			p.next()
			if l, ok := p.parseValue().(*List); ok {
				field.Examples = l.Items
			}
		case "items":
			p.next()
			field.Items = p.parseSchemaType()
		case "prefix_items":
			p.next()
			field.PrefixItems = p.schemaTypeListClause()
		case "contains":
			p.next()
			field.Contains = p.parseSchemaType()
		case "pattern_properties":
			p.next()
			field.PatternProperties = p.parseValue()
		case "dependent_required":
			p.next()
			field.DependentRequired = p.parseValue()
		case "lt_field":
			p.next()
			field.LTField = p.schemaStringClause()
		case "lte_field":
			p.next()
			field.LTEField = p.schemaStringClause()
		case "gt_field":
			p.next()
			field.GTField = p.schemaStringClause()
		case "gte_field":
			p.next()
			field.GTEField = p.schemaStringClause()
		case "eq_field":
			p.next()
			field.EqField = p.schemaStringClause()
		case "all_of":
			p.next()
			field.AllOf = p.schemaTypeListClause()
		case "any_of":
			p.next()
			field.AnyOf = p.schemaTypeListClause()
		case "one_of":
			p.next()
			field.OneOf = p.schemaTypeListClause()
		case "not":
			p.next()
			field.Not = p.parseSchemaType()
		case "if":
			p.next()
			field.If = p.parseSchemaType()
		case "then":
			p.next()
			field.Then = p.parseSchemaType()
		case "else":
			p.next()
			field.Else = p.parseSchemaType()
		case "classification":
			p.next()
			field.Classification = p.schemaStringClause()
		case "audit":
			p.next()
			field.Audit = p.schemaStringClause()
		case "explain":
			p.next()
			field.Explain = p.schemaStringClause()
		case "pii":
			p.next()
			field.PII = p.schemaStringClause()
		case "policy_tag":
			p.next()
			field.PolicyTag = p.schemaStringClause()
		case "owner":
			p.next()
			field.Owner = p.schemaStringClause()
		case "severity":
			p.next()
			field.Severity = p.schemaStringClause()
		default:
			if strings.HasPrefix(p.peek().text, "x_") {
				key := p.next().text
				if field.Extensions == nil {
					field.Extensions = map[string]Value{}
				}
				field.Extensions[key] = p.parseValue()
				continue
			}
			p.skipLineTail()
		}
	}
	return field
}

func (p *parser) parseSchemaFieldName() token {
	first := p.expect(tokIdent, "expected field name")
	parts := []string{first.text}
	sp := first.span
	for p.peek().kind == tokDot {
		p.next()
		part := p.expect(tokIdent, "expected field path segment")
		parts = append(parts, part.text)
		sp = spanJoin(sp, part.span)
	}
	first.text = strings.Join(parts, ".")
	first.span = sp
	return first
}

func (p *parser) schemaBoolClause(defaultValue bool) bool {
	if p.peek().kind == tokNewline || p.peek().kind == tokRBrace || p.peek().kind == tokLBrace || p.peek().kind == tokEOF {
		return defaultValue
	}
	v := p.parseValue()
	if lit, ok := v.(*Literal); ok {
		if b, ok := lit.Data.(bool); ok {
			return b
		}
	}
	return defaultValue
}

func (p *parser) parseSchemaFieldBlockClause(field *SchemaField) bool {
	switch p.peek().text {
	case "ref":
		p.next()
		field.Ref = p.schemaStringClause()
	case "const":
		p.next()
		field.Const = p.parseValue()
	case "enum":
		p.next()
		if l, ok := p.parseValue().(*List); ok {
			field.Enum = l.Items
		}
	case "default":
		p.next()
		field.Default = p.parseValue()
	case "description", "doc":
		p.next()
		field.Description = p.schemaStringClause()
	case "title":
		p.next()
		field.Title = p.schemaStringClause()
	case "deprecated":
		p.next()
		if p.peek().kind == tokNewline || p.peek().kind == tokRBrace || p.peek().kind == tokEOF {
			field.Deprecated = "true"
		} else {
			field.Deprecated = p.schemaStringClause()
		}
	case "sensitive":
		p.next()
		field.Sensitive = true
	case "generated":
		p.next()
		field.Generated = true
	case "derived":
		p.next()
		field.Derived = true
	case "read_only":
		p.next()
		field.ReadOnly = true
	case "write_only":
		p.next()
		field.WriteOnly = true
	case "nullable":
		p.next()
		field.Nullable = true
	case "unique_items":
		p.next()
		field.UniqueItems = true
	case "closed":
		p.next()
		field.ClosedSet = true
		field.Closed = p.schemaBoolClause(true)
	case "additional_properties":
		p.next()
		v := p.schemaBoolClause(true)
		field.AdditionalProperties = &v
	case "min":
		p.next()
		field.Min = p.parseValue()
	case "max":
		p.next()
		field.Max = p.parseValue()
	case "exclusive_min":
		p.next()
		field.ExclusiveMin = p.parseValue()
	case "exclusive_max":
		p.next()
		field.ExclusiveMax = p.parseValue()
	case "multiple_of":
		p.next()
		field.MultipleOf = p.parseValue()
	case "min_len":
		p.next()
		field.MinLen = p.parseValue()
	case "max_len":
		p.next()
		field.MaxLen = p.parseValue()
	case "min_items":
		p.next()
		field.MinItems = p.parseValue()
	case "max_items":
		p.next()
		field.MaxItems = p.parseValue()
	case "min_props":
		p.next()
		field.MinProps = p.parseValue()
	case "max_props":
		p.next()
		field.MaxProps = p.parseValue()
	case "pattern":
		p.next()
		field.Pattern = p.schemaStringClause()
	case "format":
		p.next()
		field.Format = p.schemaStringClause()
	case "content_encoding":
		p.next()
		field.ContentEncoding = p.schemaStringClause()
	case "content_media_type":
		p.next()
		field.ContentMediaType = p.schemaStringClause()
	case "examples":
		p.next()
		if l, ok := p.parseValue().(*List); ok {
			field.Examples = l.Items
		}
	case "items":
		p.next()
		field.Items = p.parseSchemaType()
	case "prefix_items":
		p.next()
		field.PrefixItems = p.schemaTypeListClause()
	case "contains":
		p.next()
		field.Contains = p.parseSchemaType()
	case "pattern_properties":
		p.next()
		field.PatternProperties = p.parseValue()
	case "dependent_required":
		p.next()
		field.DependentRequired = p.parseValue()
	case "lt_field":
		p.next()
		field.LTField = p.schemaStringClause()
	case "lte_field":
		p.next()
		field.LTEField = p.schemaStringClause()
	case "gt_field":
		p.next()
		field.GTField = p.schemaStringClause()
	case "gte_field":
		p.next()
		field.GTEField = p.schemaStringClause()
	case "eq_field":
		p.next()
		field.EqField = p.schemaStringClause()
	case "all_of":
		p.next()
		field.AllOf = p.schemaTypeListClause()
	case "any_of":
		p.next()
		field.AnyOf = p.schemaTypeListClause()
	case "one_of":
		p.next()
		field.OneOf = p.schemaTypeListClause()
	case "not":
		p.next()
		field.Not = p.parseSchemaType()
	case "if":
		p.next()
		field.If = p.parseSchemaType()
	case "then":
		p.next()
		field.Then = p.parseSchemaType()
	case "else":
		p.next()
		field.Else = p.parseSchemaType()
	case "classification":
		p.next()
		field.Classification = p.schemaStringClause()
	case "audit":
		p.next()
		field.Audit = p.schemaStringClause()
	case "explain":
		p.next()
		field.Explain = p.schemaStringClause()
	case "pii":
		p.next()
		field.PII = p.schemaStringClause()
	case "policy_tag":
		p.next()
		field.PolicyTag = p.schemaStringClause()
	case "owner":
		p.next()
		field.Owner = p.schemaStringClause()
	case "severity":
		p.next()
		field.Severity = p.schemaStringClause()
	default:
		if strings.HasPrefix(p.peek().text, "x_") {
			key := p.next().text
			if field.Extensions == nil {
				field.Extensions = map[string]Value{}
			}
			field.Extensions[key] = p.parseValue()
			return true
		}
		return false
	}
	return true
}

func (p *parser) schemaTypeListClause() []string {
	if p.peek().kind != tokLBracket {
		if typ := p.parseSchemaType(); typ != "" {
			return []string{typ}
		}
		return nil
	}
	p.next()
	var out []string
	for {
		p.skipNewlines()
		if p.peek().kind == tokRBracket || p.peek().kind == tokEOF {
			if p.peek().kind == tokRBracket {
				p.next()
			}
			return out
		}
		if typ := p.parseSchemaType(); typ != "" {
			out = append(out, typ)
		} else {
			p.next()
		}
		p.skipNewlines()
		if p.peek().kind == tokComma {
			p.next()
		}
	}
}

func (p *parser) schemaStringClause() string {
	v := p.parseValue()
	switch x := v.(type) {
	case *Literal:
		return literalScalar(x)
	case *Reference:
		return x.Path
	default:
		return ""
	}
}

func (p *parser) parseSchemaType() string {
	var parts []string
	depth := 0
	for {
		t := p.peek()
		if t.kind == tokEOF || t.kind == tokNewline || t.kind == tokRBrace || t.kind == tokRBracket || t.kind == tokComma || t.kind == tokLBrace {
			break
		}
		if depth == 0 && t.kind == tokIdent && isSchemaClause(t.text) {
			break
		}
		if t.text == "<" {
			depth++
		}
		if t.text == ">" && depth > 0 {
			depth--
		}
		parts = append(parts, p.next().text)
	}
	if len(parts) == 0 {
		return "any"
	}
	return joinTypeParts(parts)
}

func isSchemaClause(s string) bool {
	switch s {
	case "required", "optional", "ref", "const", "enum", "default", "description", "doc", "title", "deprecated", "sensitive", "generated", "derived", "read_only", "write_only", "nullable", "unique_items", "closed", "additional_properties", "min", "max", "exclusive_min", "exclusive_max", "multiple_of", "min_len", "max_len", "min_items", "max_items", "min_props", "max_props", "pattern", "format", "content_encoding", "content_media_type", "examples", "items", "prefix_items", "contains", "pattern_properties", "dependent_required", "lt_field", "lte_field", "gt_field", "eq_field", "all_of", "any_of", "one_of", "not", "if", "then", "else", "classification", "audit", "explain", "pii", "policy_tag", "owner", "severity":
		return true
	default:
		if strings.HasPrefix(s, "x_") {
			return true
		}
		return false
	}
}

func (p *parser) parseValueUntilLine() Value {
	if p.isExpressionLine() {
		return p.parseExprLine()
	}
	return p.parseValue()
}

func (p *parser) parseValue() Value {
	t := p.next()
	switch t.kind {
	case tokString, tokHeredoc:
		return &Literal{Type: "string", Data: t.text, Span: t.span}
	case tokNumber:
		return parseNumber(t)
	case tokIdent:
		path := p.collectRef(t)
		switch t.text {
		case "true":
			return &Literal{Type: "bool", Data: true, Span: t.span}
		case "false":
			return &Literal{Type: "bool", Data: false, Span: t.span}
		case "null":
			return &Literal{Type: "null", Data: nil, Span: t.span}
		}
		if p.peek().kind == tokLParen {
			t.text = path
			return p.parseCall(t)
		}
		if path == t.text && !looksConstantName(path) {
			return &Literal{Type: "identifier", Data: path, Span: t.span}
		}
		return &Reference{Path: path, Span: t.span}
	case tokLBracket:
		return p.parseList(t)
	case tokLBrace:
		body := p.parseNodes(tokRBrace)
		return &Object{Fields: body, Span: t.span}
	default:
		p.error(t, "expected value")
		return &Literal{Type: "null", Data: nil, Span: t.span}
	}
}

func (p *parser) parseList(start token) Value {
	items := make([]Value, 0, 4)
	for {
		p.skipNewlines()
		if p.peek().kind == tokRBracket || p.peek().kind == tokEOF {
			end := p.next()
			return &List{Items: items, Tuple: len(items) > 0, Span: spanJoin(start.span, end.span)}
		}
		if p.peek().kind == tokLBrace {
			lb := p.next()
			items = append(items, &Object{Fields: p.parseNodes(tokRBrace), Span: lb.span})
		} else {
			items = append(items, p.parseValue())
		}
		p.skipNewlines()
		if p.peek().kind == tokComma {
			p.next()
		}
	}
}

func (p *parser) parseCall(name token) Value {
	p.next()
	call := &Call{Name: name.text, Args: make([]Value, 0, 2), Span: name.span}
	for {
		p.skipNewlines()
		if p.peek().kind == tokRParen || p.peek().kind == tokEOF {
			call.Span = spanJoin(call.Span, p.next().span)
			return call
		}
		call.Args = append(call.Args, p.parseValue())
		p.skipNewlines()
		if p.peek().kind == tokComma {
			p.next()
		}
	}
}

func (p *parser) parseExprLine() Value {
	start := p.peek().span
	depth := 0
	rawStart := start.Start.Offset
	rawEnd := start.End.Offset
	for {
		t := p.peek()
		if t.kind == tokEOF || (depth == 0 && (t.kind == tokNewline || t.kind == tokRBrace)) {
			break
		}
		if t.kind == tokLBrace || t.kind == tokLBracket || t.kind == tokLParen {
			depth++
		}
		if t.kind == tokRBrace || t.kind == tokRBracket || t.kind == tokRParen {
			depth--
		}
		rawEnd = p.next().span.End.Offset
	}
	end := start
	if p.pos > 0 {
		end = p.toks[p.pos-1].span
	}
	return &Expr{Raw: p.rawExpr(rawStart, rawEnd), Span: spanJoin(start, end)}
}

func (p *parser) isExpressionLine() bool {
	if p.peek().kind == tokIdent && p.peek().text == "match" {
		return true
	}
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		t := p.toks[i]
		if depth == 0 && (t.kind == tokNewline || t.kind == tokRBrace || t.kind == tokEOF) {
			return false
		}
		if depth == 0 && (t.kind == tokOperator || isExprOperator(t.text)) {
			return true
		}
		if t.kind == tokLBracket || t.kind == tokLParen {
			depth++
		}
		if t.kind == tokRBracket || t.kind == tokRParen {
			depth--
		}
	}
	return false
}

func blockValue(name string, body []Node, sp Span) Value {
	if name == "when" {
		return buildCondition("all", body, sp)
	}
	if name == "then" {
		return &Object{Fields: body, Span: sp}
	}
	if len(body) > 0 {
		allObjects := true
		objects := make([]Value, 0, len(body))
		for _, n := range body {
			if obj, ok := n.(*Object); ok {
				objects = append(objects, obj)
			} else {
				allObjects = false
				break
			}
		}
		if allObjects {
			return &List{Items: objects, Span: sp}
		}
	}
	allBare := len(body) > 0
	items := make([]Value, 0, len(body))
	for _, n := range body {
		a, ok := n.(*Assignment)
		if !ok || a.Value == nil || a.Name == "" {
			allBare = false
			break
		}
		if ref, ok := a.Value.(*Reference); ok && ref.Path == "" {
			items = append(items, &Literal{Type: "identifier", Data: a.Name, Span: a.Span})
		} else {
			allBare = false
			break
		}
	}
	if allBare {
		return &List{Items: items, Span: sp}
	}
	return &Object{Fields: body, Span: sp}
}

func buildCondition(op string, body []Node, sp Span) Value {
	cond := &Condition{Op: op, Span: sp}
	for _, n := range body {
		switch x := n.(type) {
		case *Assignment:
			if expr, ok := x.Value.(*Expr); ok {
				cond.Children = append(cond.Children, &Condition{Op: "expr", Expr: expr, Span: expr.Span})
			}
		case *Block:
			if x.Type == "all" || x.Type == "any" || x.Type == "not" || x.Type == "none" {
				if child, ok := buildCondition(x.Type, x.Body, x.Span).(*Condition); ok {
					cond.Children = append(cond.Children, child)
				}
			}
		}
	}
	if len(cond.Children) == 1 && op == "all" {
		return cond.Children[0]
	}
	return cond
}

func parseNumber(t token) Value {
	raw := t.text
	if looksDateTime(raw) {
		typ := "date"
		if strings.Contains(raw, "T") || strings.Count(raw, ":") > 0 {
			typ = "datetime"
		}
		return &Literal{Type: typ, Raw: raw, Data: raw, Span: t.span}
	}
	unitStart := len(raw)
	for i, r := range raw {
		if i > 0 && (r < '0' || r > '9') && r != '.' {
			unitStart = i
			break
		}
	}
	num, unit := raw[:unitStart], raw[unitStart:]
	if unit != "" {
		typ := "duration"
		if isByteUnit(unit) {
			typ = "bytes"
		}
		return &Literal{Type: typ, Raw: raw, Data: raw, Span: t.span}
	}
	if strings.Contains(num, ".") {
		f, _ := strconv.ParseFloat(num, 64)
		return &Literal{Type: "float", Raw: raw, Data: f, Span: t.span}
	}
	i, _ := strconv.ParseInt(num, 10, 64)
	return &Literal{Type: "int", Raw: raw, Data: i, Span: t.span}
}

func looksDateTime(s string) bool {
	if len(s) < 10 {
		return false
	}
	return len(s) >= 10 && s[4] == '-' && s[7] == '-'
}

func looksConstantName(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func (p *parser) collectRef(first token) string {
	parts := []string{first.text}
	for p.peek().kind == tokDot {
		p.next()
		parts = append(parts, p.expect(tokIdent, "expected reference segment").text)
	}
	return strings.Join(parts, ".")
}

func (p *parser) skipNewlines() {
	for p.peek().kind == tokNewline {
		p.next()
	}
}

func (p *parser) skipNodeSeparators() {
	for p.peek().kind == tokNewline || p.peek().kind == tokComma {
		p.next()
	}
}

func (p *parser) skipLineTail() {
	for p.peek().kind != tokNewline && p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
		p.next()
	}
}

func (p *parser) recoverLine() {
	for p.peek().kind != tokNewline && p.peek().kind != tokEOF {
		p.next()
	}
}

func (p *parser) expect(k tokenKind, msg string) token {
	t := p.peek()
	if t.kind != k {
		p.error(t, msg)
		return t
	}
	return p.next()
}

func (p *parser) next() token {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *parser) peek() token {
	if p.pos >= len(p.toks) {
		return token{kind: tokEOF}
	}
	return p.toks[p.pos]
}

func (p *parser) peekN(n int) token {
	if p.pos+n >= len(p.toks) {
		return token{kind: tokEOF}
	}
	return p.toks[p.pos+n]
}

func (p *parser) error(t token, msg string) {
	p.errs = append(p.errs, Diagnostic{Severity: "error", Message: msg, Span: t.span})
}

func (p *parser) rawExpr(start, end int) string {
	if start < 0 || end < start || end > len(p.source) {
		return ""
	}
	return strings.TrimSpace(p.source[start:end])
}

func spanJoin(a Span, b Span) Span {
	if a.File == "" {
		a.File = b.File
	}
	a.End = b.End
	return a
}

func isExprOperator(s string) bool {
	switch s {
	case "in", "not_in", "contains", "starts_with", "ends_with", "matches", "has", "has_any", "has_all", "between", "exists", "empty", "equals", "greater_than", "less_than", "greater_or_equal", "less_or_equal", "to", "and", "or":
		return true
	default:
		return false
	}
}

func isCommandStatement(s string) bool {
	switch s {
	case "map", "field", "include":
		return true
	default:
		return false
	}
}

func isKnownBlock(s string) bool {
	switch s {
	case "bcl", "namespace", "profile", "module", "override", "runtime", "evaluation", "audit", "context", "session", "all", "any", "not", "none":
		return true
	default:
		return false
	}
}

func isCapitalizedBlockName(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r != utf8.RuneError && unicode.IsUpper(r)
}

func isByteUnit(s string) bool {
	switch len(s) {
	case 1:
		return s[0] == 'B' || s[0] == 'b'
	case 2:
		return (s[1] == 'B' || s[1] == 'b') && (s[0] == 'K' || s[0] == 'k' || s[0] == 'M' || s[0] == 'm' || s[0] == 'G' || s[0] == 'g' || s[0] == 'T' || s[0] == 't')
	case 3:
		return (s[1] == 'I' || s[1] == 'i') && (s[2] == 'B' || s[2] == 'b') && (s[0] == 'K' || s[0] == 'k' || s[0] == 'M' || s[0] == 'm' || s[0] == 'G' || s[0] == 'g' || s[0] == 'T' || s[0] == 't')
	default:
		return false
	}
}

func joinTypeParts(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		if p == "," {
			b.WriteString(", ")
			continue
		}
		b.WriteString(p)
	}
	return b.String()
}

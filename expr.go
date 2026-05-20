package bcl

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

type EvalOptions struct {
	AllowHash     bool
	AllowEncoding bool
	AllowTime     bool
	Variables     map[string]any
}

func Eval(raw string, vars map[string]any) (any, error) {
	return evalExpr(raw, vars, nil)
}

func EvalExpr(raw string, opts *EvalOptions) (any, error) {
	var vars map[string]any
	if opts != nil {
		vars = opts.Variables
	}
	return evalExpr(raw, vars, opts)
}

func evalExpr(raw string, vars map[string]any, opts *EvalOptions) (any, error) {
	if opts == nil {
		opts = defaultEvalOptions()
	}
	prog, err := CompileExpression(raw)
	if err != nil {
		return nil, err
	}
	return prog.Eval(vars, opts)
}

var evalDefaults = EvalOptions{}

func defaultEvalOptions() *EvalOptions {
	return &evalDefaults
}

var exprTokenCache sync.Map
var regexCache sync.Map

var exprProgramCache = struct {
	sync.RWMutex
	m map[string]*ExpressionProgram
}{m: make(map[string]*ExpressionProgram)}

type ExpressionProgram struct {
	Raw   string
	Instr []exprInstr
	fast  exprFast
}

type Symbol string

const (
	SymbolANY  Symbol = "ANY"
	SymbolNONE Symbol = "NONE"
)

type OptionalValue struct {
	Present bool
	Value   any
}

type AllPattern struct {
	Pattern string
}

type exprFastKind byte

const (
	exprFastNone exprFastKind = iota
	exprFastUnary
	exprFastBinary
)

type exprOperandKind byte

const (
	exprOperandConst exprOperandKind = iota
	exprOperandRef
)

type exprOperand struct {
	kind  exprOperandKind
	value any
	parts []string
}

type exprFast struct {
	kind  exprFastKind
	op    string
	left  exprOperand
	right exprOperand
}

type exprOp byte

const (
	exprPushConst exprOp = iota
	exprPushRef
	exprMakeList
	exprCall
	exprBinary
	exprBetween
	exprExists
	exprEmpty
)

type exprInstr struct {
	op    exprOp
	text  string
	value any
	n     int
	parts []string
}

func CompileExpression(raw string) (*ExpressionProgram, error) {
	exprProgramCache.RLock()
	if prog, ok := exprProgramCache.m[raw]; ok {
		exprProgramCache.RUnlock()
		return prog, nil
	}
	exprProgramCache.RUnlock()
	toks, err := exprTokens(raw)
	if err != nil {
		return nil, err
	}
	_ = toks
	prog := &ExpressionProgram{Raw: raw}
	exprProgramCache.Lock()
	if existing, ok := exprProgramCache.m[raw]; ok {
		exprProgramCache.Unlock()
		return existing, nil
	}
	exprProgramCache.m[raw] = prog
	exprProgramCache.Unlock()
	return prog, nil
}

func (p *ExpressionProgram) Eval(vars map[string]any, opts *EvalOptions) (any, error) {
	if opts == nil {
		opts = defaultEvalOptions()
	}
	if vars == nil {
		vars = opts.Variables
	}
	if len(p.Instr) == 0 {
		return evalProgramRaw(p.Raw, vars, opts)
	}
	if p.fast.kind != exprFastNone {
		return p.evalFast(vars)
	}
	var stack [2]any
	sp := 0
	for _, in := range p.Instr {
		switch in.op {
		case exprPushConst:
			if sp >= len(stack) {
				return p.evalHeap(vars, opts)
			}
			stack[sp] = in.value
			sp++
		case exprPushRef:
			if sp >= len(stack) {
				return p.evalHeap(vars, opts)
			}
			stack[sp] = lookupParts(vars, in.parts)
			sp++
		case exprMakeList:
			start := sp - in.n
			xs := make([]any, in.n)
			copy(xs, stack[start:sp])
			clear(stack[start:sp])
			sp = start
			stack[sp] = xs
			sp++
		case exprCall:
			start := sp - in.n
			v, err := evalCall(in.text, stack[start:sp], opts)
			if err != nil {
				return nil, err
			}
			clear(stack[start:sp])
			sp = start
			stack[sp] = v
			sp++
		case exprBinary:
			b := stack[sp-1]
			a := stack[sp-2]
			stack[sp-1] = nil
			stack[sp-2] = nil
			sp -= 2
			v, err := evalOp(in.text, a, b)
			if err != nil {
				return nil, err
			}
			stack[sp] = v
			sp++
		case exprBetween:
			hi := stack[sp-1]
			lo := stack[sp-2]
			v := stack[sp-3]
			clear(stack[sp-3 : sp])
			sp -= 3
			stack[sp] = compare(v, lo) >= 0 && compare(v, hi) <= 0
			sp++
		case exprExists:
			i := sp - 1
			stack[i] = stack[i] != nil
		case exprEmpty:
			i := sp - 1
			stack[i] = isEmpty(stack[i])
		}
	}
	if sp == 0 {
		return nil, nil
	}
	return stack[sp-1], nil
}

func (p *ExpressionProgram) evalFast(vars map[string]any) (any, error) {
	left := p.fast.left.eval(vars)
	switch p.fast.kind {
	case exprFastUnary:
		if p.fast.op == "exists" {
			return left != nil, nil
		}
		return isEmpty(left), nil
	case exprFastBinary:
		right := p.fast.right.eval(vars)
		return evalOp(p.fast.op, left, right)
	default:
		return nil, nil
	}
}

func (o exprOperand) eval(vars map[string]any) any {
	if o.kind == exprOperandRef {
		return lookupParts(vars, o.parts)
	}
	return o.value
}

func (p *ExpressionProgram) evalHeap(vars map[string]any, opts *EvalOptions) (any, error) {
	stack := make([]any, 0, len(p.Instr))
	for _, in := range p.Instr {
		switch in.op {
		case exprPushConst:
			stack = append(stack, in.value)
		case exprPushRef:
			stack = append(stack, lookupParts(vars, in.parts))
		case exprMakeList:
			start := len(stack) - in.n
			xs := make([]any, in.n)
			copy(xs, stack[start:])
			stack = stack[:start]
			stack = append(stack, xs)
		case exprCall:
			start := len(stack) - in.n
			v, err := evalCall(in.text, stack[start:], opts)
			if err != nil {
				return nil, err
			}
			stack = stack[:start]
			stack = append(stack, v)
		case exprBinary:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			v, err := evalOp(in.text, a, b)
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case exprBetween:
			hi := stack[len(stack)-1]
			lo := stack[len(stack)-2]
			v := stack[len(stack)-3]
			stack = stack[:len(stack)-3]
			stack = append(stack, compare(v, lo) >= 0 && compare(v, hi) <= 0)
		case exprExists:
			i := len(stack) - 1
			stack[i] = stack[i] != nil
		case exprEmpty:
			i := len(stack) - 1
			stack[i] = isEmpty(stack[i])
		}
	}
	if len(stack) == 0 {
		return nil, nil
	}
	return stack[len(stack)-1], nil
}

type exprCompiler struct {
	toks []token
	pos  int
}

func (c *exprCompiler) primary() ([]exprInstr, any, bool, error) {
	t := c.next()
	switch t.kind {
	case tokString:
		return nil, t.text, true, nil
	case tokNumber:
		return nil, parseNumber(t).ToInterface(false), true, nil
	case tokLBracket:
		var code []exprInstr
		var consts []any
		allConst := true
		count := 0
		for c.peek().kind != tokRBracket && c.peek().kind != tokEOF {
			itemCode, itemConst, itemIsConst, err := c.primary()
			if err != nil {
				return nil, nil, false, err
			}
			count++
			if allConst && itemIsConst {
				consts = append(consts, itemConst)
			} else {
				if allConst {
					code = make([]exprInstr, 0, count+2)
					for _, v := range consts {
						code = append(code, exprInstr{op: exprPushConst, value: v})
					}
					allConst = false
				}
				code = append(code, materializeExpr(itemCode, itemConst, itemIsConst)...)
			}
			if c.peek().kind == tokComma {
				c.next()
			}
		}
		if c.peek().kind == tokRBracket {
			c.next()
		}
		if allConst {
			return nil, consts, true, nil
		}
		code = append(code, exprInstr{op: exprMakeList, n: count})
		return code, nil, false, nil
	case tokIdent:
		if t.text == "true" && c.peek().kind != tokDot {
			return nil, true, true, nil
		}
		if t.text == "false" && c.peek().kind != tokDot {
			return nil, false, true, nil
		}
		if t.text == "null" && c.peek().kind != tokDot {
			return nil, nil, true, nil
		}
		parts := c.collectParts(t)
		if len(parts) == 2 && parts[0] == "time" && parts[1] == "now" && c.peek().kind != tokLParen {
			return []exprInstr{{op: exprCall, text: "now"}}, nil, false, nil
		}
		if c.peek().kind == tokLParen {
			name := strings.Join(parts, ".")
			return c.call(name)
		}
		return []exprInstr{{op: exprPushRef, parts: parts}}, nil, false, nil
	default:
		return nil, nil, false, fmt.Errorf("unexpected expression token %q", t.text)
	}
}

func (c *exprCompiler) call(name string) ([]exprInstr, any, bool, error) {
	c.next()
	var code []exprInstr
	var constArgs []any
	allConst := true
	count := 0
	for c.peek().kind != tokRParen && c.peek().kind != tokEOF {
		argCode, argConst, argIsConst, err := c.primary()
		if err != nil {
			return nil, nil, false, err
		}
		count++
		if allConst && argIsConst {
			constArgs = append(constArgs, argConst)
		} else {
			if allConst {
				code = make([]exprInstr, 0, count+2)
				for _, v := range constArgs {
					code = append(code, exprInstr{op: exprPushConst, value: v})
				}
				allConst = false
			}
			code = append(code, materializeExpr(argCode, argConst, argIsConst)...)
		}
		if c.peek().kind == tokComma {
			c.next()
		}
	}
	if c.peek().kind == tokRParen {
		c.next()
	}
	if allConst && pureConstCall(name) {
		v, err := evalCall(name, constArgs, defaultEvalOptions())
		return nil, v, err == nil, err
	}
	if allConst {
		code = make([]exprInstr, 0, len(constArgs)+1)
		for _, v := range constArgs {
			code = append(code, exprInstr{op: exprPushConst, value: v})
		}
	}
	code = append(code, exprInstr{op: exprCall, text: name, n: count})
	return code, nil, false, nil
}

func pureConstCall(name string) bool {
	switch name {
	case "lower", "upper", "trim", "len", "contains", "regex", "cidr", "ip", "duration", "concat", "coalesce":
		return true
	default:
		return false
	}
}

func materializeExpr(code []exprInstr, constValue any, isConst bool) []exprInstr {
	if isConst {
		return []exprInstr{{op: exprPushConst, value: constValue}}
	}
	return code
}

func (c *exprCompiler) collectParts(first token) []string {
	if c.peek().kind != tokDot {
		return []string{first.text}
	}
	parts := []string{first.text}
	for c.peek().kind == tokDot {
		c.next()
		parts = append(parts, c.next().text)
	}
	return parts
}

func (c *exprCompiler) next() token {
	t := c.peek()
	if c.pos < len(c.toks) {
		c.pos++
	}
	return t
}

func (c *exprCompiler) peek() token {
	if c.pos >= len(c.toks) {
		return token{kind: tokEOF}
	}
	return c.toks[c.pos]
}

func exprTokens(raw string) ([]token, error) {
	if cached, ok := exprTokenCache.Load(raw); ok {
		return cached.([]token), nil
	}
	toks, errs := lexString("<expr>", raw)
	if len(errs) > 0 {
		return nil, errs
	}
	exprTokenCache.Store(raw, toks)
	return toks, nil
}

type exprParser struct {
	toks []token
	pos  int
	vars map[string]any
	opts *EvalOptions
}

func evalProgramRaw(raw string, vars map[string]any, opts *EvalOptions) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "match ") || strings.HasPrefix(raw, "match(") {
		return evalMatchRaw(raw, vars, opts)
	}
	toks, err := exprTokens(raw)
	if err != nil {
		return nil, err
	}
	return (&exprParser{toks: toks, vars: vars, opts: opts}).parse()
}

func (e *exprParser) parse() (any, error) {
	return e.parseExpr(0)
}

func (e *exprParser) parseExpr(minPrec int) (any, error) {
	left, err := e.prefix()
	if err != nil {
		return nil, err
	}
	for {
		t := e.peek()
		if t.kind == tokEOF || t.kind == tokNewline || t.kind == tokRBrace || t.kind == tokRParen || t.kind == tokRBracket || t.kind == tokComma {
			return left, nil
		}
		if t.text == "exists" || t.text == "empty" {
			if 8 < minPrec {
				return left, nil
			}
			e.next()
			if t.text == "exists" {
				left = left != nil
			} else {
				left = isEmpty(left)
			}
			continue
		}
		if t.text == "between" {
			if 4 < minPrec {
				return left, nil
			}
			e.next()
			lo, err := e.parseExpr(5)
			if err != nil {
				return nil, err
			}
			if e.peek().text == "and" {
				e.next()
			}
			hi, err := e.parseExpr(5)
			if err != nil {
				return nil, err
			}
			left = compare(left, lo) >= 0 && compare(left, hi) <= 0
			continue
		}
		if t.text == "?" {
			if 1 < minPrec {
				return left, nil
			}
			e.next()
			thenVal, err := e.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if e.peek().text != ":" {
				return nil, fmt.Errorf("expected ':' in ternary expression")
			}
			e.next()
			elseVal, err := e.parseExpr(1)
			if err != nil {
				return nil, err
			}
			if truthy(left) {
				left = thenVal
			} else {
				left = elseVal
			}
			continue
		}
		op := t.text
		prec, ok := infixPrecedence(op)
		if !ok || prec < minPrec {
			return left, nil
		}
		e.next()
		right, err := e.parseExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		left, err = evalOp(op, left, right)
		if err != nil {
			return nil, err
		}
	}
}

func (e *exprParser) prefix() (any, error) {
	t := e.next()
	switch t.kind {
	case tokString:
		return t.text, nil
	case tokNumber:
		return parseNumber(t).ToInterface(false), nil
	case tokOperator:
		switch t.text {
		case "!":
			v, err := e.parseExpr(8)
			return !truthy(v), err
		case "-":
			v, err := e.parseExpr(8)
			if err != nil {
				return nil, err
			}
			f, ok := num(v)
			if !ok {
				return nil, fmt.Errorf("unary - requires numeric value")
			}
			return -f, nil
		}
		return nil, fmt.Errorf("unexpected expression token %q", t.text)
	case tokLParen:
		v, err := e.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if e.peek().kind == tokRParen {
			e.next()
		}
		return v, nil
	case tokLBracket:
		var out []any
		for e.peek().kind != tokRBracket && e.peek().kind != tokEOF {
			v, err := e.parseExpr(0)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			if e.peek().kind == tokComma {
				e.next()
			}
		}
		if e.peek().kind == tokRBracket {
			e.next()
		}
		return out, nil
	case tokIdent:
		if t.text == "true" && e.peek().kind != tokDot {
			return true, nil
		}
		if t.text == "false" && e.peek().kind != tokDot {
			return false, nil
		}
		if t.text == "null" && e.peek().kind != tokDot {
			return nil, nil
		}
		if t.text == "ANY" && e.peek().kind != tokDot && e.peek().kind != tokLParen {
			return SymbolANY, nil
		}
		if t.text == "NONE" && e.peek().kind != tokDot && e.peek().kind != tokLParen {
			return OptionalValue{Present: false}, nil
		}
		if e.isTimeNow(t) {
			if !e.opts.AllowTime {
				return nil, fmt.Errorf("time.now requires time capability")
			}
			return time.Now().UTC().Format(time.RFC3339), nil
		}
		if e.isDottedCall() {
			path := e.collectRef(t)
			return e.call(path)
		}
		if e.peek().kind == tokLParen {
			return e.call(t.text)
		}
		return e.lookupRef(t), nil
	default:
		return nil, fmt.Errorf("unexpected expression token %q", t.text)
	}
}

func (e *exprParser) call(name string) (any, error) {
	e.next()
	var args []any
	for e.peek().kind != tokRParen && e.peek().kind != tokEOF {
		v, err := e.parseExpr(0)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
		if e.peek().kind == tokComma {
			e.next()
		}
	}
	if e.peek().kind == tokRParen {
		e.next()
	}
	return evalCall(name, args, e.opts)
}

func infixPrecedence(op string) (int, bool) {
	switch op {
	case "or":
		return 2, true
	case "and":
		return 3, true
	case "==", "!=", ">", ">=", "<", "<=", "in", "not_in", "contains", "starts_with", "ends_with", "matches", "has", "has_any", "has_all", "equals", "greater_than", "less_than", "greater_or_equal", "less_or_equal":
		return 4, true
	case "+", "-":
		return 5, true
	case "*", "/", "%":
		return 6, true
	default:
		return 0, false
	}
}

func evalCall(name string, args []any, opts *EvalOptions) (any, error) {
	if opts == nil {
		opts = defaultEvalOptions()
	}
	switch name {
	case "SOME":
		if len(args) != 1 {
			return nil, fmt.Errorf("SOME requires 1 argument")
		}
		return OptionalValue{Present: true, Value: args[0]}, nil
	case "ALL":
		if len(args) != 1 {
			return nil, fmt.Errorf("ALL requires 1 argument")
		}
		return AllPattern{Pattern: fmt.Sprint(args[0])}, nil
	case "lower":
		if len(args) != 1 {
			return nil, fmt.Errorf("lower requires 1 argument")
		}
		return strings.ToLower(fmt.Sprint(args[0])), nil
	case "upper":
		if len(args) != 1 {
			return nil, fmt.Errorf("upper requires 1 argument")
		}
		return strings.ToUpper(fmt.Sprint(args[0])), nil
	case "trim":
		if len(args) != 1 {
			return nil, fmt.Errorf("trim requires 1 argument")
		}
		return strings.TrimSpace(fmt.Sprint(args[0])), nil
	case "len", "length":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires 1 argument", name)
		}
		return length(args[0]), nil
	case "replace":
		if len(args) != 3 {
			return nil, fmt.Errorf("replace requires 3 arguments")
		}
		return strings.ReplaceAll(fmt.Sprint(args[0]), fmt.Sprint(args[1]), fmt.Sprint(args[2])), nil
	case "split":
		if len(args) != 2 {
			return nil, fmt.Errorf("split requires 2 arguments")
		}
		parts := strings.Split(fmt.Sprint(args[0]), fmt.Sprint(args[1]))
		out := make([]any, 0, len(parts))
		for _, part := range parts {
			out = append(out, part)
		}
		return out, nil
	case "join":
		if len(args) != 2 {
			return nil, fmt.Errorf("join requires 2 arguments")
		}
		var parts []string
		switch xs := args[0].(type) {
		case []any:
			for _, x := range xs {
				parts = append(parts, fmt.Sprint(x))
			}
		case []string:
			parts = xs
		default:
			rv := reflect.ValueOf(args[0])
			if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
				return nil, fmt.Errorf("join requires a list")
			}
			for i := 0; i < rv.Len(); i++ {
				parts = append(parts, fmt.Sprint(rv.Index(i).Interface()))
			}
		}
		return strings.Join(parts, fmt.Sprint(args[1])), nil
	case "contains":
		if len(args) != 2 {
			return nil, fmt.Errorf("contains requires 2 arguments")
		}
		return contains(args[0], args[1]), nil
	case "regex":
		if len(args) != 1 {
			return nil, fmt.Errorf("regex requires 1 argument")
		}
		pattern := fmt.Sprint(args[0])
		if cached, ok := regexCache.Load(pattern); ok {
			return cached.(*regexp.Regexp), nil
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		regexCache.Store(pattern, re)
		return re, nil
	case "cidr":
		if len(args) != 1 {
			return nil, fmt.Errorf("cidr requires 1 argument")
		}
		_, ipnet, err := net.ParseCIDR(fmt.Sprint(args[0]))
		return ipnet, err
	case "ip":
		if len(args) != 1 {
			return nil, fmt.Errorf("ip requires 1 argument")
		}
		return net.ParseIP(fmt.Sprint(args[0])), nil
	case "time":
		if len(args) == 0 {
			if !opts.AllowTime {
				return nil, fmt.Errorf("time requires time capability")
			}
			return time.Now().UTC().Format("15:04:05"), nil
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("time requires 0 or 1 arguments")
		}
		return fmt.Sprint(args[0]), nil
	case "date":
		if len(args) == 0 {
			if !opts.AllowTime {
				return nil, fmt.Errorf("date requires time capability")
			}
			return time.Now().UTC().Format("2006-01-02"), nil
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("date requires 0 or 1 arguments")
		}
		return fmt.Sprint(args[0]), nil
	case "datetime", "timestamp":
		if len(args) == 0 {
			if !opts.AllowTime {
				return nil, fmt.Errorf("%s requires time capability", name)
			}
			return time.Now().UTC().Format(time.RFC3339), nil
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires 0 or 1 arguments", name)
		}
		return fmt.Sprint(args[0]), nil
	case "today":
		if len(args) != 0 {
			return nil, fmt.Errorf("today requires 0 arguments")
		}
		if !opts.AllowTime {
			return nil, fmt.Errorf("today requires time capability")
		}
		return time.Now().UTC().Format("2006-01-02"), nil
	case "duration":
		if len(args) != 1 {
			return nil, fmt.Errorf("duration requires 1 argument")
		}
		return time.ParseDuration(fmt.Sprint(args[0]))
	case "now":
		if len(args) != 0 {
			return nil, fmt.Errorf("now requires 0 arguments")
		}
		if !opts.AllowTime {
			return nil, fmt.Errorf("now requires time capability")
		}
		return time.Now().UTC().Format(time.RFC3339), nil
	case "uuid":
		if len(args) != 0 {
			return nil, fmt.Errorf("uuid requires 0 arguments")
		}
		return randomUUID()
	case "unique_id":
		prefix := "id"
		if len(args) > 0 && fmt.Sprint(args[0]) != "" {
			prefix = fmt.Sprint(args[0])
		}
		id, err := randomHex(12)
		if err != nil {
			return nil, err
		}
		return prefix + "_" + id, nil
	case "json":
		if len(args) != 1 {
			return nil, fmt.Errorf("json requires 1 argument")
		}
		b, err := json.Marshal(args[0])
		if err != nil {
			return nil, err
		}
		return string(b), nil
	case "concat":
		var b strings.Builder
		for _, a := range args {
			b.WriteString(fmt.Sprint(a))
		}
		return b.String(), nil
	case "coalesce":
		for _, a := range args {
			if a != nil && !isEmpty(a) {
				return a, nil
			}
		}
		return nil, nil
	case "base64":
		if len(args) != 1 {
			return nil, fmt.Errorf("base64 requires 1 argument")
		}
		if !opts.AllowEncoding {
			return nil, fmt.Errorf("base64 requires encoding capability")
		}
		return base64.StdEncoding.EncodeToString([]byte(fmt.Sprint(args[0]))), nil
	case "sha256":
		if len(args) != 1 {
			return nil, fmt.Errorf("sha256 requires 1 argument")
		}
		if !opts.AllowHash {
			return nil, fmt.Errorf("sha256 requires hash capability")
		}
		sum := sha256.Sum256([]byte(fmt.Sprint(args[0])))
		return hex.EncodeToString(sum[:]), nil
	default:
		if len(args) == 1 {
			return args[0], nil
		}
		return map[string]any{"$call": name, "args": args}, nil
	}
}

func evalOp(op string, a, b any) (any, error) {
	switch op {
	case "equals":
		return evalOp("==", a, b)
	case "greater_than":
		return evalOp(">", a, b)
	case "less_than":
		return evalOp("<", a, b)
	case "greater_or_equal":
		return evalOp(">=", a, b)
	case "less_or_equal":
		return evalOp("<=", a, b)
	case "==":
		return reflect.DeepEqual(a, b), nil
	case "!=":
		return !reflect.DeepEqual(a, b), nil
	case "+":
		if af, ok := num(a); ok {
			if bf, bok := num(b); bok {
				return af + bf, nil
			}
		}
		return fmt.Sprint(a) + fmt.Sprint(b), nil
	case "-":
		af, aok := num(a)
		bf, bok := num(b)
		if !aok || !bok {
			return nil, fmt.Errorf("- requires numeric values")
		}
		return af - bf, nil
	case "*":
		af, aok := num(a)
		bf, bok := num(b)
		if !aok || !bok {
			return nil, fmt.Errorf("* requires numeric values")
		}
		return af * bf, nil
	case "/":
		af, aok := num(a)
		bf, bok := num(b)
		if !aok || !bok {
			return nil, fmt.Errorf("/ requires numeric values")
		}
		if bf == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return af / bf, nil
	case "%":
		ai, aok := intScalarValue(a)
		bi, bok := intScalarValue(b)
		if !aok || !bok {
			return nil, fmt.Errorf("%% requires integer values")
		}
		if bi == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return ai % bi, nil
	case ">", ">=", "<", "<=":
		c := compare(a, b)
		switch op {
		case ">":
			return c > 0, nil
		case ">=":
			return c >= 0, nil
		case "<":
			return c < 0, nil
		default:
			return c <= 0, nil
		}
	case "in", "contains", "has":
		if ipnet, ok := b.(*net.IPNet); ok {
			return ipnet.Contains(net.ParseIP(fmt.Sprint(a))), nil
		}
		return contains(b, a) || contains(a, b), nil
	case "not_in":
		ok, _ := evalOp("in", a, b)
		return !ok.(bool), nil
	case "starts_with":
		return strings.HasPrefix(fmt.Sprint(a), fmt.Sprint(b)), nil
	case "ends_with":
		return strings.HasSuffix(fmt.Sprint(a), fmt.Sprint(b)), nil
	case "matches":
		if isPatternValue(b) {
			binds := map[string]any{}
			return matchPatternValue(a, b, binds), nil
		}
		re, ok := b.(*regexp.Regexp)
		if !ok {
			var err error
			re, err = regexp.Compile(fmt.Sprint(b))
			if err != nil {
				return false, err
			}
		}
		return re.MatchString(fmt.Sprint(a)), nil
	case "has_any":
		return hasAny(a, b), nil
	case "has_all":
		return hasAll(a, b), nil
	case "and":
		return truthy(a) && truthy(b), nil
	case "or":
		return truthy(a) || truthy(b), nil
	default:
		return nil, fmt.Errorf("unsupported operator %q", op)
	}
}

func evalMatchRaw(raw string, vars map[string]any, opts *EvalOptions) (any, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "match(") {
		return evalMatchCallRaw(raw, vars, opts)
	}
	open := strings.IndexByte(raw, '{')
	if open < 0 || !strings.HasPrefix(raw, "match ") {
		return nil, fmt.Errorf("malformed match expression")
	}
	subjectExpr := strings.TrimSpace(raw[len("match "):open])
	close := strings.LastIndexByte(raw, '}')
	if close < open {
		return nil, fmt.Errorf("malformed match expression")
	}
	subject, err := evalProgramRaw(subjectExpr, vars, opts)
	if err != nil {
		return nil, err
	}
	for _, rawCase := range splitMatchCases(raw[open+1 : close]) {
		pat, guard, result, err := parseRawCase(rawCase)
		if err != nil {
			return nil, err
		}
		binds := map[string]any{}
		if !matchPatternRaw(subject, pat, binds) {
			continue
		}
		caseVars := mergeVars(vars, binds)
		if guard != "" {
			ok, err := evalProgramRaw(guard, caseVars, opts)
			if err != nil {
				return nil, err
			}
			if !truthy(ok) {
				continue
			}
		}
		return evalProgramRaw(result, caseVars, opts)
	}
	return nil, nil
}

func evalMatchCallRaw(raw string, vars map[string]any, opts *EvalOptions) (any, error) {
	inner := strings.TrimSpace(raw[len("match("):])
	if !strings.HasSuffix(inner, ")") {
		return nil, fmt.Errorf("malformed match call")
	}
	args := splitTopLevel(inner[:len(inner)-1], ',')
	if len(args) < 2 {
		return nil, fmt.Errorf("match requires a value and at least one case")
	}
	subject, err := evalProgramRaw(args[0], vars, opts)
	if err != nil {
		return nil, err
	}
	for _, arg := range args[1:] {
		arg = strings.TrimSpace(arg)
		if !strings.HasPrefix(arg, "case(") || !strings.HasSuffix(arg, ")") {
			v, err := evalProgramRaw(arg, vars, opts)
			if err != nil {
				return nil, err
			}
			return v, nil
		}
		parts := splitTopLevel(arg[len("case("):len(arg)-1], ',')
		if len(parts) < 2 {
			return nil, fmt.Errorf("case requires pattern and result")
		}
		pat, guard := splitPatternGuard(parts[0])
		result := strings.Join(parts[1:], ",")
		binds := map[string]any{}
		if !matchPatternRaw(subject, pat, binds) {
			continue
		}
		caseVars := mergeVars(vars, binds)
		if guard != "" {
			ok, err := evalProgramRaw(guard, caseVars, opts)
			if err != nil {
				return nil, err
			}
			if !truthy(ok) {
				continue
			}
		}
		return evalProgramRaw(result, caseVars, opts)
	}
	return nil, nil
}

func parseRawCase(raw string) (pattern, guard, result string, err error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "case ") {
		raw = strings.TrimSpace(raw[len("case "):])
	}
	arrow := findTopLevelArrow(raw)
	if arrow < 0 {
		return "", "", "", fmt.Errorf("case missing =>")
	}
	pattern, guard = splitPatternGuard(raw[:arrow])
	result = strings.TrimSpace(raw[arrow+2:])
	if pattern == "" || result == "" {
		return "", "", "", fmt.Errorf("case requires pattern and result")
	}
	return pattern, guard, result, nil
}

func splitPatternGuard(raw string) (string, string) {
	parts := splitTopLevelWord(raw, "if")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(raw), ""
}

func splitMatchCases(body string) []string {
	var cases []string
	depth := 0
	quote := rune(0)
	start := -1
	for i, r := range body {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.HasPrefix(body[i:], "case ") {
			if start >= 0 {
				cases = append(cases, strings.TrimSpace(body[start:i]))
			}
			start = i
		}
	}
	if start >= 0 {
		cases = append(cases, strings.TrimSpace(body[start:]))
	}
	return cases
}

func findTopLevelArrow(s string) int {
	depth := 0
	quote := rune(0)
	for i, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth == 0 && i+1 < len(s) && s[i+1] == '>' {
				return i
			}
		}
	}
	return -1
}

func splitTopLevel(s string, sep rune) []string {
	var out []string
	depth := 0
	quote := rune(0)
	start := 0
	for i, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + len(string(r))
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

func splitTopLevelWord(s, word string) []string {
	depth := 0
	quote := rune(0)
	for i, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.HasPrefix(s[i:], word) && isWordBoundary(s, i-1) && isWordBoundary(s, i+len(word)) {
			return []string{s[:i], s[i+len(word):]}
		}
	}
	return []string{s}
}

func isWordBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	return !(c == '_' || c == '-' || c == '.' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z')
}

func matchPatternRaw(v any, pattern string, binds map[string]any) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	for _, alt := range splitTopLevel(pattern, '|') {
		if alt == pattern {
			break
		}
		local := map[string]any{}
		if matchPatternRaw(v, alt, local) {
			for k, val := range local {
				binds[k] = val
			}
			return true
		}
	}
	if pattern == "_" || pattern == "ANY" {
		return true
	}
	if pattern == "NONE" {
		if opt, ok := v.(OptionalValue); ok {
			return !opt.Present
		}
		return v == nil || v == SymbolNONE
	}
	if strings.HasPrefix(pattern, "SOME(") && strings.HasSuffix(pattern, ")") {
		opt, ok := v.(OptionalValue)
		if !ok || !opt.Present {
			return false
		}
		return matchPatternRaw(opt.Value, pattern[len("SOME("):len(pattern)-1], binds)
	}
	if strings.HasPrefix(pattern, "ALL(") && strings.HasSuffix(pattern, ")") {
		xs, ok := sliceValues(v)
		if !ok {
			return false
		}
		inner := pattern[len("ALL(") : len(pattern)-1]
		for _, item := range xs {
			if !matchPatternRaw(item, inner, map[string]any{}) {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(pattern, "{") && strings.HasSuffix(pattern, "}") {
		return matchObjectPattern(v, pattern[1:len(pattern)-1], binds)
	}
	if strings.HasPrefix(pattern, "[") && strings.HasSuffix(pattern, "]") {
		return matchListPattern(v, pattern[1:len(pattern)-1], binds)
	}
	if name, typ, ok := splitTypedPattern(pattern); ok {
		if !matchesTypeName(v, typ) {
			return false
		}
		if name != "_" {
			binds[name] = v
		}
		return true
	}
	if isBindName(pattern) {
		binds[pattern] = v
		return true
	}
	if lit, ok := parsePatternLiteral(pattern); ok {
		return equalLoose(v, lit)
	}
	if strings.Contains(pattern, "(") && strings.HasSuffix(pattern, ")") {
		name := strings.TrimSpace(pattern[:strings.IndexByte(pattern, '(')])
		inner := pattern[strings.IndexByte(pattern, '(')+1 : len(pattern)-1]
		if fmt.Sprint(lookupPart(v, "type")) != name && fmt.Sprint(lookupPart(v, "__type")) != name {
			return false
		}
		parts := splitTopLevel(inner, ',')
		for _, part := range parts {
			if part == "" {
				continue
			}
			if !matchPatternRaw(v, part, binds) {
				return false
			}
		}
		return true
	}
	return false
}

func matchPatternValue(v, pattern any, binds map[string]any) bool {
	switch p := pattern.(type) {
	case Symbol:
		return p == SymbolANY || (p == SymbolNONE && v == nil)
	case OptionalValue:
		if !p.Present {
			return v == nil
		}
		return matchPatternValue(v, p.Value, binds)
	case AllPattern:
		return matchPatternRaw(v, "ALL("+p.Pattern+")", binds)
	default:
		return equalLoose(v, p)
	}
}

func isPatternValue(v any) bool {
	switch v.(type) {
	case Symbol, OptionalValue, AllPattern:
		return true
	default:
		return false
	}
}

func matchObjectPattern(v any, body string, binds map[string]any) bool {
	for _, field := range splitTopLevel(body, ',') {
		field = strings.TrimSpace(field)
		if field == "" || strings.HasPrefix(field, "...") {
			continue
		}
		parts := splitTopLevel(field, ':')
		if len(parts) < 2 {
			return false
		}
		key := unquotePatternKey(parts[0])
		pat := strings.Join(parts[1:], ":")
		if !matchPatternRaw(lookupPart(v, key), pat, binds) {
			return false
		}
	}
	return true
}

func matchListPattern(v any, body string, binds map[string]any) bool {
	items, ok := sliceValues(v)
	if !ok {
		return false
	}
	parts := splitTopLevel(body, ',')
	need := 0
	hasRest := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "...") {
			hasRest = true
			continue
		}
		if need >= len(items) || !matchPatternRaw(items[need], part, binds) {
			return false
		}
		need++
	}
	return hasRest || need == len(items)
}

func splitTypedPattern(pattern string) (string, string, bool) {
	parts := splitTopLevel(pattern, ':')
	if len(parts) != 2 {
		return "", "", false
	}
	name := strings.TrimSpace(parts[0])
	typ := strings.TrimSpace(parts[1])
	return name, typ, isBindName(name) || name == "_"
}

func parsePatternLiteral(pattern string) (any, bool) {
	switch pattern {
	case "true":
		return true, true
	case "false":
		return false, true
	case "null":
		return nil, true
	}
	if len(pattern) >= 2 {
		q := pattern[0]
		if (q == '"' || q == '\'' || q == '`') && pattern[len(pattern)-1] == q {
			if q == '`' {
				return pattern[1 : len(pattern)-1], true
			}
			var s string
			if err := json.Unmarshal([]byte(strconvQuotePattern(pattern)), &s); err == nil {
				return s, true
			}
			return pattern[1 : len(pattern)-1], true
		}
	}
	if strings.Contains(pattern, ".") {
		var f float64
		if _, err := fmt.Sscan(pattern, &f); err == nil {
			return f, true
		}
	} else {
		var i int64
		if _, err := fmt.Sscan(pattern, &i); err == nil {
			return i, true
		}
	}
	return nil, false
}

func strconvQuotePattern(pattern string) string {
	if strings.HasPrefix(pattern, "'") {
		b, _ := json.Marshal(pattern[1 : len(pattern)-1])
		return string(b)
	}
	return pattern
}

func unquotePatternKey(s string) string {
	s = strings.TrimSpace(s)
	if v, ok := parsePatternLiteral(s); ok {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(s, ":")
}

func isBindName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !(r == '_' || r >= 'a' && r <= 'z') {
				return false
			}
			continue
		}
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func matchesTypeName(v any, typ string) bool {
	switch typ {
	case "string":
		_, ok := v.(string)
		return ok
	case "int":
		_, ok := intScalarValue(v)
		return ok
	case "float", "number":
		_, ok := num(v)
		return ok
	case "bool":
		_, ok := v.(bool)
		return ok
	case "list", "array":
		_, ok := sliceValues(v)
		return ok
	case "object", "map":
		rv := reflect.ValueOf(v)
		return rv.IsValid() && rv.Kind() == reflect.Map || rv.IsValid() && rv.Kind() == reflect.Struct
	default:
		return fmt.Sprint(lookupPart(v, "type")) == typ || fmt.Sprint(lookupPart(v, "__type")) == typ
	}
}

func sliceValues(v any) ([]any, bool) {
	switch xs := v.(type) {
	case []any:
		return xs, true
	case []string:
		out := make([]any, 0, len(xs))
		for _, x := range xs {
			out = append(out, x)
		}
		return out, true
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out, true
}

func mergeVars(vars map[string]any, binds map[string]any) map[string]any {
	out := make(map[string]any, len(vars)+len(binds))
	for k, v := range vars {
		out[k] = v
	}
	for k, v := range binds {
		out[k] = v
	}
	return out
}

func EvalCondition(c *Condition, vars map[string]any, opts *EvalOptions) (bool, error) {
	if c == nil {
		return true, nil
	}
	switch c.Op {
	case "expr":
		if c.Expr == nil {
			return true, nil
		}
		v, err := evalExpr(c.Expr.Raw, evalVars(vars, opts), opts)
		if err != nil {
			return false, err
		}
		return truthy(v), nil
	case "all":
		for _, child := range c.Children {
			ok, err := EvalCondition(child, vars, opts)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case "any":
		for _, child := range c.Children {
			ok, err := EvalCondition(child, vars, opts)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "not":
		if len(c.Children) == 0 {
			return true, nil
		}
		ok, err := EvalCondition(c.Children[0], vars, opts)
		return !ok, err
	case "none":
		for _, child := range c.Children {
			ok, err := EvalCondition(child, vars, opts)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unknown condition op %q", c.Op)
	}
}

func evalVars(vars map[string]any, opts *EvalOptions) map[string]any {
	if opts != nil && opts.Variables != nil {
		return opts.Variables
	}
	return vars
}

func lookup(vars map[string]any, path string) any {
	var cur any = vars
	for path != "" {
		part := path
		if i := strings.IndexByte(path, '.'); i >= 0 {
			part = path[:i]
			path = path[i+1:]
		} else {
			path = ""
		}
		if m, ok := cur.(map[string]any); ok {
			cur = m[part]
			continue
		}
		rv := reflect.ValueOf(cur)
		if rv.Kind() == reflect.Struct {
			f := rv.FieldByName(part)
			if f.IsValid() {
				cur = f.Interface()
				continue
			}
		}
		return nil
	}
	return cur
}

func lookupPart(cur any, part string) any {
	if m, ok := cur.(map[string]any); ok {
		return m[part]
	}
	rv := reflect.ValueOf(cur)
	if rv.Kind() == reflect.Struct {
		f := rv.FieldByName(part)
		if f.IsValid() {
			return f.Interface()
		}
	}
	return nil
}

func lookupParts(vars map[string]any, parts []string) any {
	var cur any = vars
	for _, part := range parts {
		cur = lookupPart(cur, part)
		if cur == nil {
			return nil
		}
	}
	return cur
}

func contains(container, value any) bool {
	if s, ok := container.(string); ok {
		return strings.Contains(s, fmt.Sprint(value))
	}
	return containsValue(container, value)
}

func containsValue(container, value any) bool {
	switch xs := container.(type) {
	case []any:
		for _, v := range xs {
			if equalLoose(v, value) {
				return true
			}
		}
		return false
	case []string:
		needle := fmt.Sprint(value)
		for _, v := range xs {
			if v == needle {
				return true
			}
		}
		return false
	case []int:
		n, ok := intScalarValue(value)
		if !ok {
			return false
		}
		for _, v := range xs {
			if v == n {
				return true
			}
		}
		return false
	}
	rv := reflect.ValueOf(container)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false
	}
	for i := 0; i < rv.Len(); i++ {
		if equalLoose(rv.Index(i).Interface(), value) {
			return true
		}
	}
	return false
}

func equalLoose(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case int:
		if y, ok := intScalarValue(b); ok {
			return x == y
		}
		return false
	case int64:
		if y, ok := intScalarValue(b); ok {
			return x == int64(y)
		}
		return false
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case nil:
		return b == nil
	}
	switch y := b.(type) {
	case string:
		x, ok := a.(string)
		return ok && x == y
	case int:
		if x, ok := intScalarValue(a); ok {
			return x == y
		}
		return false
	case bool:
		x, ok := a.(bool)
		return ok && x == y
	case nil:
		return a == nil
	}
	return reflect.DeepEqual(a, b) || fmt.Sprint(a) == fmt.Sprint(b)
}

func hasAny(container, needles any) bool {
	switch xs := needles.(type) {
	case []any:
		for _, x := range xs {
			if contains(container, x) {
				return true
			}
		}
		return false
	case []string:
		for _, x := range xs {
			if contains(container, x) {
				return true
			}
		}
		return false
	}
	rv := reflect.ValueOf(needles)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false
	}
	for i := 0; i < rv.Len(); i++ {
		if contains(container, rv.Index(i).Interface()) {
			return true
		}
	}
	return false
}

func hasAll(container, needles any) bool {
	switch xs := needles.(type) {
	case []any:
		for _, x := range xs {
			if !contains(container, x) {
				return false
			}
		}
		return true
	case []string:
		for _, x := range xs {
			if !contains(container, x) {
				return false
			}
		}
		return true
	}
	rv := reflect.ValueOf(needles)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false
	}
	for i := 0; i < rv.Len(); i++ {
		if !contains(container, rv.Index(i).Interface()) {
			return false
		}
	}
	return true
}

func length(v any) int {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
		return rv.Len()
	default:
		return 0
	}
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	return length(v) == 0 && fmt.Sprint(v) == ""
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case nil:
		return false
	case string:
		return x != ""
	case int, int64, float64, float32:
		f, _ := num(x)
		return f != 0
	default:
		return true
	}
}

func compare(a, b any) int {
	af, aok := num(a)
	bf, bok := num(b)
	if aok && bok {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	default:
		return 0, false
	}
}

func intScalarValue(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		i := int(x)
		return i, float64(i) == x
	case float32:
		i := int(x)
		return i, float32(i) == x
	default:
		return 0, false
	}
}

func (e *exprParser) collectRef(first token) string {
	if e.peek().kind != tokDot {
		return first.text
	}
	var b strings.Builder
	b.WriteString(first.text)
	for e.peek().kind == tokDot {
		e.next()
		b.WriteByte('.')
		b.WriteString(e.next().text)
	}
	return b.String()
}

func (e *exprParser) lookupRef(first token) any {
	cur := lookupPart(e.vars, first.text)
	for e.peek().kind == tokDot {
		e.next()
		cur = lookupPart(cur, e.next().text)
	}
	return cur
}

func (e *exprParser) isTimeNow(first token) bool {
	if first.text != "time" || e.peek().kind != tokDot || e.peekN(1).text != "now" {
		return false
	}
	e.next()
	e.next()
	return true
}

func (e *exprParser) isDottedCall() bool {
	i := e.pos
	for i+1 < len(e.toks) && e.toks[i].kind == tokDot {
		i += 2
	}
	return i < len(e.toks) && e.toks[i].kind == tokLParen
}

func (e *exprParser) next() token {
	t := e.peek()
	if e.pos < len(e.toks) {
		e.pos++
	}
	return t
}

func (e *exprParser) peek() token {
	if e.pos >= len(e.toks) {
		return token{kind: tokEOF}
	}
	return e.toks[e.pos]
}

func (e *exprParser) peekN(n int) token {
	i := e.pos + n
	if i >= len(e.toks) {
		return token{kind: tokEOF}
	}
	return e.toks[i]
}

package bcl

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Node interface {
	Eval(env *Environment) (any, error)
	ToBCL(indent string) string
}

type AssignmentNode struct {
	VarName string
	Value   Node
}

func (a *AssignmentNode) Eval(env *Environment) (any, error) {
	if include, ok := a.Value.(*IncludeNode); ok {
		val, err := include.Eval(env)
		if err != nil {
			return nil, err
		}
		env.vars[a.VarName] = val
		return map[string]any{a.VarName: val}, nil
	}
	val, err := a.Value.Eval(env)
	if err != nil {
		return nil, err
	}
	env.vars[a.VarName] = val
	return map[string]any{a.VarName: val}, nil
}

func (a *AssignmentNode) ToBCL(indent string) string {
	if include, ok := a.Value.(*IncludeNode); ok {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%s%s = {\n", indent, a.VarName))
		for _, node := range include.Nodes {
			sb.WriteString(node.ToBCL(indent+"    ") + "\n")
		}
		sb.WriteString(fmt.Sprintf("%s}", indent))
		return sb.String()
	}
	return fmt.Sprintf("%s%s = %s", indent, a.VarName, a.Value.ToBCL(""))
}

type BlockNode struct {
	Type  string
	Label string
	Props []Node
}

func (b *BlockNode) Eval(env *Environment) (any, error) {
	local := NewEnv(nil)
	for _, n := range b.Props {
		_, err := n.Eval(local)
		if err != nil {
			return nil, err
		}
	}

	local.vars["name"] = b.Label
	block := map[string]any{
		"__type":  b.Type,
		"__label": b.Label,
		"props":   local.vars,
	}

	env.vars[b.Label] = block
	if existing, ok := env.vars[b.Type]; ok {
		if m, ok2 := existing.(map[string]any); ok2 {
			m[b.Label] = block
		}
	} else {
		env.vars[b.Type] = map[string]any{b.Label: block}
	}
	return block, nil
}

func (b *BlockNode) ToBCL(indent string) string {
	lbl := b.Label
	if strings.ContainsAny(lbl, " \t") {
		lbl = fmt.Sprintf("\"%s\"", lbl)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s%s %s {\n", indent, b.Type, lbl))
	for _, p := range b.Props {
		sb.WriteString(p.ToBCL(indent + "    "))
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("%s}", indent))
	return sb.String()
}

type BlockContainerNode struct {
	Type   string
	Blocks []Node
}

func (b *BlockContainerNode) Eval(env *Environment) (any, error) {
	result := make(map[string]any)
	for _, blockNode := range b.Blocks {
		val, err := blockNode.Eval(NewEnv(nil))
		if err != nil {
			return nil, err
		}
		if block, ok := val.(map[string]any); ok {
			if label, ok := block["__label"].(string); ok {
				result[label] = block
			}
		}
	}
	env.vars[b.Type] = result
	return result, nil
}

func (b *BlockContainerNode) ToBCL(indent string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s%s {\n", indent, b.Type))
	for _, blockNode := range b.Blocks {
		sb.WriteString("    " + blockNode.ToBCL(indent+"    ") + "\n")
	}
	sb.WriteString(fmt.Sprintf("%s}", indent))
	return sb.String()
}

type MapNode struct {
	Entries []*AssignmentNode
}

func (m *MapNode) Eval(env *Environment) (any, error) {
	local := NewEnv(nil)
	for _, entry := range m.Entries {
		_, err := entry.Eval(local)
		if err != nil {
			return nil, err
		}
	}
	return local.vars, nil
}

func (m *MapNode) ToBCL(indent string) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString(writeAssignments(indent, m.Entries))
	sb.WriteString(indent + "}")
	return sb.String()
}

type CombinedMapNode struct {
	Entries []*AssignmentNode
	Blocks  []Node
}

func (m *CombinedMapNode) Eval(env *Environment) (any, error) {
	local := NewEnv(nil)
	for _, entry := range m.Entries {
		_, err := entry.Eval(local)
		if err != nil {
			return nil, err
		}
	}
	for _, block := range m.Blocks {
		_, err := block.Eval(local)
		if err != nil {
			return nil, err
		}
	}
	return local.vars, nil
}

func (m *CombinedMapNode) ToBCL(indent string) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString(writeAssignments(indent, m.Entries))
	for _, block := range m.Blocks {
		sb.WriteString(indent + "    " + block.ToBCL(indent+"    ") + "\n")
	}
	sb.WriteString(indent + "}")
	return sb.String()
}

type SliceNode struct {
	Elements []Node
}

func (s *SliceNode) Eval(env *Environment) (any, error) {
	var result []any
	for _, el := range s.Elements {
		val, err := el.Eval(env)
		if err != nil {
			return nil, err
		}
		result = append(result, val)
	}
	return result, nil
}

func (s *SliceNode) ToBCL(indent string) string {
	if len(s.Elements) > 0 {
		switch s.Elements[0].(type) {
		case *PrimitiveNode:
			var parts []string
			for _, el := range s.Elements {
				parts = append(parts, el.ToBCL(""))
			}
			return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
		default:
			var parts []string
			for _, el := range s.Elements {
				parts = append(parts, el.ToBCL(indent+"    "))
			}
			return fmt.Sprintf("[\n%s\n%s]", strings.Join(parts, "\n"), indent)
		}
	}
	return "[]"
}

type PrimitiveNode struct {
	Value any
}

func (p *PrimitiveNode) Eval(env *Environment) (any, error) {
	if s, ok := p.Value.(string); ok {
		if strings.Contains(s, "${") {
			interpolated, err := interpolateDynamic(s, env)
			if err != nil {
				return nil, err
			}
			return interpolated, nil
		}
	}
	return p.Value, nil
}

func (p *PrimitiveNode) ToBCL(indent string) string {
	switch v := p.Value.(type) {
	case string:
		return fmt.Sprintf("\"%s\"", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func interpolateDynamic(s string, env *Environment) (string, error) {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			j := i + 2
			braceCount := 1
			for j < len(s) && braceCount > 0 {
				if s[j] == '{' {
					braceCount++
				} else if s[j] == '}' {
					braceCount--
				}
				j++
			}
			if braceCount != 0 {
				return "", fmt.Errorf("unmatched braces in dynamic expression")
			}
			exprStr := s[i+2 : j-1]
			parser := NewParser(exprStr)
			expr, err := parser.parseExpression()
			if err != nil {
				return "", err
			}
			val, err := expr.Eval(env)
			if err != nil {
				return "", err
			}
			result.WriteString(fmt.Sprintf("%v", val))
			i = j
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String(), nil
}

type IdentifierNode struct {
	Name string
}

type EnvMap struct{}

func (i *IdentifierNode) Eval(env *Environment) (any, error) {
	if i.Name == "env" {
		return EnvMap{}, nil
	}
	if val, ok := env.Lookup(i.Name); ok {
		return val, nil
	}
	return nil, fmt.Errorf("undefined variable %s", i.Name)
}

func (i *IdentifierNode) ToBCL(indent string) string {
	return i.Name
}

type DotAccessNode struct {
	Left  Node
	Right string
}

func (d *DotAccessNode) Eval(env *Environment) (any, error) {
	leftVal, err := d.Left.Eval(env)
	if err != nil {
		return nil, err
	}
	if _, ok := leftVal.(EnvMap); ok {
		if strings.Contains(d.Right, ":") {
			parts := strings.SplitN(d.Right, ":", 2)
			val := os.Getenv(parts[0])
			if val == "" {
				return parts[1], nil
			}
			return val, nil
		}
		return os.Getenv(d.Right), nil
	}
	m, ok := leftVal.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("dot access on non-map type")
	}
	if v, exists := m[d.Right]; exists {
		return v, nil
	}
	if v, exists := m["props"].(map[string]any); exists {
		if v, exists := v[d.Right]; exists {
			return v, nil
		}
	}
	return nil, fmt.Errorf("key %s not found", d.Right)
}

func (d *DotAccessNode) ToBCL(indent string) string {
	quoted := d.Right
	if strings.ContainsAny(d.Right, " \t") {
		quoted = fmt.Sprintf("\"%s\"", d.Right)
	}
	return fmt.Sprintf("%s%s.%s", indent, d.Left.ToBCL(""), quoted)
}

type ArithmeticNode struct {
	Op    string
	Left  Node
	Right Node
}

func (a *ArithmeticNode) Eval(env *Environment) (any, error) {

	if a.Op == "or" || a.Op == "and" {
		leftVal, err := a.Left.Eval(env)
		if err != nil {
			return nil, err
		}
		rightVal, err := a.Right.Eval(env)
		if err != nil {
			return nil, err
		}
		if lb, ok := leftVal.(bool); ok {
			if rb, ok := rightVal.(bool); ok {
				if a.Op == "and" {
					return lb && rb, nil
				}
				return lb || rb, nil
			}
		}
		lf, err := toFloat(leftVal)
		if err != nil {
			return nil, err
		}
		rf, err := toFloat(rightVal)
		if err != nil {
			return nil, err
		}
		if a.Op == "and" {
			return (lf != 0) && (rf != 0), nil
		}
		return (lf != 0) || (rf != 0), nil
	}

	leftVal, err := a.Left.Eval(env)
	if err != nil {
		return nil, err
	}
	rightVal, err := a.Right.Eval(env)
	if err != nil {
		return nil, err
	}
	lf, err := toFloat(leftVal)
	if err != nil {
		return nil, err
	}
	rf, err := toFloat(rightVal)
	if err != nil {
		return nil, err
	}
	switch a.Op {
	case "+", "add":
		return lf + rf, nil
	case "-", "subtract":
		return lf - rf, nil
	case "*", "multiply":
		return lf * rf, nil
	case "/", "divide":
		if rf == 0 {
			return nil, errors.New("division by zero")
		}
		return lf / rf, nil
	case "mod":
		return float64(int(lf) % int(rf)), nil
	case "<":
		return lf < rf, nil
	case "&":
		return int(lf) & int(rf), nil
	case "|":
		return int(lf) | int(rf), nil
	case "^":
		return int(lf) ^ int(rf), nil
	case ">>":
		return int(lf) >> int(rf), nil
	case "<<":
		return int(lf) << int(rf), nil
	default:
		return nil, fmt.Errorf("unknown operator %s", a.Op)
	}
}

func (a *ArithmeticNode) ToBCL(indent string) string {
	return fmt.Sprintf("%s%s %s %s", indent, a.Left.ToBCL(""), a.Op, a.Right.ToBCL(""))
}

func toFloat(val any) (float64, error) {
	switch t := val.(type) {
	case int:
		return float64(t), nil
	case float64:
		return t, nil
	case string:
		return strconv.ParseFloat(t, 64)
	default:
		return 0, errors.New("unable to convert to float")
	}
}

type ControlNode struct {
	Condition Node
	Body      []Node
	Else      *ControlNode
}

func (c *ControlNode) Eval(env *Environment) (any, error) {
	condTrue := false
	if c.Condition != nil {
		condVal, err := c.Condition.Eval(env)
		if err != nil {
			return nil, err
		}
		if b, ok := condVal.(bool); ok && b {
			condTrue = true
		}
	} else {
		condTrue = true
	}
	if condTrue {
		for _, stmt := range c.Body {
			_, err := stmt.Eval(env)
			if err != nil {
				return nil, err
			}
		}
	} else if c.Else != nil {
		_, err := c.Else.Eval(env)
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (c *ControlNode) ToBCL(indent string) string {
	var sb strings.Builder
	if c.Condition != nil {
		sb.WriteString(fmt.Sprintf("%sIF (%s) {\n", indent, c.Condition.ToBCL("")))
	} else {
		sb.WriteString(fmt.Sprintf("%sELSE {\n", indent))
	}
	for _, stmt := range c.Body {
		sb.WriteString(stmt.ToBCL(indent + "    "))
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("%s}", indent))
	if c.Else != nil {
		sb.WriteString(" " + c.Else.ToBCL(indent))
	}
	return sb.String()
}

var includeCache = make(map[string][]Node)

type IncludeNode struct {
	FileName string
	Nodes    []Node
}

func (i *IncludeNode) Eval(env *Environment) (any, error) {
	if cached, ok := includeCache[i.FileName]; ok {
		for _, node := range cached {
			_, err := node.Eval(env)
			if err != nil {
				return nil, err
			}
		}
		return cached, nil
	}
	local := NewEnv(nil)
	for _, node := range i.Nodes {
		_, err := node.Eval(local)
		if err != nil {
			return nil, err
		}
	}
	for key, val := range local.vars {
		env.vars[key] = val
	}

	includeCache[i.FileName] = i.Nodes
	return local.vars, nil
}

func (i *IncludeNode) ToBCL(indent string) string {
	return fmt.Sprintf(`%s@include "%s"`, indent, i.FileName)
}

type CommentNode struct {
	Text string
}

func (c *CommentNode) Eval(env *Environment) (any, error) {
	return nil, nil
}

func (c *CommentNode) ToBCL(indent string) string {
	return indent + c.Text
}

// NEW: Add UnaryNode to support unary operators
type UnaryNode struct {
	Op    string
	Child Node
}

func (u *UnaryNode) Eval(env *Environment) (any, error) {
	val, err := u.Child.Eval(env)
	if err != nil {
		return nil, err
	}
	switch u.Op {
	case "-":
		f, err := toFloat(val)
		if err != nil {
			return nil, err
		}
		return -f, nil
	case "!":
		if b, ok := val.(bool); ok {
			return !b, nil
		}
		return nil, fmt.Errorf("cannot apply '!' to non-bool type")
	default:
		return nil, fmt.Errorf("unknown unary operator %s", u.Op)
	}
}

// Updated UnaryNode.ToBCL to insert space after '!' operator
func (u *UnaryNode) ToBCL(indent string) string {
	// For logical negation add a space to avoid token merging issues (e.g., "! true")
	if u.Op == "!" {
		return fmt.Sprintf("%s%s %s", indent, u.Op, u.Child.ToBCL(""))
	}
	return fmt.Sprintf("%s%s%s", indent, u.Op, u.Child.ToBCL(""))
}

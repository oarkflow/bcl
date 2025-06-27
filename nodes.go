package bcl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/goccy/go-reflect"
)

// Reuse builders to reduce allocations
var builderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

func getBuilder(capacity int) *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.Grow(capacity)
	return b
}

func putBuilder(b *strings.Builder) {
	builderPool.Put(b)
}

type Node interface {
	Eval(env *Environment) (any, error)
	ToBCL(indent string) string
	NodeType() string
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
		if _, ok := a.Value.(*TupleExtractNode); ok {
			val = nil
		} else {
			return nil, err
		}
	}
	// NEW: If the evaluated value is a block (i.e. a map containing "__type"),
	// wrap it in a new map with key equal to the assignment variable.
	if m, ok := val.(map[string]any); ok {
		if _, isBlock := m["__type"]; isBlock {
			wrapped := map[string]any{a.VarName: m}
			env.vars[a.VarName] = wrapped
			return wrapped, nil
		}
	}
	if tuple, ok := val.([]any); ok && len(tuple) == 2 {
		if _, ok := a.Value.(*TupleExtractNode); !ok {
			if reflect.TypeOf(tuple[0]) != reflect.TypeOf(tuple[1]) {
				val = tuple[0]
			}
		}
	}
	env.vars[a.VarName] = val
	return map[string]any{a.VarName: val}, nil
}

func (a *AssignmentNode) ToBCL(indent string) string {
	capacity := len(indent) + len(a.VarName) + 16
	sb := getBuilder(capacity)
	sb.WriteString(indent)
	sb.WriteString(a.VarName)
	sb.WriteString(" = ")
	sb.WriteString(a.Value.ToBCL(""))
	result := sb.String()
	putBuilder(sb)
	return result
}

func (a *AssignmentNode) NodeType() string { return "Assignment" }

type MultiAssignNode struct {
	Assignments []*AssignmentNode
}

func (m *MultiAssignNode) Eval(env *Environment) (any, error) {
	result := make(map[string]any)
	for _, assign := range m.Assignments {
		val, err := assign.Eval(env)
		if err != nil {
			return nil, err
		}
		// Merge each assignment result into result map.
		if mres, ok := val.(map[string]any); ok {
			for k, v := range mres {
				result[k] = v
			}
		}
	}
	return result, nil
}

// ToBCL to print the original expression for tuple assignments.
func (m *MultiAssignNode) ToBCL(indent string) string {
	allTuple := true
	var baseNode Node
	for i, assign := range m.Assignments {
		te, ok := assign.Value.(*TupleExtractNode)
		if !ok {
			allTuple = false
			break
		}
		if i == 0 {
			baseNode = te.Base
		} else if baseNode != nil && baseNode.ToBCL("") != te.Base.ToBCL("") {
			allTuple = false
			break
		}
	}
	if allTuple && baseNode != nil {
		var lhs []string
		for _, assign := range m.Assignments {
			lhs = append(lhs, assign.VarName)
		}
		return indent + strings.Join(lhs, ", ") + " = " + baseNode.ToBCL("")
	}
	// Fallback: use individual assignments.
	var parts []string
	for _, assign := range m.Assignments {
		parts = append(parts, assign.ToBCL(""))
	}
	return indent + strings.Join(parts, ", ")
}

func (m *MultiAssignNode) NodeType() string { return "MultiAssign" }

type BlockNode struct {
	Type  string
	Label string
	Props []Node
}

func (b *BlockNode) Eval(env *Environment) (any, error) {
	if b.Label == "" {
		b.Label = b.Type
	}
	local := NewEnv(env)
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

	if existing, ok := env.vars[b.Type]; ok {
		if m, ok2 := existing.(map[string]any); ok2 {
			m[b.Label] = block
		}
	} else {
		env.vars[b.Type] = map[string]any{b.Label: block}
	}
	// NEW: Create a shallow copy to store block by its label for dot notation access.
	shallow := make(map[string]any)
	for k, v := range block {
		shallow[k] = v
	}
	env.vars[b.Label] = shallow
	return block, nil
}

func (b *BlockNode) ToBCL(indent string) string {
	lbl := b.Label
	if strings.ContainsAny(lbl, " \t") {
		lbl = "\"" + lbl + "\""
	}
	capacity := len(indent) + len(b.Type) + len(lbl) + len(b.Props)*32 + 16
	sb := getBuilder(capacity)
	sb.WriteString(indent)
	sb.WriteString(b.Type)
	sb.WriteString(" ")
	sb.WriteString(lbl)
	sb.WriteString(" {\n")
	for _, p := range b.Props {
		sb.WriteString(p.ToBCL(indent + "    "))
		sb.WriteByte('\n')
	}
	sb.WriteString(indent)
	sb.WriteByte('}')
	result := sb.String()
	putBuilder(sb)
	return result
}

func (b *BlockNode) NodeType() string { return "Block" }

type ArrowNode struct {
	Type   string
	Source string
	Target string
	Props  []Node
}

func (a *ArrowNode) Eval(env *Environment) (any, error) {
	local := NewEnv(env)
	for _, n := range a.Props {
		_, err := n.Eval(local)
		if err != nil {
			return nil, err
		}
	}
	if a.Type == "" {
		a.Type = "Edge"
	}
	local.vars["source"] = a.Source
	local.vars["target"] = a.Target
	result := map[string]any{
		"type":  a.Type,
		"props": local.vars,
	}
	// Append the result to a slice at key "arrows"
	arrows, ok := env.vars[a.Type].([]any)
	if !ok {
		arrows = []any{}
	}
	arrows = append(arrows, result)
	env.vars[a.Type] = arrows
	return result, nil
}

func (a *ArrowNode) ToBCL(indent string) string {
	typ := a.Type
	if typ == "" {
		typ = "Edge"
	}
	sb := getBuilder(64)
	sb.WriteString(indent)
	if typ != "Edge" {
		sb.WriteString(typ)
		sb.WriteString(" ")
	}
	sb.WriteString(a.Source)
	sb.WriteString(" -> ")
	sb.WriteString(a.Target)
	if len(a.Props) > 0 {
		sb.WriteString(" {\n")
		for _, p := range a.Props {
			sb.WriteString(p.ToBCL(indent + "    "))
			sb.WriteByte('\n')
		}
		sb.WriteString(indent)
		sb.WriteByte('}')
	}
	result := sb.String()
	putBuilder(sb)
	return result
}

func (a *ArrowNode) NodeType() string { return "Arrow" }

type BlockContainerNode struct {
	Type   string
	Blocks []Node
}

func (b *BlockContainerNode) Eval(env *Environment) (any, error) {
	result := make(map[string]any)
	for _, blockNode := range b.Blocks {
		val, err := blockNode.Eval(NewEnv(env))
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
	sb := getBuilder(32)
	sb.WriteString(fmt.Sprintf("%s%s {\n", indent, b.Type))
	for _, blockNode := range b.Blocks {
		sb.WriteString("    " + blockNode.ToBCL(indent+"    ") + "\n")
	}
	sb.WriteString(fmt.Sprintf("%s}", indent))
	result := sb.String()
	putBuilder(sb)
	return result
}

func (b *BlockContainerNode) NodeType() string { return "BlockContainer" }

type MapNode struct {
	Entries []*AssignmentNode
}

func (m *MapNode) Eval(env *Environment) (any, error) {
	local := NewEnv(env)
	for _, entry := range m.Entries {
		_, err := entry.Eval(local)
		if err != nil {
			return nil, err
		}
	}
	return local.vars, nil
}

func (m *MapNode) ToBCL(indent string) string {
	sb := getBuilder(32)
	sb.WriteString("{\n")
	sb.WriteString(writeAssignments(indent, m.Entries))
	sb.WriteString(indent + "}")
	result := sb.String()
	putBuilder(sb)
	return result
}

func (m *MapNode) NodeType() string { return "Map" }

type CombinedMapNode struct {
	Entries []*AssignmentNode
	Blocks  []Node
}

func (m *CombinedMapNode) Eval(env *Environment) (any, error) {
	local := NewEnv(env)
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
	sb := getBuilder(32)
	sb.WriteString("{\n")
	sb.WriteString(writeAssignments(indent, m.Entries))
	for _, block := range m.Blocks {
		sb.WriteString(indent + "    " + block.ToBCL(indent+"    ") + "\n")
	}
	sb.WriteString(indent + "}")
	result := sb.String()
	putBuilder(sb)
	return result
}

func (m *CombinedMapNode) NodeType() string { return "CombinedMap" }

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
			sb := getBuilder(32)
			sb.WriteString("[\n")
			var parts []string
			for _, el := range s.Elements {
				parts = append(parts, el.ToBCL(indent+"    "))
			}
			sb.WriteString(strings.Join(parts, "\n"))
			sb.WriteString("\n" + indent + "]")
			result := sb.String()
			putBuilder(sb)
			return result
		}
	}
	return "[]"
}

func (s *SliceNode) NodeType() string { return "Slice" }

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
		if strings.Contains(v, "\n") {
			return fmt.Sprintf("<<EOF\n%s\nEOF", v)
		}
		return fmt.Sprintf("\"%s\"", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (p *PrimitiveNode) NodeType() string { return "Primitive" }

func interpolateDynamic(s string, env *Environment) (string, error) {
	sb := getBuilder(len(s))
	defer putBuilder(sb)
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
			sb.WriteString(fmt.Sprintf("%v", val))
			i = j
		} else {
			sb.WriteByte(s[i])
			i++
		}
	}
	return sb.String(), nil
}

type IdentifierNode struct {
	Name string
}

type EnvMap struct{}

// Undefined Represents an undefined variable.
type Undefined struct{}

func (u Undefined) String() string {
	return "undefined"
}

// Eval to return Undefined instead of an error.
func (i *IdentifierNode) Eval(env *Environment) (any, error) {
	if i.Name == "env" {
		return EnvMap{}, nil
	}
	if val, ok := env.Lookup(i.Name); ok {
		return val, nil
	}
	return Undefined{}, nil
}

func (i *IdentifierNode) ToBCL(indent string) string {
	return i.Name
}

func (i *IdentifierNode) NodeType() string { return "Identifier" }

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
	// Handling for map container.
	if m, ok := leftVal.(map[string]any); ok {
		// If key exists directly, try to get full block from env.vars.
		if v, exists := m[d.Right]; exists {
			if full, found := env.Lookup(d.Right); found {
				return full, nil
			}
			return v, nil
		}
		// If this map is a block, extract from props.
		if _, isBlock := m["__type"]; isBlock {
			if props, exists := m["props"].(map[string]any); exists {
				if v, exists := props[d.Right]; exists {
					return v, nil
				}
			}
			return nil, nil
		}
		// Legacy case: try nested lookup in a single-entry container.
		if len(m) == 1 {
			for _, v := range m {
				if block, ok := v.(map[string]any); ok {
					if props, exists := block["props"].(map[string]any); exists {
						if v, exists := props[d.Right]; exists {
							return v, nil
						}
					}
				}
			}
		}
		return nil, nil
	}
	// NEW: Allow dot access when leftVal is a slice (e.g. a block container).
	if slice, ok := leftVal.([]any); ok {
		for _, item := range slice {
			if block, ok := item.(map[string]any); ok {
				if label, ok := block["__label"].(string); ok && label == d.Right {
					if full, found := env.Lookup(d.Right); found {
						return full, nil
					}
					return block, nil
				}
				// Also look into nested props for block property.
				if _, isBlock := block["__type"]; isBlock {
					if props, exists := block["props"].(map[string]any); exists {
						if v, exists := props[d.Right]; exists {
							return v, nil
						}
					}
				}
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("dot access on non-map type")
}

func (d *DotAccessNode) ToBCL(indent string) string {
	leftStr := d.Left.ToBCL("")
	capacity := len(indent) + len(leftStr) + len(d.Right) + 10
	sb := getBuilder(capacity)
	sb.WriteString(indent)
	sb.WriteString(leftStr)
	sb.WriteByte('.')
	if strings.ContainsAny(d.Right, " \t") {
		sb.WriteByte('"')
		sb.WriteString(d.Right)
		sb.WriteByte('"')
	} else {
		sb.WriteString(d.Right)
	}
	result := sb.String()
	putBuilder(sb)
	return result
}

func (d *DotAccessNode) NodeType() string { return "DotAccess" }

type ArithmeticNode struct {
	Op    string
	Left  Node
	Right Node
}

func (a *ArithmeticNode) Eval(env *Environment) (any, error) {
	switch a.Op {
	case "==":
		leftVal, err := a.Left.Eval(env)
		if err != nil {
			return nil, err
		}
		rightVal, err := a.Right.Eval(env)
		if err != nil {
			return nil, err
		}
		return reflect.DeepEqual(leftVal, rightVal), nil
	case "!=":
		leftVal, err := a.Left.Eval(env)
		if err != nil {
			return nil, err
		}
		rightVal, err := a.Right.Eval(env)
		if err != nil {
			return nil, err
		}
		return !reflect.DeepEqual(leftVal, rightVal), nil
	case ">":
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
		return lf > rf, nil
	case ">=":
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
		return lf >= rf, nil
	case "<=":
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
		return lf <= rf, nil
	}
	switch a.Op {
	case "+":
		leftVal, err := a.Left.Eval(env)
		if err != nil {
			return nil, err
		}
		rightVal, err := a.Right.Eval(env)
		if err != nil {
			return nil, err
		}
		if ls, ok := leftVal.(string); ok {
			if rs, ok := rightVal.(string); ok {
				return ls + rs, nil
			}
			return nil, fmt.Errorf("type error: cannot concatenate string and %T", rightVal)
		}
		if rs, ok := rightVal.(string); ok {
			if lf, err := toFloat(leftVal); err == nil {
				return fmt.Sprint(lf) + rs, nil
			}
			return nil, fmt.Errorf("type error: cannot concatenate %T and string", leftVal)
		}
		lf, err := toFloat(leftVal)
		if err != nil {
			return nil, err
		}
		rf, err := toFloat(rightVal)
		if err != nil {
			return nil, err
		}
		return lf + rf, nil
	case "add":
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
		return lf + rf, nil
	case "-", "subtract":
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
		return lf - rf, nil
	case "*", "multiply":
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
		return lf * rf, nil
	case "/", "divide":
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
		if rf == 0 {
			return nil, errors.New("division by zero")
		}
		return lf / rf, nil
	case "%", "mod":
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
		return float64(int(lf) % int(rf)), nil
	case "<":
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
		return lf < rf, nil
	case "||":
		leftVal, err := a.Left.Eval(env)
		if err != nil {
			return nil, err
		}
		rightVal, err := a.Right.Eval(env)
		if err != nil {
			return nil, err
		}
		lb, ok := leftVal.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of || is not bool")
		}
		rb, ok := rightVal.(bool)
		fmt.Println(rightVal)
		if !ok {
			return nil, fmt.Errorf("right operand of || is not bool")
		}
		return lb || rb, nil
	case "&&":
		leftVal, err := a.Left.Eval(env)
		if err != nil {
			return nil, err
		}
		rightVal, err := a.Right.Eval(env)
		if err != nil {
			return nil, err
		}
		lb, ok := leftVal.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of && is not bool")
		}
		rb, ok := rightVal.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of && is not bool")
		}
		return lb && rb, nil
	case "&":
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
		return int(lf) & int(rf), nil
	case "|":
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
		return int(lf) | int(rf), nil
	case "^":
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
		return int(lf) ^ int(rf), nil
	case ">>":
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
		return int(lf) >> int(rf), nil
	case "<<":
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
		return int(lf) << int(rf), nil
	case "??":
		leftVal, err := a.Left.Eval(env)
		if err != nil {
			return nil, err
		}
		if leftVal != nil {
			return leftVal, nil
		}
		return a.Right.Eval(env)
	default:
		return nil, fmt.Errorf("unknown operator %s", a.Op)
	}
}

func (a *ArithmeticNode) ToBCL(indent string) string {
	leftStr := a.Left.ToBCL("")
	rightStr := a.Right.ToBCL("")
	capacity := len(indent) + len(leftStr) + len(a.Op) + len(rightStr) + 4
	sb := getBuilder(capacity)
	sb.WriteString(indent)
	sb.WriteString(leftStr)
	sb.WriteByte(' ')
	sb.WriteString(a.Op)
	sb.WriteByte(' ')
	sb.WriteString(rightStr)
	result := sb.String()
	putBuilder(sb)
	return result
}

func (a *ArithmeticNode) NodeType() string { return "Arithmetic" }

func toFloat(val any) (float64, error) {
	switch t := val.(type) {
	case int:
		return float64(t), nil
	case int32:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case float32:
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
	sb := getBuilder(64)
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
	result := sb.String()
	putBuilder(sb)
	return result
}

func (c *ControlNode) NodeType() string { return "Control" }

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

func (i *IncludeNode) NodeType() string { return "Include" }

type CommentNode struct {
	Text string
}

func (c *CommentNode) Eval(env *Environment) (any, error) {
	return nil, nil
}

func (c *CommentNode) ToBCL(indent string) string {
	return indent + c.Text
}

func (c *CommentNode) NodeType() string { return "Comment" }

// UnaryNode to support unary operators
type UnaryNode struct {
	Op    string
	Child Node
}

func (u *UnaryNode) Eval(env *Environment) (any, error) {
	val, err := u.Child.Eval(env)
	if err != nil {
		return nil, err
	}

	if reflect.TypeOf(val).Kind() == reflect.Slice {
		v := reflect.ValueOf(val)
		if v.Len() == 0 {
			return nil, fmt.Errorf("cannot apply unary operator to empty slice")
		}
		val = v.Index(0).Interface()
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

// ToBCL to insert space after '!' operator
func (u *UnaryNode) ToBCL(indent string) string {
	// For logical negation add a space to avoid token merging issues (e.g., "! true")
	if u.Op == "!" {
		return fmt.Sprintf("%s%s %s", indent, u.Op, u.Child.ToBCL(""))
	}
	return fmt.Sprintf("%s%s%s", indent, u.Op, u.Child.ToBCL(""))
}

func (u *UnaryNode) NodeType() string { return "Unary" }

// TernaryNode implementation for condition ? trueExpr : falseExpr
type TernaryNode struct {
	Condition Node
	TrueExpr  Node
	FalseExpr Node
}

func (t *TernaryNode) Eval(env *Environment) (any, error) {
	condVal, err := t.Condition.Eval(env)
	if err != nil {
		return nil, err
	}
	switch condVal := condVal.(type) {
	case bool:
		if condVal {
			return t.TrueExpr.Eval(env)
		}
		return t.FalseExpr.Eval(env)
	case []any:
		if len(condVal) == 2 {
			switch b := condVal[0].(type) {
			case bool:
				if b {
					return t.TrueExpr.Eval(env)
				}
				return t.FalseExpr.Eval(env)
			}
		}
	}
	fmt.Println(reflect.TypeOf(condVal))
	return nil, fmt.Errorf("ternary condition did not evaluate to bool")
}

func (t *TernaryNode) ToBCL(indent string) string {
	tCond := t.Condition.ToBCL("")
	tTrue := t.TrueExpr.ToBCL("")
	tFalse := t.FalseExpr.ToBCL("")
	capacity := len(indent) + len(tCond) + len(tTrue) + len(tFalse) + 8
	sb := getBuilder(capacity)
	sb.WriteString(indent)
	sb.WriteString(tCond)
	sb.WriteString(" ? ")
	sb.WriteString(tTrue)
	sb.WriteString(" : ")
	sb.WriteString(tFalse)
	result := sb.String()
	putBuilder(sb)
	return result
}

func (t *TernaryNode) NodeType() string { return "Ternary" }

// GroupNode to preserve parentheses
type GroupNode struct {
	Child Node
}

func (g *GroupNode) Eval(env *Environment) (any, error) {
	return g.Child.Eval(env)
}

func (g *GroupNode) ToBCL(indent string) string {
	// Always output the group with parentheses.
	return fmt.Sprintf("(%s)", g.Child.ToBCL(""))
}

func (g *GroupNode) NodeType() string { return "Group" }

type FunctionNode struct {
	FuncName string
	Args     []Node
}

func (f *FunctionNode) NodeType() string {
	return "FunctionNode"
}

// Eval to always return a two-element tuple.
func (f *FunctionNode) Eval(env *Environment) (any, error) {
	var args []any
	for _, arg := range f.Args {
		val, err := arg.Eval(env)
		if err != nil {
			return nil, err
		}
		args = append(args, val)
	}
	fn, ok := LookupFunction(f.FuncName)
	if !ok {
		return nil, fmt.Errorf("unknown function %s", f.FuncName)
	}
	res, fnErr := fn(args...)
	return []any{res, fnErr}, nil
}

func (f *FunctionNode) ToBCL(indent string) string {
	var argsStr []string
	for _, a := range f.Args {
		argsStr = append(argsStr, a.ToBCL(""))
	}
	return fmt.Sprintf("%s%s(%s)", indent, f.FuncName, strings.Join(argsStr, ", "))
}

type EnvInterpolationNode struct {
	EnvVar       string
	DefaultValue string
}

func (e *EnvInterpolationNode) Eval(env *Environment) (any, error) {
	value := os.Getenv(e.EnvVar)
	if value == "" {
		return e.DefaultValue, nil
	}
	return value, nil
}

func (e *EnvInterpolationNode) ToBCL(indent string) string {
	return fmt.Sprintf("${%s:%s}", e.EnvVar, e.DefaultValue)
}

func (e *EnvInterpolationNode) NodeType() string { return "EnvInterpolation" }

// TupleExtractNode for tuple extraction in multiple assignments.
type TupleExtractNode struct {
	Base  Node
	Index int
}

func (t *TupleExtractNode) Eval(env *Environment) (any, error) {
	val, err := t.Base.Eval(env)
	if err != nil {
		return nil, err
	}
	arr, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("tuple extraction: value is not a tuple")
	}
	if t.Index < 0 || t.Index >= len(arr) {
		return nil, fmt.Errorf("tuple extraction: index %d out of bounds", t.Index)
	}
	return arr[t.Index], nil
}

// ToBCL to preserve original expression.
func (t *TupleExtractNode) ToBCL(indent string) string {
	return t.Base.ToBCL(indent)
}

func (t *TupleExtractNode) NodeType() string { return "TupleExtract" }

// commonCommandMapping maps common commands to their platform‑specific equivalents.
// For Windows, built‑in commands (such as "echo", "dir", etc.) are run via "cmd" with "/C".
var commonCommandMapping = map[string]map[string]string{
	"echo": {
		"windows": "cmd",
		"linux":   "echo",
		"darwin":  "echo",
	},
	"dir": {
		"windows": "cmd",
		"linux":   "ls",
		"darwin":  "ls",
	},
	"copy": {
		"windows": "cmd",
		"linux":   "cp",
		"darwin":  "cp",
	},
	"move": {
		"windows": "cmd",
		"linux":   "mv",
		"darwin":  "mv",
	},
	"rm": {
		"windows": "cmd",
		"linux":   "rm",
		"darwin":  "rm",
	},
	"mkdir": {
		"windows": "cmd",
		"linux":   "mkdir",
		"darwin":  "mkdir",
	},
	// additional mappings
	"cat": {
		"windows": "cmd",
		"linux":   "cat",
		"darwin":  "cat",
	},
	"find": {
		"windows": "cmd",
		"linux":   "find",
		"darwin":  "find",
	},
	"pwd": {
		"windows": "cmd",
		"linux":   "pwd",
		"darwin":  "pwd",
	},
	"del": {
		"windows": "cmd",
		"linux":   "rm",
		"darwin":  "rm",
	},
	"grep": {
		"windows": "cmd",
		"linux":   "grep",
		"darwin":  "grep",
	},
	"awk": {
		"windows": "gawk",
		"linux":   "awk",
		"darwin":  "awk",
	},
	"sed": {
		"windows": "sed",
		"linux":   "sed",
		"darwin":  "sed",
	},
	"clear": {
		"windows": "cls",
		"linux":   "clear",
		"darwin":  "clear",
	},
	"ping": {
		"windows": "ping",
		"linux":   "ping",
		"darwin":  "ping",
	},
	"chmod": {
		"windows": "icacls",
		"linux":   "chmod",
		"darwin":  "chmod",
	},
}

// commandArgsRequired defines the minimum number of required arguments for each command.
var commandArgsRequired = map[string]int{
	"echo":  0,
	"dir":   0,
	"copy":  2,
	"move":  2,
	"rm":    1,
	"mkdir": 1,
	"cat":   1,
	"find":  1,
	"pwd":   0,
	"del":   1,
	"grep":  1,
	"awk":   1,
	"sed":   1,
	"clear": 0,
	"ping":  1,
	"chmod": 2,
}

type ExecNode struct {
	Config map[string]Node
}

func (e *ExecNode) Eval(env *Environment) (any, error) {
	// Evaluate configuration parameters.
	config := make(map[string]any)
	for key, node := range e.Config {
		val, err := node.Eval(env)
		if err != nil {
			return nil, err
		}
		config[key] = val
	}
	// "cmd" is required.
	cmdVal, ok := config["cmd"]
	if !ok {
		return nil, fmt.Errorf("exec: missing 'cmd' parameter")
	}
	cmdName, ok := cmdVal.(string)
	if !ok {
		return nil, fmt.Errorf("exec parameter 'cmd' must be a string")
	}

	// Process "args": allow string or slice (either []any or []string).
	var args []string
	if a, exists := config["args"]; exists {
		switch argVal := a.(type) {
		case string:
			args = append(args, argVal)
		case []any:
			for _, v := range argVal {
				args = append(args, fmt.Sprint(v))
			}
		case []string:
			args = append(args, argVal...)
		default:
			return nil, fmt.Errorf("exec parameter 'args' must be a string or a slice of strings, got %T", a)
		}
	}

	// Determine the current operating system.
	osName := runtime.GOOS
	originalCmd := cmdName
	lowerCmd := strings.ToLower(cmdName)
	if mapping, found := commonCommandMapping[lowerCmd]; found {
		if osName == "windows" {
			// For Windows, use "cmd" with "/C" then the original command and its arguments.
			cmdName = mapping["windows"]
			newArgs := []string{"/C", originalCmd}
			newArgs = append(newArgs, args...)
			args = newArgs
		} else {
			// For Linux or macOS, use the mapped command.
			cmdName = mapping[osName]
		}
	}

	// Validate required arguments for the command.
	if req, exists := commandArgsRequired[lowerCmd]; exists {
		if len(args) < req {
			return nil, fmt.Errorf("exec: command %s requires at least %d arguments, got %d", originalCmd, req, len(args))
		}
	}

	// Clean the "dir" parameter if provided.
	if dirVal, ok := config["dir"]; ok {
		if dirStr, ok := dirVal.(string); ok {
			config["dir"] = filepath.Clean(dirStr)
		}
	}

	// Build and execute the command.
	command := exec.Command(cmdName, args...)
	if dir, ok := config["dir"].(string); ok {
		command.Dir = dir
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("exec command error: %v, output: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func (e *ExecNode) ToBCL(indent string) string {
	sb := getBuilder(64)
	sb.WriteString(indent)
	sb.WriteString("@exec(")
	var parts []string
	for key, node := range e.Config {
		parts = append(parts, fmt.Sprintf(`%s=%s`, key, node.ToBCL("")))
	}
	sb.WriteString(strings.Join(parts, ", "))
	sb.WriteByte(')')
	result := sb.String()
	putBuilder(sb)
	return result
}

func (e *ExecNode) NodeType() string { return "Exec" }

type PipelineNode struct {
	Nodes []Node
}

func (p *PipelineNode) Eval(env *Environment) (any, error) {
	pipeEnv := NewEnv(env)
	assignments := make(map[string]Node)
	var sequentialOrder []string
	var arrows []struct{ Source, Target string }
	for _, node := range p.Nodes {
		switch n := node.(type) {
		case *AssignmentNode:
			assignments[n.VarName] = n.Value
			sequentialOrder = append(sequentialOrder, n.VarName)
		case *ArrowNode:
			arrows = append(arrows, struct{ Source, Target string }{Source: n.Source, Target: n.Target})
		default:
			if _, err := node.Eval(pipeEnv); err != nil {
				return nil, err
			}
		}
	}
	var orderedSteps []string
	if len(arrows) > 0 {
		targets := make(map[string]struct{})
		for _, edge := range arrows {
			targets[edge.Target] = struct{}{}
		}
		var start string
		for _, edge := range arrows {
			if _, found := targets[edge.Source]; !found {
				start = edge.Source
				break
			}
		}
		if start == "" && len(arrows) > 0 {
			start = arrows[0].Source
		}
		orderedSteps = append(orderedSteps, start)
		current := start
		for {
			found := false
			for _, edge := range arrows {
				if edge.Source == current {
					orderedSteps = append(orderedSteps, edge.Target)
					current = edge.Target
					found = true
					break
				}
			}
			if !found {
				break
			}
		}
	} else {
		orderedSteps = sequentialOrder
	}
	var lastOutput any
	for _, step := range orderedSteps {
		node, ok := assignments[step]
		if !ok {
			return nil, fmt.Errorf("pipeline: step %s not found", step)
		}
		val, err := node.Eval(pipeEnv)
		if err != nil {
			return nil, err
		}
		switch val := val.(type) {
		case []any:
			if len(val) == 2 {
				if err, ok := val[1].(error); ok && err != nil {
					return nil, err
				}
				pipeEnv.vars[step] = val[0]
				lastOutput = val[0]
			}
		default:
			pipeEnv.vars[step] = val
			lastOutput = val
		}
	}
	return lastOutput, nil
}

func (p *PipelineNode) ToBCL(indent string) string {
	sb := getBuilder(32)
	sb.WriteString(indent)
	sb.WriteString("@pipeline {\n")
	for _, node := range p.Nodes {
		sb.WriteString("    ")
		sb.WriteString(node.ToBCL(indent + "    "))
		sb.WriteByte('\n')
	}
	sb.WriteString(indent)
	sb.WriteString("}")
	result := sb.String()
	putBuilder(sb)
	return result
}

func (p *PipelineNode) NodeType() string { return "Pipeline" }

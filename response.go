package bcl

import (
	"fmt"
	"strings"
)

type ResponsePlan struct {
	ExpectStatus []int             `json:"expect_status,omitempty"`
	Format       string            `json:"format,omitempty"`
	Capture      map[string]string `json:"capture,omitempty"`
	OnStatus     map[int]string    `json:"on_status,omitempty"`
	Defaults     map[string]any    `json:"defaults,omitempty"`
}

func ResponsePlanFromBlock(b *Block) (*ResponsePlan, error) {
	if b == nil {
		return nil, nil
	}
	plan := &ResponsePlan{Capture: map[string]string{}, OnStatus: map[int]string{}, Defaults: map[string]any{}}
	for _, n := range b.Body {
		switch x := n.(type) {
		case *Assignment:
			switch x.Name {
			case "expect_status":
				plan.ExpectStatus = statusList(x.Value)
			case "format":
				plan.Format = literalString(x.Value)
			case "capture", "map":
				if obj, ok := x.Value.(*Object); ok {
					for _, item := range obj.Fields {
						if a, ok := item.(*Assignment); ok {
							plan.Capture[a.Name] = valuePath(a.Value)
						}
					}
				}
			case "$expr":
				if expr, ok := x.Value.(*Expr); ok {
					src, dst := parseCaptureExpr(expr.Raw)
					if src != "" && dst != "" {
						plan.Capture[dst] = src
					}
				}
			}
		case *Block:
			switch x.Type {
			case "capture", "map":
				for _, item := range x.Body {
					if a, ok := item.(*Assignment); ok {
						plan.Capture[a.Name] = valuePath(a.Value)
					}
				}
			case "on_status":
				code := intValueFromString(x.ID)
				if code == 0 {
					return nil, fmt.Errorf("invalid on_status %q", x.ID)
				}
				plan.OnStatus[code] = summarizeStatusAction(x)
			case "default":
				for _, item := range x.Body {
					if a, ok := item.(*Assignment); ok {
						plan.Defaults[a.Name] = a.Value.ToInterface(false)
					}
				}
			}
		}
	}
	return plan, nil
}

func statusList(v Value) []int {
	if lit, ok := v.(*Literal); ok && lit.Type == "int" {
		return []int{int(lit.Data.(int64))}
	}
	if l, ok := v.(*List); ok {
		var out []int
		for _, item := range l.Items {
			if lit, ok := item.(*Literal); ok && lit.Type == "int" {
				out = append(out, int(lit.Data.(int64)))
			}
		}
		return out
	}
	return nil
}

func parseCaptureExpr(raw string) (string, string) {
	parts := strings.Fields(raw)
	if len(parts) == 3 && parts[1] == "as" {
		return parts[0], parts[2]
	}
	return "", ""
}

func valuePath(v Value) string {
	if ref, ok := v.(*Reference); ok {
		return ref.Path
	}
	if expr, ok := v.(*Expr); ok {
		return expr.Raw
	}
	return literalString(v)
}

func summarizeStatusAction(b *Block) string {
	for _, n := range b.Body {
		if a, ok := n.(*Assignment); ok {
			if a.Name == "retry" {
				return "retry"
			}
			if a.Name == "fail" {
				return "fail:" + literalString(a.Value)
			}
		}
		if child, ok := n.(*Block); ok && child.Type == "default" {
			return "default"
		}
	}
	return "custom"
}

func intValueFromString(s string) int {
	var out int
	_, _ = fmt.Sscan(s, &out)
	return out
}

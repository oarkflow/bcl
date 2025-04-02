package bcl

type Token int

const (
	EOF Token = iota
	IDENT
	STRING
	NUMBER
	BOOL
	ASSIGN
	LBRACE
	RBRACE
	LBRACKET
	RBRACKET
	LPAREN
	RPAREN
	COMMA
	OPERATOR
	KEYWORD
	AT
	LANGLE
	RANGLE
	DOT
	COMMENT
)

type tokenInfo struct {
	typ   Token
	value string
}

type Environment struct {
	vars   map[string]any
	parent *Environment
}

func NewEnv(parent *Environment) *Environment {
	return &Environment{
		vars:   make(map[string]any),
		parent: parent,
	}
}

func (env *Environment) Lookup(name string) (any, bool) {
	if v, ok := env.vars[name]; ok {
		return v, true
	}
	if env.parent != nil {
		return env.parent.Lookup(name)
	}
	return nil, false
}

// Update operatorPrecedence to add precedence for "<"
var operatorPrecedence = map[string]int{
	".":        4,
	"*":        3,
	"/":        3,
	"multiply": 3,
	"divide":   3,
	"mod":      3,
	"+":        2,
	"-":        2,
	"add":      2,
	"subtract": 2,
	"<":        1,
	"<=":       1,
	">":        1,
	">=":       1,
	"==":       1,
	"!=":       1,
	"<<":       2,
	">>":       2,
	"&":        1,
	"^":        1,
	"|":        1,
	"or":       1,
	"and":      1,
}

func getPrecedence(op string) int {
	if prec, ok := operatorPrecedence[op]; ok {
		return prec
	}
	return 0
}

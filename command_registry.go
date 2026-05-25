package bcl

type CommandValidator func(CommandValidationContext) []Diagnostic

type CommandExecutor interface {
	ExecuteCommand(CommandExecutionContext) (CommandExecutionResult, error)
}

type CommandSpec struct {
	Type             string
	Kind             string
	Phase            string
	AllowedChildren  []string
	RequiredChildren []string
	Repeatable       bool
	Validate         CommandValidator
	Executor         CommandExecutor
	Description      string
	Examples         []string
}

type CommandValidationContext struct {
	Block   *Block
	Spec    CommandSpec
	Schemas map[string]*SchemaDecl
}

type CommandExecutionContext struct {
	Block   map[string]any
	Spec    CommandSpec
	Input   map[string]any
	DryRun  bool
	Context map[string]any
}

type CommandExecutionResult struct {
	Outputs     map[string]any `json:"outputs,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
}

type CommandRegistry struct {
	specs      map[string]CommandSpec
	validators []CommandValidator
}

func NewCommandRegistry(specs ...CommandSpec) *CommandRegistry {
	r := &CommandRegistry{specs: map[string]CommandSpec{}}
	for _, spec := range specs {
		r.Register(spec)
	}
	return r
}

func (r *CommandRegistry) Register(spec CommandSpec) {
	if r == nil || spec.Type == "" {
		return
	}
	if r.specs == nil {
		r.specs = map[string]CommandSpec{}
	}
	r.specs[spec.Type] = spec
}

func (r *CommandRegistry) RegisterValidator(validate CommandValidator) {
	if r == nil || validate == nil {
		return
	}
	r.validators = append(r.validators, validate)
}

func (r *CommandRegistry) Spec(blockType string) (CommandSpec, bool) {
	if r == nil || r.specs == nil {
		return CommandSpec{}, false
	}
	spec, ok := r.specs[blockType]
	return spec, ok
}

func (r *CommandRegistry) Specs() map[string]CommandSpec {
	out := map[string]CommandSpec{}
	if r == nil {
		return out
	}
	for k, v := range r.specs {
		out[k] = v
	}
	return out
}

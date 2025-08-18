package bcl

import (
	"fmt"
	"strings"
)

// ParseError represents an error that occurred during parsing
type ParseError struct {
	Message string
	File    string
	Line    int
	Column  int
	Context string
}

func (e *ParseError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s at %s:%d:%d", e.Message, e.File, e.Line, e.Column))
	if e.Context != "" {
		sb.WriteString("\n")
		sb.WriteString(e.Context)
	}
	return sb.String()
}

// EvalError represents an error that occurred during evaluation
type EvalError struct {
	Message string
	Node    Node
	Cause   error
}

func (e *EvalError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *EvalError) Unwrap() error {
	return e.Cause
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Value   any
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field %s: %s (value: %v)", e.Field, e.Message, e.Value)
}

// MultiError represents multiple errors
type MultiError struct {
	Errors []error
}

func (e *MultiError) Error() string {
	if len(e.Errors) == 0 {
		return "no errors"
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d errors occurred:\n", len(e.Errors)))
	for i, err := range e.Errors {
		sb.WriteString(fmt.Sprintf("  %d. %v\n", i+1, err))
	}
	return sb.String()
}

func (e *MultiError) Add(err error) {
	if err != nil {
		e.Errors = append(e.Errors, err)
	}
}

func (e *MultiError) HasErrors() bool {
	return len(e.Errors) > 0
}

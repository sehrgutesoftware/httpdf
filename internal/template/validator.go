package template

import "github.com/kaptinlin/jsonschema"

// Validator is an interface for validating placeholder values against a schema
type Validator interface {
	Validate(tmpl *Template, values map[string]any) *jsonschema.EvaluationResult
}

type validator struct {
	//
}

// NewValidator creates a new Validator
func NewValidator() Validator {
	return &validator{}
}

// Validate validates the given values against the schema
func (v *validator) Validate(tmpl *Template, values map[string]any) *jsonschema.EvaluationResult {
	return tmpl.Schema.Validate(values)
}

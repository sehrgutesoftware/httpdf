package template

import (
	"fmt"
	"html/template"
	"io"
)

// Renderer is an interface for rendering HTML templates
type Renderer interface {
	Render(tmpl *Template, values map[string]any, out io.Writer) error
}

type renderer struct {
	//
}

// NewRenderer creates a new Renderer
func NewRenderer() Renderer {
	return &renderer{}
}

// Render renders the template with the given values to the output
func (r *renderer) Render(tmpl *Template, values map[string]any, out io.Writer) error {
	defer tmpl.Content.Close()
	content, err := io.ReadAll(tmpl.Content)
	if err != nil {
		return fmt.Errorf("could not read template content: %w", err)
	}

	renderer := template.New("main").Funcs(templateFuncs)
	parsed, err := renderer.Parse(string(content))
	if err != nil {
		return fmt.Errorf("could not parse template: %w", err)
	}

	err = parsed.Execute(out, values)
	if err != nil {
		return fmt.Errorf("could not execute template: %w", err)
	}

	return nil
}

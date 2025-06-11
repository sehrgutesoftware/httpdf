package template

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"text/template"

	"github.com/kaptinlin/jsonschema"
)

// Config represents the configuration of a template
type Config struct {
	Page struct {
		Width  float64 `yaml:"width"`
		Height float64 `yaml:"height"`
	} `yaml:"page"`
}

// Template represents a template
type Template struct {
	bytes.Buffer
	Config  Config
	Schema  *jsonschema.Schema
	Assets  fs.FS
	Example map[string]any
}

// Render the template with the given values to the output
func (t *Template) Render(values map[string]any, assetsPrefix string, out io.Writer) error {
	renderer := template.New("main").Funcs(templateFuncs)
	parsed, err := renderer.Parse(t.String())
	if err != nil {
		return fmt.Errorf("could not parse template: %w", err)
	}

	values["__assets__"] = assetsPrefix

	err = parsed.Execute(out, values)
	if err != nil {
		return fmt.Errorf("could not execute template: %w", err)
	}

	return nil
}

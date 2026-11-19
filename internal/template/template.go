package template

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"text/template"

	"github.com/kaptinlin/go-i18n"
	"github.com/kaptinlin/jsonschema"
)

// Config represents the configuration of a template
type Config struct {
	Page struct {
		Width  float64 `yaml:"width"`
		Height float64 `yaml:"height"`
	} `yaml:"page"`
	Locale *struct {
		Locales []string `yaml:"locales"`
		Default string   `yaml:"default"`
	} `yaml:"locale"`
	ExposedEnvVars []string `yaml:"exposedEnvVars"`
	PDF            struct {
		GenerateTaggedPDF       bool `yaml:"generateTaggedPDF"`
		GenerateDocumentOutline bool `yaml:"generateDocumentOutline"`
	} `yaml:"pdf"`
}

// Partial is an additional template file that can be included from the main
// template via its name, i.e. its path relative to the partials directory.
type Partial struct {
	Name   string
	Source string
}

// Template represents a template
type Template struct {
	bytes.Buffer
	Partials []Partial
	Config   Config
	Schema   *jsonschema.Schema
	Assets   fs.FS
	Example  map[string]any
	I18n     *i18n.I18n
}

// Render the template with the given values to the output
func (t *Template) Render(values map[string]any, assetsPrefix string, locale string, out io.Writer) error {
	parsed, err := t.parse(assetsPrefix, locale)
	if err != nil {
		return err
	}

	err = parsed.Execute(out, values)
	if err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	return nil
}

// parse the template and its partials into a single template set
func (t *Template) parse(assetsPrefix string, locale string) (*template.Template, error) {
	funcs := templateFuncs(assetsPrefix)
	i18nTemplateFuncs(funcs, t.I18n, locale)
	envTemplateFuncs(funcs, t.Config.ExposedEnvVars)
	barcodeTemplateFuncs(funcs)

	parsed, err := template.New("template.html").Funcs(funcs).Parse(t.String())
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	for _, p := range t.Partials {
		if _, err := parsed.New(p.Name).Parse(p.Source); err != nil {
			return nil, fmt.Errorf("parse partial %s: %w", p.Name, err)
		}
	}

	return parsed, nil
}

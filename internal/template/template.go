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

// Template represents a template
type Template struct {
	bytes.Buffer
	Config  Config
	Schema  *jsonschema.Schema
	Assets  fs.FS
	Example map[string]any
	I18n    *i18n.I18n
}

// Render the template with the given values to the output
func (t *Template) Render(values map[string]any, assetsPrefix string, locale string, out io.Writer) error {
	funcs := templateFuncs(assetsPrefix)
	i18nTemplateFuncs(funcs, t.I18n, locale)
	envTemplateFuncs(funcs, t.Config.ExposedEnvVars)

	renderer := template.New("main").Funcs(funcs)
	parsed, err := renderer.Parse(t.String())
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	err = parsed.Execute(out, values)
	if err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	return nil
}

package template_test

import (
	"strings"
	"testing"

	"github.com/kaptinlin/go-i18n"
	"github.com/kaptinlin/jsonschema"
	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestValidate(t *testing.T) {
	compileSchema := func(t *testing.T, schema string) *jsonschema.Schema {
		t.Helper()
		compiled, err := jsonschema.NewCompiler().Compile([]byte(schema))
		require.NoError(t, err)
		return compiled
	}

	localizedTemplate := func(t *testing.T) *template.Template {
		t.Helper()
		bundle := i18n.NewBundle(
			i18n.WithUnmarshaler(yaml.Unmarshal),
			i18n.WithDefaultLocale("en"),
			i18n.WithLocales("de", "en"),
		)
		require.NoError(t, bundle.LoadMessages(map[string]map[string]string{
			"de": {"hello": "Hallo"},
			"en": {"hello": "Hello"},
		}))

		tmpl := &template.Template{I18n: bundle}
		tmpl.Config.Locale = &struct {
			Locales []string `yaml:"locales"`
			Default string   `yaml:"default"`
		}{Locales: []string{"de", "en"}, Default: "en"}
		return tmpl
	}

	t.Run("it_passes_a_valid_template_without_example_data", func(t *testing.T) {
		tmpl := &template.Template{
			Partials: []template.Partial{{Name: "header.html", Source: "<h1>{{.title}}</h1>"}},
		}
		tmpl.WriteString(`{{template "header.html" .}}{{.name | upper}}`)

		assert.NoError(t, tmpl.Validate())
	})

	t.Run("it_reports_syntax_errors_with_the_file_name_and_line", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("line 1\n{{if .name}}")

		err := tmpl.Validate()

		assert.ErrorContains(t, err, "parse template: template: template.html:2:")
	})

	t.Run("it_reports_undefined_functions", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("{{.name | qrcode}}")

		err := tmpl.Validate()

		assert.ErrorContains(t, err, `function "qrcode" not defined`)
	})

	t.Run("it_reports_syntax_errors_in_partials", func(t *testing.T) {
		tmpl := &template.Template{
			Partials: []template.Partial{{Name: "sections/broken.html", Source: "{{end}}"}},
		}
		tmpl.WriteString("Hello")

		err := tmpl.Validate()

		assert.ErrorContains(t, err, "parse partial sections/broken.html: template: sections/broken.html:1:")
	})

	t.Run("it_passes_a_template_with_valid_example_data", func(t *testing.T) {
		tmpl := &template.Template{
			Schema:  compileSchema(t, `{"type": "object", "properties": {"name": {"type": "string"}}}`),
			Example: map[string]any{"name": "World"},
		}
		tmpl.WriteString("Hello {{.name}}")

		assert.NoError(t, tmpl.Validate())
	})

	t.Run("it_reports_where_and_why_example_data_doesnt_match_the_schema", func(t *testing.T) {
		tmpl := &template.Template{
			Schema: compileSchema(t, `{
				"type": "object",
				"required": ["name", "count"],
				"properties": {
					"name": {"type": "string"},
					"items": {"type": "array", "items": {
						"type": "object",
						"properties": {"qty": {"type": "integer", "minimum": 1}}
					}}
				}
			}`),
			Example: map[string]any{
				"name":  42.0,
				"items": []any{map[string]any{"qty": 0.0}},
			},
		}
		tmpl.WriteString("Hello {{.name}}")

		err := tmpl.Validate()

		require.Error(t, err)
		assert.Equal(t, `example data doesn't match the schema:
/: Required property 'count' is missing
/name: Value is integer but should be string
/items/0/qty: 0 should be at least 1`, err.Error())
	})

	t.Run("it_reports_errors_when_rendering_the_example_data", func(t *testing.T) {
		tmpl := &template.Template{Example: map[string]any{}}
		tmpl.WriteString(`{{template "missing.html" .}}`)

		err := tmpl.Validate()

		assert.ErrorContains(t, err, `render example data: execute template: template: template.html:1:11: executing "template.html"`)
		assert.ErrorContains(t, err, `template "missing.html" not defined`)
	})

	t.Run("it_renders_the_example_data_in_every_locale", func(t *testing.T) {
		tmpl := localizedTemplate(t)
		tmpl.Example = map[string]any{}
		tmpl.WriteString(`{{if eq locale "de"}}{{index .missing 0}}{{end}}`)

		err := tmpl.Validate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render example data (locale de): execute template:")
		assert.NotContains(t, err.Error(), "de, en")
	})

	t.Run("it_reports_an_error_that_occurs_in_every_locale_once", func(t *testing.T) {
		tmpl := localizedTemplate(t)
		tmpl.Example = map[string]any{}
		tmpl.WriteString(`{{tr "hello"}} {{index .missing 0}}`)

		err := tmpl.Validate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render example data (locale de, en): execute template:")
		assert.Equal(t, 1, strings.Count(err.Error(), "render example data"))
	})
}

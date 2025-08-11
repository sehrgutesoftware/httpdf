package template_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/kaptinlin/go-i18n"
	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRender(t *testing.T) {
	t.Run("it_renders_simple_template_with_values", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("Hello {{.name}}!")

		var output bytes.Buffer
		values := map[string]any{"name": "World"}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Hello World!", output.String())
	})

	t.Run("it_includes_assets_prefix_in_template_context", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("Assets at: {{.__assets__}}")

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/static/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Assets at: /static/assets", output.String())
	})

	t.Run("it_returns_an_error_if_the_template_cant_be_parsed", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("{{.invalid syntax")

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parse template")
	})

	t.Run("it_returns_an_error_if_template_execution_fails", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("{{index .items \"badkey\"}}")

		var output bytes.Buffer
		values := map[string]any{"items": []string{"a", "b"}}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "execute template")
	})

	t.Run("it_uses_sprig_template_functions", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("{{.name | upper}}")

		var output bytes.Buffer
		values := map[string]any{"name": "world"}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "WORLD", output.String())
	})

	t.Run("it_uses_chunk_template_function", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("{{range chunk .items 2}}{{len .}} {{end}}")

		var output bytes.Buffer
		values := map[string]any{
			"items": []any{"a", "b", "c", "d", "e"},
		}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "2 2 1 ", output.String())
	})

	t.Run("it_handles_templates_without_i18n", func(t *testing.T) {
		tmpl := &template.Template{
			I18n: nil,
		}
		tmpl.WriteString("Hello {{.name}}")

		var output bytes.Buffer
		values := map[string]any{"name": "Test"}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Hello Test", output.String())
	})

	t.Run("it_sets_locale_in_template_context_when_i18n_is_available", func(t *testing.T) {
		i18nInstance := i18n.NewBundle(
			i18n.WithUnmarshaler(yaml.Unmarshal),
			i18n.WithDefaultLocale("en"),
			i18n.WithLocales("en"),
		)

		tmpl := &template.Template{
			I18n: i18nInstance,
		}
		tmpl.WriteString("Locale: {{.__locale__}}")

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Contains(t, output.String(), "Locale:")
	})

	t.Run("it_handles_tr_function_when_localizer_is_nil", func(t *testing.T) {
		tmpl := &template.Template{
			I18n: nil,
		}
		tmpl.WriteString("{{tr \"hello\"}}")

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "!(localizer is nil)", output.String())
	})

	t.Run("it_renders_complex_template_with_multiple_features", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString(`Assets: {{.__assets__}}
{{- $items := .items -}}
{{- $chunks := chunk $items 2 -}}
{{range $chunks}}Items: {{. | join ", "}}
{{end}}Name: {{.name | title}}`)

		var output bytes.Buffer
		values := map[string]any{
			"name":  "john doe",
			"items": []any{"apple", "banana", "cherry", "date"},
		}

		err := tmpl.Render(values, "/my-assets", "en", &output)

		assert.NoError(t, err)
		result := output.String()
		assert.Contains(t, result, "Assets: /my-assets")
		assert.Contains(t, result, "Items: apple, banana")
		assert.Contains(t, result, "Items: cherry, date")
		assert.Contains(t, result, "Name: John Doe")
	})

	t.Run("it_handles_empty_values_map", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("Static content only")

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Static content only", output.String())
	})

	t.Run("it_handles_nil_values_in_template", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("Value: {{if .value}}{{.value}}{{else}}default{{end}}")

		var output bytes.Buffer
		values := map[string]any{"value": nil}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Value: default", output.String())
	})

	t.Run("it_uses_env_function_with_exposed_variables", func(t *testing.T) {
		// Set up test environment variables
		err := os.Setenv("EXPOSED_VAR", "secret_value")
		require.NoError(t, err)
		defer os.Unsetenv("EXPOSED_VAR")

		err = os.Setenv("HIDDEN_VAR", "hidden_secret")
		require.NoError(t, err)
		defer os.Unsetenv("HIDDEN_VAR")

		tmpl := &template.Template{
			Config: template.Config{
				ExposedEnvVars: []string{"EXPOSED_VAR"},
			},
		}
		tmpl.WriteString("Exposed: {{env \"EXPOSED_VAR\"}}, Hidden: {{env \"HIDDEN_VAR\"}}, Missing: {{env \"MISSING_VAR\"}}")

		var output bytes.Buffer
		values := map[string]any{}

		err = tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Exposed: secret_value, Hidden: , Missing: ", output.String())
	})

	t.Run("it_handles_env_function_with_empty_exposed_list", func(t *testing.T) {
		err := os.Setenv("ANY_VAR", "any_value")
		require.NoError(t, err)
		defer os.Unsetenv("ANY_VAR")

		tmpl := &template.Template{
			Config: template.Config{
				ExposedEnvVars: []string{},
			},
		}
		tmpl.WriteString("Value: {{env \"ANY_VAR\"}}")

		var output bytes.Buffer
		values := map[string]any{}

		err = tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Value: ", output.String())
	})
}

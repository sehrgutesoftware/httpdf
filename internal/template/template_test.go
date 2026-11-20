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
		tmpl.WriteString("Assets at: {{asset \"assets\"}}")

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/static", "en", &output)

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

	t.Run("it_renders_partials_by_name", func(t *testing.T) {
		tmpl := &template.Template{
			Partials: []template.Partial{
				{Name: "header.html", Source: "<h1>{{.title}}</h1>"},
				{Name: "sections/items.html", Source: "{{range .items}}<li>{{.}}</li>{{end}}"},
			},
		}
		tmpl.WriteString(`{{template "header.html" .}}<ul>{{template "sections/items.html" .}}</ul>`)

		var output bytes.Buffer
		values := map[string]any{"title": "List", "items": []any{"a", "b"}}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "<h1>List</h1><ul><li>a</li><li>b</li></ul>", output.String())
	})

	t.Run("it_makes_templates_defined_in_partials_available", func(t *testing.T) {
		tmpl := &template.Template{
			Partials: []template.Partial{
				{Name: "macros.html", Source: `{{define "greeting"}}Hello {{.}}!{{end}}`},
			},
		}
		tmpl.WriteString(`{{template "greeting" .name}}`)

		var output bytes.Buffer
		values := map[string]any{"name": "World"}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "Hello World!", output.String())
	})

	t.Run("it_makes_template_functions_available_in_partials", func(t *testing.T) {
		tmpl := &template.Template{
			Partials: []template.Partial{
				{Name: "logo.html", Source: `<img src="{{asset "logo.png"}}">{{.name | upper}}`},
			},
		}
		tmpl.WriteString(`{{template "logo.html" .}}`)

		var output bytes.Buffer
		values := map[string]any{"name": "acme"}

		err := tmpl.Render(values, "/static", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, `<img src="/static/logo.png">ACME`, output.String())
	})

	t.Run("it_lets_partials_override_blocks_of_the_main_template", func(t *testing.T) {
		tmpl := &template.Template{
			Partials: []template.Partial{
				{Name: "content.html", Source: `{{define "content"}}custom{{end}}`},
			},
		}
		tmpl.WriteString(`[{{block "content" .}}default{{end}}]`)

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "[custom]", output.String())
	})

	t.Run("it_returns_an_error_if_a_partial_cant_be_parsed", func(t *testing.T) {
		tmpl := &template.Template{
			Partials: []template.Partial{
				{Name: "broken.html", Source: "{{.invalid syntax"},
			},
		}
		tmpl.WriteString("Hello")

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parse partial broken.html")
	})

	t.Run("it_returns_an_error_if_a_referenced_partial_does_not_exist", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString(`{{template "missing.html" .}}`)

		var output bytes.Buffer
		values := map[string]any{}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "execute template")
	})

	t.Run("it_escapes_values_according_to_their_context", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString(`<p title="{{.name}}">{{.name}}</p><a href="{{.url}}"></a><script>var name = {{.name}};</script>`)

		var output bytes.Buffer
		values := map[string]any{
			"name": `<script>alert("hi")</script>`,
			"url":  "javascript:alert(1)",
		}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, `<p title="&lt;script&gt;alert(&#34;hi&#34;)&lt;/script&gt;">&lt;script&gt;alert(&#34;hi&#34;)&lt;/script&gt;</p>`+
			`<a href="#ZgotmplZ"></a>`+
			`<script>var name = "\u003cscript\u003ealert(\"hi\")\u003c/script\u003e";</script>`, output.String())
	})

	t.Run("it_inserts_values_marked_as_trusted_without_escaping", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString(`{{.html | safeHTML}}<img src="{{.logo | safeURL}}"><p style="color: {{.color | safeCSS}}"></p>`)

		var output bytes.Buffer
		values := map[string]any{
			"html":  "<strong>bold</strong>",
			"logo":  "data:image/png;base64,AAAA",
			"color": "rgb(0, 0, 0)",
		}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, `<strong>bold</strong><img src="data:image/png;base64,AAAA"><p style="color: rgb(0, 0, 0)"></p>`, output.String())
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

	t.Run("it_uses_a_chunk_size_from_the_request_data", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("{{range chunk .items .columns}}{{len .}} {{end}}")

		var output bytes.Buffer
		values := map[string]any{
			"items":   []any{"a", "b", "c", "d", "e"},
			"columns": 2.0, // JSON numbers are float64
		}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Equal(t, "2 2 1 ", output.String())
	})

	t.Run("it_returns_an_error_for_a_chunk_size_of_0", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString("{{range chunk .items .columns}}{{len .}} {{end}}")

		var output bytes.Buffer
		values := map[string]any{"items": []any{"a"}, "columns": 0.0}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.ErrorContains(t, err, "chunk: size must be greater than 0, got 0")
	})

	t.Run("it_uses_a_qr_code_size_from_the_request_data", func(t *testing.T) {
		tmpl := &template.Template{}
		tmpl.WriteString(`<img src="{{qrCode .size .url}}">`)

		var output bytes.Buffer
		values := map[string]any{
			"size": 128.0, // JSON numbers are float64
			"url":  "https://example.com",
		}

		err := tmpl.Render(values, "/assets", "en", &output)

		assert.NoError(t, err)
		assert.Contains(t, output.String(), `<img src="data:image/png;base64,`)
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
		tmpl.WriteString("Locale: {{locale}}")

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
		tmpl.WriteString(`Assets: {{asset "assets"}}
{{- $items := .items -}}
{{- $chunks := chunk $items 2 -}}
{{range $chunks}}Items: {{. | join ", "}}
{{end}}Name: {{.name | title}}`)

		var output bytes.Buffer
		values := map[string]any{
			"name":  "john doe",
			"items": []any{"apple", "banana", "cherry", "date"},
		}

		err := tmpl.Render(values, "/my", "en", &output)

		assert.NoError(t, err)
		result := output.String()
		assert.Contains(t, result, "Assets: /my/assets")
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

	t.Run("it_doesnt_provide_sprigs_unrestricted_env_functions", func(t *testing.T) {
		t.Setenv("HIDDEN_VAR", "hidden_secret")

		for _, call := range []string{`{{expandenv "$HIDDEN_VAR"}}`, `{{getHostByName "localhost"}}`} {
			tmpl := &template.Template{}
			tmpl.WriteString(call)

			var output bytes.Buffer
			err := tmpl.Render(map[string]any{}, "/assets", "en", &output)

			assert.ErrorContains(t, err, "not defined")
			assert.Empty(t, output.String())
		}

		tmpl := &template.Template{}
		tmpl.WriteString(`{{env "HIDDEN_VAR"}}`)
		var output bytes.Buffer
		require.NoError(t, tmpl.Render(map[string]any{}, "/assets", "en", &output))
		assert.Empty(t, output.String())
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

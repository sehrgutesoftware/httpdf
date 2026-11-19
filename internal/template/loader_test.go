package template_test

import (
	"encoding/json"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFSLoader(t *testing.T) {
	t.Run("it_creates_a_new_fsloader_with_provided_filesystem", func(t *testing.T) {
		mockFS := fstest.MapFS{}
		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)

		loader := template.NewFSLoader(subFS.(fs.SubFS))

		assert.NotNil(t, loader)
		var _ template.Loader = loader
	})
}

func TestFSLoader_Load(t *testing.T) {
	t.Run("it_loads_a_complete_template_with_all_components", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"test-template/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Hello {{.name}}!</body></html>`),
			},
			"test-template/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297
locale:
  locales:
    - en
    - de
  default: en`),
			},
			"test-template/schema.json": &fstest.MapFile{
				Data: []byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "name": {"type": "string"}
  }
}`),
			},
			"test-template/example.json": &fstest.MapFile{
				Data: []byte(`{"name": "World"}`),
			},
			"test-template/assets/style.css": &fstest.MapFile{
				Data: []byte(`body { margin: 0; }`),
			},
			"test-template/locales/en.yaml": &fstest.MapFile{
				Data: []byte(`hello: Hello`),
			},
			"test-template/locales/de.yaml": &fstest.MapFile{
				Data: []byte(`hello: Hallo`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("test-template")

		assert.NoError(t, err)
		assert.NotNil(t, tmpl)
		assert.Contains(t, tmpl.String(), "Hello {{.name}}!")
		assert.Equal(t, 210.0, tmpl.Config.Page.Width)
		assert.Equal(t, 297.0, tmpl.Config.Page.Height)
		assert.Equal(t, "en", tmpl.Config.Locale.Default)
		assert.Contains(t, tmpl.Config.Locale.Locales, "en")
		assert.Contains(t, tmpl.Config.Locale.Locales, "de")
		assert.NotNil(t, tmpl.Schema)
		assert.NotNil(t, tmpl.Assets)
		assert.NotNil(t, tmpl.I18n)
		assert.Equal(t, "World", tmpl.Example["name"])
	})

	t.Run("it_loads_template_without_optional_components", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"minimal-template/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Minimal template</body></html>`),
			},
			"minimal-template/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
			"minimal-template/schema.json": &fstest.MapFile{
				Data: []byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object"
}`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("minimal-template")

		assert.NoError(t, err)
		assert.NotNil(t, tmpl)
		assert.Contains(t, tmpl.String(), "Minimal template")
		assert.Equal(t, 210.0, tmpl.Config.Page.Width)
		assert.Equal(t, 297.0, tmpl.Config.Page.Height)
		assert.Nil(t, tmpl.Config.Locale)
		assert.NotNil(t, tmpl.Schema)
		assert.Nil(t, tmpl.Partials)
		assert.Nil(t, tmpl.Assets)
		assert.Nil(t, tmpl.I18n)
		assert.Nil(t, tmpl.Example)
	})

	t.Run("it_loads_partials_when_partials_directory_exists", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"with-partials/template.html": &fstest.MapFile{
				Data: []byte(`{{template "header.html" .}}`),
			},
			"with-partials/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
			"with-partials/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
			"with-partials/partials/header.html": &fstest.MapFile{
				Data: []byte(`<header>{{.title}}</header>`),
			},
			"with-partials/partials/sections/items.html": &fstest.MapFile{
				Data: []byte(`{{range .items}}<li>{{.}}</li>{{end}}`),
			},
			"with-partials/partials/notes.txt": &fstest.MapFile{
				Data: []byte(`not a partial`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("with-partials")

		assert.NoError(t, err)
		assert.Equal(t, []template.Partial{
			{Name: "header.html", Source: `<header>{{.title}}</header>`},
			{Name: "sections/items.html", Source: `{{range .items}}<li>{{.}}</li>{{end}}`},
		}, tmpl.Partials)
	})

	t.Run("it_returns_template_not_found_error_when_template_html_is_missing", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"incomplete-template/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
			"incomplete-template/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("incomplete-template")

		assert.Error(t, err)
		assert.Nil(t, tmpl)
		assert.ErrorIs(t, err, template.ErrTemplateNotFound)
		assert.Contains(t, err.Error(), "template.html")
	})

	t.Run("it_returns_template_not_found_error_when_config_yaml_is_missing", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"incomplete-template/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Test</body></html>`),
			},
			"incomplete-template/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("incomplete-template")

		assert.Error(t, err)
		assert.Nil(t, tmpl)
		assert.ErrorIs(t, err, template.ErrTemplateNotFound)
		assert.Contains(t, err.Error(), "config.yaml")
	})

	t.Run("it_returns_template_not_found_error_when_schema_json_is_missing", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"incomplete-template/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Test</body></html>`),
			},
			"incomplete-template/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("incomplete-template")

		assert.Error(t, err)
		assert.Nil(t, tmpl)
		assert.ErrorIs(t, err, template.ErrTemplateNotFound)
		assert.Contains(t, err.Error(), "schema.json")
	})

	t.Run("it_returns_error_when_template_name_does_not_exist", func(t *testing.T) {
		mockFS := fstest.MapFS{}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("nonexistent-template")

		assert.Error(t, err)
		assert.Nil(t, tmpl)
		assert.ErrorIs(t, err, template.ErrTemplateNotFound)
	})

	t.Run("it_returns_error_when_config_yaml_is_malformed", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"bad-config/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Test</body></html>`),
			},
			"bad-config/config.yaml": &fstest.MapFile{
				Data: []byte(`invalid: yaml: content: [malformed`),
			},
			"bad-config/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("bad-config")

		assert.Error(t, err)
		assert.Nil(t, tmpl)
		assert.Contains(t, err.Error(), "decode config file")
	})

	t.Run("it_returns_error_when_schema_json_is_malformed", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"bad-schema/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Test</body></html>`),
			},
			"bad-schema/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
			"bad-schema/schema.json": &fstest.MapFile{
				Data: []byte(`{invalid json content`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("bad-schema")

		assert.Error(t, err)
		assert.Nil(t, tmpl)
		assert.Contains(t, err.Error(), "compile schema")
	})

	t.Run("it_returns_error_when_example_json_is_malformed", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"bad-example/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Test</body></html>`),
			},
			"bad-example/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
			"bad-example/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
			"bad-example/example.json": &fstest.MapFile{
				Data: []byte(`{invalid json`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("bad-example")

		assert.Error(t, err)
		assert.Nil(t, tmpl)
		assert.Contains(t, err.Error(), "decode example data file")
	})

	t.Run("it_loads_assets_when_assets_directory_exists", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"with-assets/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>Test</body></html>`),
			},
			"with-assets/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
			"with-assets/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
			"with-assets/assets/style.css": &fstest.MapFile{
				Data: []byte(`body { margin: 0; }`),
			},
			"with-assets/assets/script.js": &fstest.MapFile{
				Data: []byte(`console.log("hello");`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("with-assets")

		assert.NoError(t, err)
		assert.NotNil(t, tmpl.Assets)

		// Verify assets can be accessed
		files, err := fs.ReadDir(tmpl.Assets, ".")
		assert.NoError(t, err)
		assert.Len(t, files, 2)

		fileNames := make([]string, len(files))
		for i, file := range files {
			fileNames[i] = file.Name()
		}
		assert.Contains(t, fileNames, "style.css")
		assert.Contains(t, fileNames, "script.js")
	})

	t.Run("it_loads_i18n_when_locale_config_and_files_exist", func(t *testing.T) {
		mockFS := fstest.MapFS{
			"with-i18n/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>{{tr "hello"}}</body></html>`),
			},
			"with-i18n/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297
locale:
  locales:
    - en
    - fr
  default: en`),
			},
			"with-i18n/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
			"with-i18n/locales/en.yaml": &fstest.MapFile{
				Data: []byte(`hello: Hello`),
			},
			"with-i18n/locales/fr.yaml": &fstest.MapFile{
				Data: []byte(`hello: Bonjour`),
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("with-i18n")

		assert.NoError(t, err)
		assert.NotNil(t, tmpl.I18n)
		assert.Equal(t, "en", tmpl.Config.Locale.Default)
		assert.Contains(t, tmpl.Config.Locale.Locales, "en")
		assert.Contains(t, tmpl.Config.Locale.Locales, "fr")
	})

	t.Run("it_loads_example_data_when_example_json_exists", func(t *testing.T) {
		exampleData := map[string]any{
			"title":   "Test Title",
			"count":   42,
			"enabled": true,
		}
		exampleJSON, _ := json.Marshal(exampleData)

		mockFS := fstest.MapFS{
			"with-example/template.html": &fstest.MapFile{
				Data: []byte(`<html><body>{{.title}}</body></html>`),
			},
			"with-example/config.yaml": &fstest.MapFile{
				Data: []byte(`page:
  width: 210
  height: 297`),
			},
			"with-example/schema.json": &fstest.MapFile{
				Data: []byte(`{"type": "object"}`),
			},
			"with-example/example.json": &fstest.MapFile{
				Data: exampleJSON,
			},
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		loader := template.NewFSLoader(subFS.(fs.SubFS))

		tmpl, err := loader.Load("with-example")

		assert.NoError(t, err)
		assert.NotNil(t, tmpl.Example)
		assert.Equal(t, "Test Title", tmpl.Example["title"])
		assert.Equal(t, float64(42), tmpl.Example["count"]) // JSON numbers are float64
		assert.Equal(t, true, tmpl.Example["enabled"])
	})
}

func TestFSLoader_LoadStrict(t *testing.T) {
	// load loads a template consisting of the given config and translation
	// files, plus a minimal template.html and schema.json
	load := func(t *testing.T, config string, locales []string, opts ...template.LoaderOption) (*template.Template, error) {
		t.Helper()
		mockFS := fstest.MapFS{
			"tpl/template.html": &fstest.MapFile{Data: []byte(`Hello`)},
			"tpl/schema.json":   &fstest.MapFile{Data: []byte(`{"type": "object"}`)},
			"tpl/config.yaml":   &fstest.MapFile{Data: []byte(config)},
		}
		for _, locale := range locales {
			mockFS["tpl/locales/"+locale+".yaml"] = &fstest.MapFile{Data: []byte(`hello: Hello`)}
		}

		subFS, err := fs.Sub(mockFS, ".")
		require.NoError(t, err)
		return template.NewFSLoader(subFS.(fs.SubFS), opts...).Load("tpl")
	}

	t.Run("it_loads_a_valid_template", func(t *testing.T) {
		config := `page:
  width: 210
  height: 297
locale:
  locales: [de, en_US]
  default: en-us
pdf:
  generateTaggedPDF: true`

		tmpl, err := load(t, config, []string{"de", "en-US"}, template.WithStrict())

		assert.NoError(t, err)
		assert.NotNil(t, tmpl)
	})

	t.Run("it_rejects_unknown_config_keys", func(t *testing.T) {
		config := `page:
  width: 210
  heigth: 297
pdf:
  generateTaggedPdf: true`

		tmpl, err := load(t, config, nil, template.WithStrict())

		assert.Nil(t, tmpl)
		assert.ErrorContains(t, err, "decode config file")
		assert.ErrorContains(t, err, `line 3: unknown key "heigth"`)
		assert.ErrorContains(t, err, `line 5: unknown key "generateTaggedPdf"`)
	})

	t.Run("it_ignores_unknown_config_keys_when_not_strict", func(t *testing.T) {
		config := `page:
  width: 210
  height: 297
unknown: true`

		tmpl, err := load(t, config, nil)

		assert.NoError(t, err)
		assert.NotNil(t, tmpl)
	})

	t.Run("it_rejects_missing_page_dimensions", func(t *testing.T) {
		tmpl, err := load(t, `page: {width: 0}`, nil, template.WithStrict())

		assert.Nil(t, tmpl)
		assert.ErrorContains(t, err, "invalid config")
		assert.ErrorContains(t, err, "page.width must be greater than 0")
		assert.ErrorContains(t, err, "page.height must be greater than 0")
	})

	t.Run("it_rejects_an_empty_list_of_locales", func(t *testing.T) {
		config := `page: {width: 210, height: 297}
locale:
  default: en`

		tmpl, err := load(t, config, []string{"en"}, template.WithStrict())

		assert.Nil(t, tmpl)
		assert.ErrorContains(t, err, "locale.locales must not be empty")
	})

	t.Run("it_rejects_a_default_locale_thats_not_in_the_list", func(t *testing.T) {
		config := `page: {width: 210, height: 297}
locale:
  locales: [de, en]
  default: fr`

		tmpl, err := load(t, config, []string{"de", "en", "fr"}, template.WithStrict())

		assert.Nil(t, tmpl)
		assert.ErrorContains(t, err, `locale.default "fr" must be one of locale.locales`)
	})

	t.Run("it_rejects_locales_without_a_translation_file", func(t *testing.T) {
		config := `page: {width: 210, height: 297}
locale:
  locales: [de, en, fr]`

		tmpl, err := load(t, config, []string{"de"}, template.WithStrict())

		assert.Nil(t, tmpl)
		assert.ErrorContains(t, err, `missing translation file locales/en.yaml for locale "en"`)
		assert.ErrorContains(t, err, `missing translation file locales/fr.yaml for locale "fr"`)
	})
}

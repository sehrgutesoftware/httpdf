package httpdf_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/kaptinlin/jsonschema"
	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fetchingRenderer mimics a browser: it loads the page and uses its body as
// the output, regardless of the response status.
type fetchingRenderer struct {
	called bool
}

func (r *fetchingRenderer) Render(ctx context.Context, url string, out io.Writer, opts pdf.RenderOpts) error {
	r.called = true
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(out, res.Body)
	return err
}

func newTemplate(t *testing.T, content string, schema string) *template.Template {
	t.Helper()
	compiled, err := jsonschema.NewCompiler().Compile([]byte(schema))
	require.NoError(t, err)
	tmpl := &template.Template{Schema: compiled}
	tmpl.WriteString(content)
	return tmpl
}

func TestHTTPDF_Generate(t *testing.T) {
	t.Run("it_renders_the_template_with_the_given_values", func(t *testing.T) {
		renderer := &fetchingRenderer{}
		tmpl := newTemplate(t, `Hello {{ .name }}`, `{}`)

		var out bytes.Buffer
		err := httpdf.New(renderer).Generate(context.Background(), tmpl, "", map[string]any{"name": "World"}, &out)

		require.NoError(t, err)
		assert.Equal(t, "Hello World", out.String())
	})

	t.Run("it_returns_an_error_on_invalid_values", func(t *testing.T) {
		renderer := &fetchingRenderer{}
		tmpl := newTemplate(t, `Hello {{ .name }}`, `{"type": "object", "required": ["name"]}`)

		var out bytes.Buffer
		err := httpdf.New(renderer).Generate(context.Background(), tmpl, "", map[string]any{}, &out)

		assert.ErrorIs(t, err, httpdf.ErrInvalidValues)
		assert.False(t, renderer.called, "renderer must not be called")
		assert.Empty(t, out.String())
	})

	t.Run("it_returns_an_error_when_template_execution_fails", func(t *testing.T) {
		renderer := &fetchingRenderer{}
		tmpl := newTemplate(t, `{{ fail "boom" }}`, `{}`)

		var out bytes.Buffer
		err := httpdf.New(renderer).Generate(context.Background(), tmpl, "", map[string]any{}, &out)

		assert.ErrorContains(t, err, "boom")
		assert.False(t, renderer.called, "renderer must not be called")
		assert.Empty(t, out.String())
	})
}

package httpdf_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/stretchr/testify/assert"
)

type stubLoader struct {
	templates map[string]*template.Template
	loads     atomic.Int32
}

func (l *stubLoader) Load(name string) (*template.Template, error) {
	l.loads.Add(1)
	if t, ok := l.templates[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("%w: %s", template.ErrTemplateNotFound, name)
}

type stubHTTPDF struct {
	err error
}

func (h *stubHTTPDF) Generate(ctx context.Context, t *template.Template, locale string, v map[string]any, w io.Writer) error {
	if h.err != nil {
		return h.err
	}
	_, err := w.Write([]byte("%PDF-1.4"))
	return err
}

func TestServer_Render(t *testing.T) {
	loader := &stubLoader{templates: map[string]*template.Template{
		"known": {},
	}}

	render := func(generator httpdf.HTTPDF, tmpl string, body string) *httptest.ResponseRecorder {
		server := httpdf.NewServer(generator, loader)
		req := httptest.NewRequest(http.MethodPost, "/templates/"+tmpl+"/render", strings.NewReader(body))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec
	}

	t.Run("it_responds_with_pdf_on_success", func(t *testing.T) {
		rec := render(&stubHTTPDF{}, "known", `{}`)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
		assert.Equal(t, "%PDF-1.4", rec.Body.String())
	})

	t.Run("it_responds_with_400_on_invalid_json", func(t *testing.T) {
		rec := render(&stubHTTPDF{}, "known", `{not json`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("it_responds_with_404_on_unknown_template", func(t *testing.T) {
		rec := render(&stubHTTPDF{}, "unknown", `{}`)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("it_responds_with_422_on_invalid_values", func(t *testing.T) {
		err := fmt.Errorf("%w: name is required", httpdf.ErrInvalidValues)
		rec := render(&stubHTTPDF{err: err}, "known", `{}`)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Contains(t, rec.Body.String(), "name is required")
	})

	t.Run("it_responds_with_500_on_other_errors", func(t *testing.T) {
		rec := render(&stubHTTPDF{err: errors.New("browser exploded")}, "known", `{}`)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestServer_TemplateCache(t *testing.T) {
	t.Run("it_loads_a_template_once_under_concurrent_requests", func(t *testing.T) {
		loader := &stubLoader{templates: map[string]*template.Template{
			"known": {},
		}}
		server := httpdf.NewServer(&stubHTTPDF{}, loader)

		var wg sync.WaitGroup
		for range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, "/templates/known/render", strings.NewReader(`{}`))
				rec := httptest.NewRecorder()
				server.ServeHTTP(rec, req)
				assert.Equal(t, http.StatusOK, rec.Code)
			}()
		}
		wg.Wait()

		assert.Equal(t, int32(1), loader.loads.Load())
	})
}

package httpdf_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

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
	url    string
}

func (r *fetchingRenderer) Render(ctx context.Context, url string, out io.Writer, opts pdf.RenderOpts) error {
	r.called = true
	r.url = url
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

// assetRenderer requests the given path from the temporary server and writes
// the response status code to the output.
type assetRenderer struct {
	path string
}

func (r *assetRenderer) Render(ctx context.Context, url string, out io.Writer, opts pdf.RenderOpts) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+r.path, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	_, err = fmt.Fprint(out, res.StatusCode)
	return err
}

// blockingRenderer signals on started when a render begins, and blocks until
// release is closed or, unless ignoreContext is set, the context is done.
type blockingRenderer struct {
	ignoreContext bool
	started       chan struct{}
	release       chan struct{}
	calls         atomic.Int32
}

func newBlockingRenderer(ignoreContext bool) *blockingRenderer {
	return &blockingRenderer{
		ignoreContext: ignoreContext,
		started:       make(chan struct{}, 10),
		release:       make(chan struct{}),
	}
}

func (r *blockingRenderer) Render(ctx context.Context, url string, out io.Writer, opts pdf.RenderOpts) error {
	r.calls.Add(1)
	r.started <- struct{}{}

	done := ctx.Done()
	if r.ignoreContext {
		done = nil
	}
	select {
	case <-r.release:
		_, err := io.WriteString(out, "%PDF")
		return err
	case <-done:
		return ctx.Err()
	}
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

	t.Run("it_serves_the_page_on_loopback_only", func(t *testing.T) {
		renderer := &fetchingRenderer{}
		tmpl := newTemplate(t, `Hello`, `{}`)

		err := httpdf.New(renderer).Generate(context.Background(), tmpl, "", map[string]any{}, io.Discard)

		require.NoError(t, err)
		u, err := url.Parse(renderer.url)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", u.Hostname())
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

	t.Run("it_responds_with_404_for_assets_when_template_has_none", func(t *testing.T) {
		renderer := &assetRenderer{path: "/assets/style.css"}
		tmpl := newTemplate(t, `<link rel="stylesheet" href="/assets/style.css">`, `{}`)

		var out bytes.Buffer
		err := httpdf.New(renderer).Generate(context.Background(), tmpl, "", map[string]any{}, &out)

		require.NoError(t, err)
		assert.Equal(t, "404", out.String())
	})
}

func TestHTTPDF_Limits(t *testing.T) {
	tmpl := newTemplate(t, `Hello {{ .name }}`, `{"type": "object", "required": ["name"]}`)
	values := map[string]any{"name": "World"}

	// generate runs Generate in the background and sends its error
	generate := func(ctx context.Context, h httpdf.HTTPDF, values map[string]any) <-chan error {
		done := make(chan error, 1)
		go func() {
			done <- h.Generate(ctx, tmpl, "", values, io.Discard)
		}()
		return done
	}

	t.Run("it_waits_for_a_free_render_slot", func(t *testing.T) {
		renderer := newBlockingRenderer(false)
		h := httpdf.New(renderer, httpdf.WithMaxConcurrentRenders(1), httpdf.WithRenderTimeout(5*time.Second))

		first := generate(t.Context(), h, values)
		<-renderer.started
		second := generate(t.Context(), h, values)

		// The second render must not start while the first one is running
		select {
		case <-renderer.started:
			t.Fatal("the second render started before the first one finished")
		case <-time.After(100 * time.Millisecond):
		}

		close(renderer.release)
		assert.NoError(t, <-first)
		assert.NoError(t, <-second)
		assert.Equal(t, int32(2), renderer.calls.Load())
	})

	t.Run("it_returns_ErrBusy_when_no_render_slot_becomes_available_in_time", func(t *testing.T) {
		renderer := newBlockingRenderer(true)
		defer close(renderer.release)
		h := httpdf.New(renderer, httpdf.WithMaxConcurrentRenders(1), httpdf.WithRenderTimeout(100*time.Millisecond))

		generate(t.Context(), h, values)
		<-renderer.started
		err := <-generate(t.Context(), h, values)

		assert.ErrorIs(t, err, httpdf.ErrBusy)
		assert.Equal(t, int32(1), renderer.calls.Load(), "the second render must not start")
	})

	t.Run("it_returns_ErrRenderTimeout_when_rendering_takes_too_long", func(t *testing.T) {
		renderer := newBlockingRenderer(false)
		h := httpdf.New(renderer, httpdf.WithRenderTimeout(100*time.Millisecond))

		err := <-generate(t.Context(), h, values)

		assert.ErrorIs(t, err, httpdf.ErrRenderTimeout)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("it_returns_the_context_error_when_the_client_goes_away_while_rendering", func(t *testing.T) {
		renderer := newBlockingRenderer(false)
		h := httpdf.New(renderer, httpdf.WithRenderTimeout(5*time.Second))
		ctx, cancel := context.WithCancel(t.Context())

		done := generate(ctx, h, values)
		<-renderer.started
		cancel()
		err := <-done

		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, httpdf.ErrRenderTimeout)
	})

	t.Run("it_returns_the_context_error_when_the_client_goes_away_while_waiting", func(t *testing.T) {
		renderer := newBlockingRenderer(true)
		defer close(renderer.release)
		h := httpdf.New(renderer, httpdf.WithMaxConcurrentRenders(1), httpdf.WithRenderTimeout(5*time.Second))
		ctx, cancel := context.WithCancel(t.Context())

		generate(t.Context(), h, values)
		<-renderer.started
		done := generate(ctx, h, values)
		cancel()
		err := <-done

		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, httpdf.ErrBusy)
	})

	t.Run("it_rejects_invalid_values_without_waiting_for_a_render_slot", func(t *testing.T) {
		renderer := newBlockingRenderer(true)
		defer close(renderer.release)
		h := httpdf.New(renderer, httpdf.WithMaxConcurrentRenders(1), httpdf.WithRenderTimeout(5*time.Second))

		generate(t.Context(), h, values)
		<-renderer.started
		start := time.Now()
		err := <-generate(t.Context(), h, map[string]any{})

		assert.ErrorIs(t, err, httpdf.ErrInvalidValues)
		assert.Less(t, time.Since(start), time.Second)
	})
}

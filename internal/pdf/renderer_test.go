package pdf

import (
	"bytes"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rodRenderer tests need Chromium, e.g. in the development container:
//
//	docker compose run --rm --entrypoint go -e HTTPDF_TEST_CHROMIUM=/usr/bin/chromium-headless-shell httpdf test ./internal/pdf/
func TestRodRenderer(t *testing.T) {
	chromium := os.Getenv("HTTPDF_TEST_CHROMIUM")
	if chromium == "" {
		t.Skip("HTTPDF_TEST_CHROMIUM is not set to the path of a Chromium binary")
	}

	html := `<!doctype html><html><head><title>Größe (1)</title></head><body><p>Hello</p></body></html>`
	pageURL := "data:text/html;charset=utf-8," + url.PathEscape(html)

	render := func(t *testing.T, opts ...RodRendererOption) (pdf []byte, logs string) {
		t.Helper()
		var out, log bytes.Buffer
		opts = append(opts, WithLogger(slog.New(slog.NewTextHandler(&log, nil))))
		r := NewRodRenderer(chromium, opts...)
		err := r.Render(t.Context(), pageURL, &out, RenderOpts{Width: 210, Height: 297})
		require.NoError(t, err)
		return out.Bytes(), log.String()
	}

	t.Run("it_prefixes_the_creator", func(t *testing.T) {
		pdf, logs := render(t, WithCreatorPrefix(testPrefix))
		assert.NotContains(t, logs, "creator")

		_, info := resolveInfo(t, pdf)
		creator, ok := lookup(info, "/Creator")
		require.True(t, ok)
		assert.Equal(t, "httpdf/1.2.3 Chromium", text(t, creator))
		title, ok := lookup(info, "/Title")
		require.True(t, ok)
		assert.Equal(t, "Größe (1)", text(t, title))
	})

	t.Run("it_keeps_the_creator_without_prefix", func(t *testing.T) {
		pdf, _ := render(t)

		trailer, info := resolveInfo(t, pdf)
		_, ok := lookup(trailer, "/Prev")
		assert.False(t, ok)
		creator, ok := lookup(info, "/Creator")
		require.True(t, ok)
		assert.Equal(t, "Chromium", text(t, creator))
	})
}

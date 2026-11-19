package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintToPDFParams(t *testing.T) {
	t.Run("it_passes_the_tagged_pdf_and_outline_options_when_false", func(t *testing.T) {
		params, err := json.Marshal(printToPDFParams(proto.PagePrintToPDF{PrintBackground: true}))
		require.NoError(t, err)

		assert.Contains(t, string(params), `"printBackground":true`)
		assert.Contains(t, string(params), `"generateTaggedPDF":false`)
		assert.Contains(t, string(params), `"generateDocumentOutline":false`)
	})

	t.Run("it_passes_the_tagged_pdf_and_outline_options_once_when_true", func(t *testing.T) {
		params, err := json.Marshal(printToPDFParams(proto.PagePrintToPDF{
			GenerateTaggedPDF:       true,
			GenerateDocumentOutline: true,
		}))
		require.NoError(t, err)

		assert.Equal(t, 1, strings.Count(string(params), `"generateTaggedPDF":true`))
		assert.Equal(t, 1, strings.Count(string(params), `"generateDocumentOutline":true`))
		assert.Equal(t, 1, strings.Count(string(params), `generateTaggedPDF`))
	})
}

// The rodRenderer tests need Chromium, e.g. in the development container:
//
//	docker compose run --rm --entrypoint go -e HTTPDF_TEST_CHROMIUM=/usr/bin/chromium-headless-shell httpdf test ./internal/pdf/
func TestRodRenderer(t *testing.T) {
	chromium := os.Getenv("HTTPDF_TEST_CHROMIUM")
	if chromium == "" {
		t.Skip("HTTPDF_TEST_CHROMIUM is not set to the path of a Chromium binary")
	}

	html := `<!doctype html><html><head><title>Größe (1)</title></head><body><h1>Hello</h1><p>World</p></body></html>`
	pageURL := "data:text/html;charset=utf-8," + url.PathEscape(html)
	a4 := RenderOpts{Width: 210, Height: 297}

	render := func(t *testing.T, renderOpts RenderOpts, opts ...RodRendererOption) (pdf []byte, logs string) {
		t.Helper()
		var out, log bytes.Buffer
		opts = append(opts, WithLogger(slog.New(slog.NewTextHandler(&log, nil))))
		r := NewRodRenderer(chromium, opts...)
		err := r.Render(t.Context(), pageURL, &out, renderOpts)
		require.NoError(t, err)
		return out.Bytes(), log.String()
	}

	// tagged and outlined report whether a PDF is tagged and has an outline
	tagged := func(pdf []byte) bool { return bytes.Contains(pdf, []byte("/StructTreeRoot")) }
	outlined := func(pdf []byte) bool { return bytes.Contains(pdf, []byte("/Outlines")) }

	t.Run("it_generates_an_untagged_pdf_without_outline_by_default", func(t *testing.T) {
		pdf, _ := render(t, a4)

		assert.False(t, tagged(pdf), "PDF shouldn't be tagged")
		assert.False(t, outlined(pdf), "PDF shouldn't have an outline")
	})

	t.Run("it_generates_a_tagged_pdf", func(t *testing.T) {
		pdf, _ := render(t, RenderOpts{Width: 210, Height: 297, GenerateTaggedPDF: true})

		assert.True(t, tagged(pdf), "PDF should be tagged")
		assert.False(t, outlined(pdf), "PDF shouldn't have an outline")
	})

	t.Run("it_generates_a_tagged_pdf_with_outline_if_only_the_outline_is_requested", func(t *testing.T) {
		pdf, _ := render(t, RenderOpts{Width: 210, Height: 297, GenerateDocumentOutline: true})

		assert.True(t, tagged(pdf), "PDF should be tagged")
		assert.True(t, outlined(pdf), "PDF should have an outline")
	})

	t.Run("it_prefixes_the_creator", func(t *testing.T) {
		pdf, logs := render(t, a4, WithCreatorPrefix(testPrefix))
		assert.NotContains(t, logs, "creator")

		_, info := resolveInfo(t, pdf)
		creator, ok := lookup(info, "/Creator")
		require.True(t, ok)
		assert.Equal(t, "httpdf/1.2.3 Chromium", text(t, creator))
		title, ok := lookup(info, "/Title")
		require.True(t, ok)
		assert.Equal(t, "Größe (1)", text(t, title))
	})

	// profiles lists the browser profiles left in the temp directory
	profiles := func(t *testing.T) []string {
		t.Helper()
		entries, err := os.ReadDir(launcher.DefaultUserDataDirPrefix)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		require.NoError(t, err)
		names := make([]string, len(entries))
		for i, entry := range entries {
			names[i] = entry.Name()
		}
		return names
	}

	t.Run("it_removes_the_browser_profile", func(t *testing.T) {
		before := profiles(t)

		render(t, a4)

		assert.Equal(t, before, profiles(t))
	})

	t.Run("it_removes_the_browser_profile_when_the_render_is_canceled", func(t *testing.T) {
		// The page never settles, so the render only ends with the context
		html := `<p id="t"></p><script>setInterval(() => { document.getElementById("t").textContent = Date.now() }, 10)</script>`
		endless := "data:text/html;charset=utf-8," + url.PathEscape(html)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		before := profiles(t)

		start := time.Now()
		err := NewRodRenderer(chromium).Render(ctx, endless, io.Discard, a4)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 5*time.Second, "the browser should be stopped right away")
		assert.Equal(t, before, profiles(t))
	})

	t.Run("it_keeps_the_creator_without_prefix", func(t *testing.T) {
		pdf, _ := render(t, a4)

		trailer, info := resolveInfo(t, pdf)
		_, ok := lookup(trailer, "/Prev")
		assert.False(t, ok)
		creator, ok := lookup(info, "/Creator")
		require.True(t, ok)
		assert.Equal(t, "Chromium", text(t, creator))
	})
}

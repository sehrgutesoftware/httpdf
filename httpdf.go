package httpdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

var (
	// ErrInvalidValues is returned when the values are invalid
	ErrInvalidValues = errors.New("invalid values")
)

// HTTPDF is the interface for the httpdf service.
type HTTPDF interface {
	// Generate renders a PDF from the given template and values.
	Generate(ctx context.Context, t *template.Template, locale string, v map[string]any, w io.Writer) error
}

// httpdf is the core implementation of the httpdf service.
type httpdf struct {
	pdfRenderer pdf.Renderer
	logger      *slog.Logger
}

// New creates a new httpdf service.
func New(
	pdfRenderer pdf.Renderer,
	opts ...Option,
) HTTPDF {
	o := newOptions(opts)
	return &httpdf{
		pdfRenderer: pdfRenderer,
		logger:      o.logger,
	}
}

// Generate a PDF from the given template and values.
func (h *httpdf) Generate(ctx context.Context, t *template.Template, locale string, v map[string]any, w io.Writer) error {
	if valid := t.Schema.Validate(v); !valid.Valid {
		return fmt.Errorf("%w: %v", ErrInvalidValues, valid.Errors)
	}

	// The template is rendered before the browser is involved, because the
	// browser would turn an error response into a PDF of the error message.
	var html bytes.Buffer
	if err := t.Render(v, "/assets", locale, &html); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serverAddr, err := h.temporaryServer(srvCtx, h.serve(ctx, t.Assets, html.Bytes()))
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	h.logger.DebugContext(ctx, "temporary server started", "address", serverAddr)

	if err := h.pdfRenderer.Render(ctx, serverAddr, w, pdf.RenderOpts{
		Width:                   t.Config.Page.Width,
		Height:                  t.Config.Page.Height,
		GenerateTaggedPDF:       t.Config.PDF.GenerateTaggedPDF,
		GenerateDocumentOutline: t.Config.PDF.GenerateDocumentOutline,
	}); err != nil {
		return fmt.Errorf("render PDF: %w", err)
	}

	return nil
}

func (h *httpdf) temporaryServer(ctx context.Context, handler http.Handler) (string, error) {
	// Assign a random free TCP port on the loopback interface, so the page is
	// only reachable from within the host/container.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("temporary server: %w", err)
	}

	server := &http.Server{
		Handler: handler,
	}

	// The temporary server can be stopped by closing the context
	go func() {
		<-ctx.Done()
		if err := server.Shutdown(context.Background()); err != nil {
			h.logger.Warn("temporary server shutdown failed", "error", err)
		}
	}()

	go func() {
		defer listener.Close()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	return fmt.Sprintf("http://%s", listener.Addr().String()), nil
}

func (h *httpdf) serve(ctx context.Context, assets fs.FS, html []byte) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", h.serveAssets(ctx, assets))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(html)
	})

	return mux
}

// serveAssets serves the template's assets to the browser and logs each
// request. Failed requests are logged as warnings, because the PDF will be
// generated without the asset.
func (h *httpdf) serveAssets(ctx context.Context, assets fs.FS) http.Handler {
	files := http.NotFoundHandler()
	if assets != nil {
		files = http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		files.ServeHTTP(rec, r)

		level := slog.LevelDebug
		if rec.statusCode() >= 400 {
			level = slog.LevelWarn
		}

		// The render's context carries the request and trace IDs
		h.logger.LogAttrs(ctx, level, "asset request",
			slog.String("component", "assets"),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.statusCode()),
			slog.Float64("duration_ms", millisSince(start)),
			slog.Int64("bytes", rec.bytes),
		)
	})
}

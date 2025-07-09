package httpdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

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
}

// New creates a new httpdf service.
func New(
	pdfRenderer pdf.Renderer,
) HTTPDF {
	return &httpdf{
		pdfRenderer: pdfRenderer,
	}
}

// Generate a PDF from the given template and values.
func (h *httpdf) Generate(ctx context.Context, t *template.Template, locale string, v map[string]any, w io.Writer) error {
	if valid := t.Schema.Validate(v); !valid.Valid {
		return fmt.Errorf("%w: %v", ErrInvalidValues, valid.Errors)
	}

	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serverAddr, err := h.temporaryServer(srvCtx, h.serve(t, locale, v))
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	log.Printf("Starting temporary server at %s", serverAddr)

	if err := h.pdfRenderer.Render(ctx, serverAddr, w, pdf.RenderOpts{
		Width:  t.Config.Page.Width,
		Height: t.Config.Page.Height,
	}); err != nil {
		return fmt.Errorf("render PDF: %w", err)
	}

	return nil
}

func (h *httpdf) temporaryServer(ctx context.Context, handler http.Handler) (string, error) {
	// Assign a random free local TCP port for the temporary server
	listener, err := net.Listen("tcp", ":0")
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
			fmt.Printf("shut down temporary server: %v\n", err)
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

func (h *httpdf) serve(t *template.Template, locale string, v map[string]any) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(t.Assets))))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		err := t.Render(v, "/assets", locale, w)
		if err != nil {
			http.Error(w, "render template", http.StatusInternalServerError)
			return
		}
	})

	return mux
}

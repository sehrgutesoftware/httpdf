package httpdf

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/handlers"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

type server struct {
	*http.ServeMux
	httpdf  HTTPDF
	loader  template.Loader
	cache   map[string]*template.Template
	cacheMu sync.Mutex
}

func NewServer(httpdf HTTPDF, loader template.Loader, opts ...Option) http.Handler {
	o := newOptions(opts)
	server := &server{
		ServeMux: http.NewServeMux(),
		httpdf:   httpdf,
		loader:   loader,
		cache:    make(map[string]*template.Template),
	}

	server.Handle("GET /healthz", http.HandlerFunc(server.healthz))
	server.Handle("POST /templates/{template}/render", http.HandlerFunc(server.render))
	server.Handle("GET /templates/{template}/preview", http.HandlerFunc(server.preview))
	server.Handle("GET /templates/{template}/assets/", http.HandlerFunc(server.assets))

	cors := handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{http.MethodOptions, http.MethodGet, http.MethodPost}),
		handlers.AllowedHeaders([]string{"Content-Type", requestIDHeader, "traceparent"}),
		handlers.ExposedHeaders([]string{requestIDHeader}),
	)

	return logRequests(o.logger, cors(server))
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *server) render(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	addLogAttrs(r.Context(),
		slog.String("template", r.PathValue("template")),
		slog.String("locale", locale),
	)

	var values map[string]any
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		httpError(w, r, "invalid request body", http.StatusBadRequest, err)
		return
	}

	// For the render operation, the cached template is used
	t, err := s.cachedTemplate(r.PathValue("template"))
	if errors.Is(err, template.ErrTemplateNotFound) {
		httpError(w, r, "template not found", http.StatusNotFound, err)
		return
	} else if err != nil {
		httpError(w, r, err.Error(), http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	err = s.httpdf.Generate(r.Context(), t, locale, values, w)
	if errors.Is(err, ErrInvalidValues) {
		httpError(w, r, err.Error(), http.StatusUnprocessableEntity, err)
		return
	} else if err != nil {
		httpError(w, r, err.Error(), http.StatusInternalServerError, err)
		return
	}
}

func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	addLogAttrs(r.Context(),
		slog.String("template", r.PathValue("template")),
		slog.String("locale", locale),
	)

	assets := fmt.Sprintf("/templates/%s/assets", r.PathValue("template"))

	// For the preview operation, the template is loaded each time in order to
	// reflect any changes made to the template files.
	t, err := s.loader.Load(r.PathValue("template"))
	if errors.Is(err, template.ErrTemplateNotFound) {
		httpError(w, r, "template not found", http.StatusNotFound, err)
		return
	} else if err != nil {
		httpError(w, r, err.Error(), http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	err = t.Render(t.Example, assets, locale, w)
	if err != nil {
		httpError(w, r, "failed to render template: "+err.Error(), http.StatusInternalServerError, err)
		return
	}
}

func (s *server) assets(w http.ResponseWriter, r *http.Request) {
	addLogAttrs(r.Context(), slog.String("template", r.PathValue("template")))

	// Assets are served from the cached template because they are read from the
	// filesystem on each request.
	t, err := s.cachedTemplate(r.PathValue("template"))
	if errors.Is(err, template.ErrTemplateNotFound) {
		httpError(w, r, "template not found", http.StatusNotFound, err)
		return
	} else if err != nil {
		httpError(w, r, err.Error(), http.StatusInternalServerError, err)
		return
	}
	if t.Assets == nil {
		httpError(w, r, "no assets available for this template", http.StatusNotFound, nil)
		return
	}

	prefix := fmt.Sprintf("/templates/%s/assets/", r.PathValue("template"))
	http.StripPrefix(prefix, http.FileServer(http.FS(t.Assets))).ServeHTTP(w, r)
}

func (s *server) cachedTemplate(name string) (*template.Template, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	if t, ok := s.cache[name]; ok {
		return t, nil
	}

	t, err := s.loader.Load(name)
	if err != nil {
		return nil, err
	}

	s.cache[name] = t
	return t, nil
}

// httpError sends an error response and records the error for the request log
func httpError(w http.ResponseWriter, r *http.Request, msg string, status int, err error) {
	if err != nil {
		addLogAttrs(r.Context(), slog.Any("error", err))
	}
	http.Error(w, msg, status)
}

func extractLocale(r *http.Request) string {
	if locale := r.URL.Query().Get("lang"); locale != "" {
		return locale
	}
	return r.Header.Get("Accept-Language")
}

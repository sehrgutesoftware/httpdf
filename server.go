package httpdf

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/handlers"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

type server struct {
	*http.ServeMux
	httpdf HTTPDF
	loader template.Loader
	cache  map[string]*template.Template
}

func NewServer(httpdf HTTPDF, loader template.Loader) http.Handler {
	server := &server{
		ServeMux: http.NewServeMux(),
		httpdf:   httpdf,
		loader:   loader,
		cache:    make(map[string]*template.Template),
	}

	server.Handle("POST /templates/{template}/render", http.HandlerFunc(server.render))
	server.Handle("GET /templates/{template}/preview", http.HandlerFunc(server.preview))
	server.Handle("GET /templates/{template}/assets/", http.HandlerFunc(server.assets))

	cors := handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{http.MethodOptions, http.MethodGet, http.MethodPost}),
		handlers.AllowedHeaders([]string{"Content-Type"}),
	)

	return cors(server)
}

func (s *server) render(w http.ResponseWriter, r *http.Request) {
	var values map[string]any
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// For the render operation, the cached template is used
	t, err := s.cachedTemplate(r.PathValue("template"))
	if errors.Is(err, template.ErrTemplateNotFound) {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	if err := s.httpdf.Generate(r.Context(), t, extractLocale(r), values, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	assets := fmt.Sprintf("/templates/%s/assets", r.PathValue("template"))

	// For the preview operation, the template is loaded each time in order to
	// reflect any changes made to the template files.
	t, err := s.loader.Load(r.PathValue("template"))
	if errors.Is(err, template.ErrTemplateNotFound) {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	err = t.Render(t.Example, assets, extractLocale(r), w)
	if err != nil {
		http.Error(w, "failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) assets(w http.ResponseWriter, r *http.Request) {
	// Assets are served from the cached template because they are read from the
	// filesystem on each request.
	t, err := s.cachedTemplate(r.PathValue("template"))
	if errors.Is(err, template.ErrTemplateNotFound) {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if t.Assets == nil {
		http.Error(w, "no assets available for this template", http.StatusNotFound)
		return
	}

	prefix := fmt.Sprintf("/templates/%s/assets/", r.PathValue("template"))
	http.StripPrefix(prefix, http.FileServer(http.FS(t.Assets))).ServeHTTP(w, r)
}

func (s *server) cachedTemplate(name string) (*template.Template, error) {
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

func extractLocale(r *http.Request) string {
	if locale := r.URL.Query().Get("lang"); locale != "" {
		return locale
	}
	return r.Header.Get("Accept-Language")
}

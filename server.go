package httpdf

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sehrgutesoftware/httpdf/internal/template"
)

type server struct {
	*http.ServeMux
	httpdf HTTPDF
}

func NewServer(httpdf HTTPDF) http.Handler {
	server := &server{
		ServeMux: http.NewServeMux(),
		httpdf:   httpdf,
	}

	server.Handle("POST /templates/{template}/render", http.HandlerFunc(server.render))
	server.Handle("GET /templates/{template}/preview", http.HandlerFunc(server.preview))

	return server
}

func (s *server) render(w http.ResponseWriter, r *http.Request) {
	var values map[string]any
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	if err := s.httpdf.Generate(r.Context(), r.PathValue("template"), values, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	err := s.httpdf.Preview(r.PathValue("template"), w)
	if errors.Is(err, template.ErrNoExampleData) {
		http.Error(w, "template has no example.json", http.StatusNotFound)
	} else if errors.Is(err, ErrInvalidValues) {
		http.Error(w, "invalid example data", http.StatusInternalServerError)
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

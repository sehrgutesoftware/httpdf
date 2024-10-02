package httpdf

import (
	"encoding/json"
	"net/http"
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

	server.Handle("POST /templates/{template}/render", http.HandlerFunc(server.renderTemplate))

	return server
}

func (s *server) renderTemplate(w http.ResponseWriter, r *http.Request) {
	var values map[string]any
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	if err := s.httpdf.Generate(r.PathValue("template"), values, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

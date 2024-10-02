package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

func main() {
	listenOn := ":8080"
	fmt.Printf("Starting httpdf server on http://localhost%s\n", listenOn)

	loader := template.NewFSLoader(os.DirFS("templates"))
	validator := template.NewValidator()
	htmlRenderer := template.NewRenderer()
	pdfRenderer := pdf.NewRodRenderer("/usr/bin/chromium")
	app := httpdf.New(loader, validator, htmlRenderer, pdfRenderer)
	server := httpdf.NewServer(app)

	err := http.ListenAndServe(listenOn, server)
	if err != nil {
		panic(err)
	}
}

package main

import (
	"fmt"
	"net/http"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

func main() {
	listenOn := ":8080"
	fmt.Printf("Starting httpdf server on http://localhost%s\n", listenOn)

	loader := template.NewFSLoader(subdirfs.New("templates"))
	pdfRenderer := pdf.NewRodRenderer("/usr/bin/chromium")
	app := httpdf.New(pdfRenderer)
	server := httpdf.NewServer(app, loader)

	err := http.ListenAndServe(listenOn, server)
	if err != nil {
		panic(err)
	}
}

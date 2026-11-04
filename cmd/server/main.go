package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

func main() {
	listenOn := ":8080"
	fmt.Printf("Starting httpdf server on http://localhost%s\n", listenOn)

	var rendererOpts []pdf.RodRendererOption
	if v := os.Getenv("HTTPDF_BROWSER_LOG"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			panic(fmt.Errorf("invalid value for HTTPDF_BROWSER_LOG: %w", err))
		}
		if enabled {
			rendererOpts = append(rendererOpts, pdf.WithBrowserLog(os.Stderr))
		}
	}

	loader := template.NewFSLoader(subdirfs.New("templates"))
	pdfRenderer := pdf.NewRodRenderer("/usr/bin/chromium", rendererOpts...)
	app := httpdf.New(pdfRenderer)
	server := httpdf.NewServer(app, loader)

	err := http.ListenAndServe(listenOn, server)
	if err != nil {
		panic(err)
	}
}

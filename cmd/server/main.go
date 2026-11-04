package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

const (
	listenOn     = ":8080"
	chromiumPath = "/usr/bin/chromium"
	templatesDir = "templates"
)

func main() {
	logFormat, errLogFormat := envLogFormat("HTTPDF_LOG_FORMAT")
	logLevel, errLogLevel := envLogLevel("HTTPDF_LOG_LEVEL")
	browserLog, errBrowserLog := envBool("HTTPDF_BROWSER_LOG")

	logger := newLogger(os.Stdout, logFormat, logLevel)
	slog.SetDefault(logger)

	if err := errors.Join(errLogFormat, errLogLevel, errBrowserLog); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	var rendererOpts []pdf.RodRendererOption
	if browserLog {
		rendererOpts = append(rendererOpts, pdf.WithBrowserLog(os.Stderr))
	}

	loader := template.NewFSLoader(subdirfs.New(templatesDir))
	pdfRenderer := pdf.NewRodRenderer(chromiumPath, rendererOpts...)
	app := httpdf.New(pdfRenderer, httpdf.WithLogger(logger))

	server := &http.Server{
		Addr:     listenOn,
		Handler:  httpdf.NewServer(app, loader, httpdf.WithLogger(logger)),
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	logger.Info("server starting",
		"address", listenOn,
		"log_format", logFormat,
		"log_level", logLevel,
		"chromium", chromiumPath,
		"templates", templatesDir,
		"browser_log", browserLog,
	)

	err := server.ListenAndServe()
	logger.Error("server failed", "error", err)
	os.Exit(1)
}

// newLogger creates a logger writing in the given format ("json" or "text")
func newLogger(w io.Writer, format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// envLogFormat reads the log format from the named env var. Defaults to json.
func envLogFormat(name string) (string, error) {
	v := os.Getenv(name)
	switch f := strings.ToLower(v); f {
	case "":
		return "json", nil
	case "json", "text":
		return f, nil
	default:
		return "json", fmt.Errorf("%s: must be json or text, got %q", name, v)
	}
}

// envLogLevel reads the log level from the named env var. Defaults to info.
func envLogLevel(name string) (slog.Level, error) {
	v := os.Getenv(name)
	if v == "" {
		return slog.LevelInfo, nil
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(v)); err != nil {
		return slog.LevelInfo, fmt.Errorf("%s: must be debug, info, warn or error, got %q", name, v)
	}
	return level, nil
}

// envBool reads a boolean from the named env var. Defaults to false.
func envBool(name string) (bool, error) {
	v := os.Getenv(name)
	if v == "" {
		return false, nil
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: must be true or false, got %q", name, v)
	}
	return b, nil
}

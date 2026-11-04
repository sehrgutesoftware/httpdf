package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/logging"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

const (
	listenOn     = ":8080"
	chromiumPath = "/usr/bin/chromium-headless-shell"
	templatesDir = "templates"

	// shutdownTimeout is how long in-flight requests may take to complete after
	// a shutdown signal.
	shutdownTimeout = 25 * time.Second
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3"
var version = "dev"

func main() {
	logFormat, errLogFormat := envLogFormat("HTTPDF_LOG_FORMAT")
	logLevel, errLogLevel := envLogLevel("HTTPDF_LOG_LEVEL")

	logger := newLogger(os.Stdout, logFormat, logLevel)
	slog.SetDefault(logger)

	if err := errors.Join(errLogFormat, errLogLevel); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	loader := template.NewFSLoader(subdirfs.New(templatesDir))
	pdfRenderer := pdf.NewRodRenderer(chromiumPath,
		pdf.WithLogger(logger),
		pdf.WithCreatorPrefix("httpdf/"+version),
	)
	app := httpdf.New(pdfRenderer, httpdf.WithLogger(logger))

	server := &http.Server{
		Handler:  httpdf.NewServer(app, loader, httpdf.WithLogger(logger)),
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	listener, err := net.Listen("tcp", listenOn)
	if err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server starting",
		"version", version,
		"address", listenOn,
		"log_format", logFormat,
		"log_level", logging.LevelName(logLevel),
		"chromium", chromiumPath,
		"templates", templatesDir,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	// Restore the default signal handling after the first signal, so that a
	// second one terminates the process immediately.
	context.AfterFunc(ctx, stop)

	if err := serve(ctx, server, listener, logger, shutdownTimeout); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}

// serve runs the server until ctx is canceled, then shuts it down gracefully.
//
// On ctx cancel, it stops accepting connections and waits up to timeout for
// in-flight requests to complete. Incomplete requests after that are aborted.
func serve(ctx context.Context, server *http.Server, listener net.Listener, logger *slog.Logger, timeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("server shutting down",
		"reason", context.Cause(ctx).Error(),
		"timeout_ms", logging.Millis(timeout),
	)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("in-flight requests did not complete within %s and were aborted", timeout)
		}
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// newLogger creates a logger writing in the given format ("json" or "text")
func newLogger(w io.Writer, format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: logging.ReplaceLevelName}
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

	level, err := logging.ParseLevel(v)
	if err != nil {
		return slog.LevelInfo, fmt.Errorf("%s: must be trace, debug, info, warn or error, got %q", name, v)
	}
	return level, nil
}

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
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/logging"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/urfave/cli/v3"
)

const (
	// shutdownTimeout is how long in-flight requests may take to complete after
	// a shutdown signal.
	shutdownTimeout = 25 * time.Second

	// The HTTP server's timeouts protect against clients that send or read
	// slowly. Reading a request's header and body counts towards the write
	// timeout, which is added on top of the render timeout.
	readHeaderTimeout  = 10 * time.Second
	readTimeout        = 30 * time.Second
	writeTimeoutMargin = 30 * time.Second
	idleTimeout        = 120 * time.Second
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3"
var version = "dev"

// errReported is returned by actions that reported their failure already
var errReported = errors.New("failure reported")

func main() {
	err := newCommand(os.Stdout, runServer).Run(context.Background(), os.Args)
	if err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

// newCommand creates the command line interface. Without a subcommand, it
// runs the server with run.
func newCommand(out io.Writer, run func(ctx context.Context, cfg Config, configFile string) error) *cli.Command {
	defaults := DefaultConfig()

	// Usage errors are returned to main, which prints them, instead of being
	// printed along with the help
	onUsageError := func(ctx context.Context, cmd *cli.Command, err error, isSubcommand bool) error {
		return fmt.Errorf("%w, see --help", err)
	}

	return &cli.Command{
		Name:    "httpdf-server",
		Usage:   "Render PDFs from HTML templates via HTTP",
		Version: version,
		Description: "Settings are taken from the defaults, the config file, the " + envPrefix + "* environment\n" +
			"variables and the flags below, each overriding the previous. See the README\n" +
			"for all settings.",
		Writer:       out,
		OnUsageError: onUsageError,
		// The flags' environment variables are read along with the others by
		// LoadEnv, so they're only mentioned in the usage, except for the
		// config file, which has to be known before
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "path of a config file",
				Sources: cli.EnvVars(envPrefix + "CONFIG"),
			},
			&cli.StringFlag{
				Name:    "listen",
				Aliases: []string{"l"},
				Usage:   "address to listen on [$" + envPrefix + "LISTEN]",
				Value:   defaults.Listen,
			},
			&cli.StringFlag{
				Name:    "templates",
				Aliases: []string{"t"},
				Usage:   "directory containing the templates [$" + envPrefix + "TEMPLATES]",
				Value:   defaults.Templates,
			},
			&cli.StringFlag{
				Name:  "chromium",
				Usage: "path of the Chromium binary [$" + envPrefix + "CHROMIUM]",
				Value: defaults.Chromium,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Present() {
				return fmt.Errorf("unexpected argument %q, see --help", cmd.Args().First())
			}
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			return run(ctx, cfg, cmd.String("config"))
		},
		Commands: []*cli.Command{
			{
				Name:         "validate",
				Usage:        "check all templates for errors, or only the named ones",
				ArgsUsage:    "[template ...]",
				OnUsageError: onUsageError,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := loadConfig(cmd)
					if err != nil {
						return err
					}
					if validate(out, cfg.Templates, cmd.Args().Slice()) != 0 {
						return errReported
					}
					return nil
				},
			},
		},
	}
}

// loadConfig layers the config: the defaults, the config file if one is given,
// the environment variables, and the flags set on the command line.
func loadConfig(cmd *cli.Command) (Config, error) {
	cfg := DefaultConfig()

	if path := cmd.String("config"); path != "" {
		if err := cfg.LoadFile(path); err != nil {
			return Config{}, fmt.Errorf("invalid configuration: %w", err)
		}
	}
	if err := cfg.LoadEnv(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}

	// Flags only override the other sources if they're set explicitly
	for name, value := range map[string]*string{
		"listen":    &cfg.Listen,
		"templates": &cfg.Templates,
		"chromium":  &cfg.Chromium,
	} {
		if cmd.IsSet(name) {
			*value = cmd.String(name)
		}
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// runServer runs the server with the given config until it receives SIGTERM
// or SIGINT.
func runServer(ctx context.Context, cfg Config, configFile string) error {
	logFormat := strings.ToLower(cfg.Log.Format)
	logLevel, _ := logging.ParseLevel(cfg.Log.Level) // validated by loadConfig
	logger := newLogger(os.Stdout, logFormat, logLevel)
	slog.SetDefault(logger)

	warnInvalidTemplates(logger, cfg.Templates)
	warnMissingChromium(logger, cfg.Chromium)

	opts := []httpdf.Option{
		httpdf.WithLogger(logger),
		httpdf.WithMaxRequestSize(int64(cfg.Limits.MaxRequestMB) << 20),
		httpdf.WithMaxConcurrentRenders(cfg.Limits.MaxConcurrentRenders),
		httpdf.WithRenderTimeout(cfg.Limits.RenderTimeout),
	}
	loader := template.NewFSLoader(subdirfs.New(cfg.Templates))
	pdfRenderer := pdf.NewRodRenderer(cfg.Chromium,
		pdf.WithLogger(logger),
		pdf.WithCreatorPrefix("httpdf/"+version),
	)
	app := httpdf.New(pdfRenderer, opts...)
	server := newHTTPServer(httpdf.NewServer(app, loader, opts...), logger, cfg.Limits.RenderTimeout)

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		logger.Error("server failed", "error", err)
		return errReported
	}

	attrs := []any{
		"version", version,
		// The listener's address shows the port in use, e.g. for port 0
		"address", listener.Addr().String(),
		"log_format", logFormat,
		"log_level", logging.LevelName(logLevel),
		"chromium", cfg.Chromium,
		"templates", cfg.Templates,
		"max_request_mb", cfg.Limits.MaxRequestMB,
		"max_concurrent_renders", cfg.Limits.MaxConcurrentRenders,
		"render_timeout", cfg.Limits.RenderTimeout.String(),
	}
	if configFile != "" {
		attrs = append(attrs, "config_file", configFile)
	}
	logger.Info("server starting", attrs...)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	// Restore the default signal handling after the first signal, so that a
	// second one terminates the process immediately.
	context.AfterFunc(ctx, stop)

	if err := serve(ctx, server, listener, logger, shutdownTimeout); err != nil {
		logger.Error("server failed", "error", err)
		return errReported
	}
	logger.Info("server stopped")
	return nil
}

// warnMissingChromium logs a warning if the Chromium binary can't be run. It
// looks it up like the renderer does: bare names in PATH, others as paths,
// which must be executable files.
func warnMissingChromium(logger *slog.Logger, chromium string) {
	if _, err := exec.LookPath(chromium); err != nil {
		logger.Warn("chromium not found, rendering PDFs will fail", "chromium", chromium, "error", err)
	}
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

// newHTTPServer creates the HTTP server. Its write timeout covers reading the
// request, waiting for a render slot, rendering and sending the PDF.
func newHTTPServer(handler http.Handler, logger *slog.Logger, renderTimeout time.Duration) *http.Server {
	return &http.Server{
		Handler:           handler,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      readTimeout + renderTimeout + writeTimeoutMargin,
		IdleTimeout:       idleTimeout,
	}
}

// newLogger creates a logger writing in the given format ("json" or "text")
func newLogger(w io.Writer, format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: logging.ReplaceLevelName}
	if format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

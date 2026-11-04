package httpdf

import (
	"log/slog"

	"github.com/sehrgutesoftware/httpdf/internal/logging"
)

// Option configures the httpdf service and server
type Option func(*options)

type options struct {
	logger *slog.Logger
}

func newOptions(opts []Option) options {
	o := options{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(&o)
	}

	// Logs emitted while handling a request carry its request and trace IDs
	o.logger = slog.New(logging.NewContextHandler(o.logger.Handler()))

	return o
}

// WithLogger sets the logger. Defaults to slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

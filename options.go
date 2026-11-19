package httpdf

import (
	"log/slog"
	"runtime"
	"time"

	"github.com/sehrgutesoftware/httpdf/internal/logging"
)

const (
	// DefaultMaxRequestSize is the default limit of render request bodies in bytes
	DefaultMaxRequestSize = 10 << 20

	// DefaultRenderTimeout is the default limit of how long a render may take,
	// including the time spent waiting for a free render slot
	DefaultRenderTimeout = 20 * time.Second
)

// DefaultMaxConcurrentRenders is the default limit of renders running at the
// same time. It's the number of CPUs available to the process, which takes
// container CPU limits into account.
func DefaultMaxConcurrentRenders() int {
	return runtime.GOMAXPROCS(0)
}

// Option configures the httpdf service and server
type Option func(*options)

type options struct {
	logger               *slog.Logger
	maxRequestSize       int64
	maxConcurrentRenders int
	renderTimeout        time.Duration
}

func newOptions(opts []Option) options {
	o := options{
		logger:               slog.Default(),
		maxRequestSize:       DefaultMaxRequestSize,
		maxConcurrentRenders: DefaultMaxConcurrentRenders(),
		renderTimeout:        DefaultRenderTimeout,
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

// WithMaxRequestSize limits the size of render request bodies in bytes. Larger
// requests are rejected with 413. Defaults to DefaultMaxRequestSize.
func WithMaxRequestSize(bytes int64) Option {
	return func(o *options) {
		o.maxRequestSize = bytes
	}
}

// WithMaxConcurrentRenders limits how many renders run at the same time. Further
// renders wait for a free slot. Defaults to DefaultMaxConcurrentRenders().
func WithMaxConcurrentRenders(n int) Option {
	return func(o *options) {
		o.maxConcurrentRenders = n
	}
}

// WithRenderTimeout limits how long a render may take, including the time
// spent waiting for a free render slot. Defaults to DefaultRenderTimeout.
func WithRenderTimeout(d time.Duration) Option {
	return func(o *options) {
		o.renderTimeout = d
	}
}

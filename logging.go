package httpdf

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const requestIDHeader = "X-Request-ID"

// statusClientClosedRequest is logged when the client went away before the
// response was complete (nginx convention, not sent to the client).
const statusClientClosedRequest = 499

var (
	// Request IDs from clients are accepted if they consist of up to 128
	// printable ASCII characters (no spaces).
	requestIDPattern = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)

	// https://www.w3.org/TR/trace-context/#traceparent-header
	traceparentPattern = regexp.MustCompile(`^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-[0-9a-f]{2}(-.*)?$`)
)

// requestInfo holds per-request data for logging
type requestInfo struct {
	requestID string
	traceID   string

	mu    sync.Mutex
	attrs []slog.Attr
	quiet bool
}

type requestInfoKey struct{}

func requestInfoFrom(ctx context.Context) *requestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(*requestInfo)
	return info
}

// addLogAttrs adds attributes to the request's summary log line
func addLogAttrs(ctx context.Context, attrs ...slog.Attr) {
	if info := requestInfoFrom(ctx); info != nil {
		info.mu.Lock()
		info.attrs = append(info.attrs, attrs...)
		info.mu.Unlock()
	}
}

// quietOnSuccess logs the request's summary line at debug level, unless the
// request fails. Meant for frequent requests that are only interesting when
// something goes wrong.
func quietOnSuccess(ctx context.Context) {
	if info := requestInfoFrom(ctx); info != nil {
		info.mu.Lock()
		info.quiet = true
		info.mu.Unlock()
	}
}

// logRequests is a middleware that assigns a request ID to each request and
// logs a summary line once the request is complete.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		info := &requestInfo{
			requestID: requestID(r.Header.Get(requestIDHeader)),
			traceID:   traceID(r.Header.Values("traceparent")),
		}
		ctx := context.WithValue(r.Context(), requestInfoKey{}, info)
		r = r.WithContext(ctx)
		w.Header().Set(requestIDHeader, info.requestID)

		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := rec.statusCode()
		canceled := errors.Is(ctx.Err(), context.Canceled)
		if canceled {
			status = statusClientClosedRequest
		}

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Float64("duration_ms", millisSince(start)),
			slog.Int64("bytes", rec.bytes),
		}
		info.mu.Lock()
		attrs = append(attrs, info.attrs...)
		quiet := info.quiet
		info.mu.Unlock()

		logger.LogAttrs(ctx, requestLogLevel(status, canceled, quiet), "request", attrs...)
	})
}

// requestLogLevel determines the level of a request's summary line. Requests
// canceled by the client are not considered failures.
func requestLogLevel(status int, canceled bool, quiet bool) slog.Level {
	switch {
	case quiet && (canceled || status < 400):
		return slog.LevelDebug
	case canceled:
		return slog.LevelInfo
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// millisSince returns the time elapsed since start in milliseconds
func millisSince(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

// requestID returns the client-provided request ID if it is acceptable, or
// generates a new one.
func requestID(header string) string {
	if requestIDPattern.MatchString(header) {
		return header
	}
	return rand.Text()
}

// traceID returns the trace ID from a W3C traceparent header, or an empty
// string if the header is missing or invalid.
func traceID(headers []string) string {
	if len(headers) != 1 {
		return ""
	}

	m := traceparentPattern.FindStringSubmatch(strings.TrimSpace(headers[0]))
	if m == nil {
		return ""
	}
	version, trace, parent, suffix := m[1], m[2], m[3], m[4]
	if version == "ff" || (version == "00" && suffix != "") {
		return ""
	}
	if trace == strings.Repeat("0", 32) || parent == strings.Repeat("0", 16) {
		return ""
	}

	return trace
}

// statusRecorder records the status code and number of bytes written
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Unwrap allows http.ResponseController to access the underlying writer
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// contextHandler adds the request and trace IDs from the context to each log
// record, so that all logs emitted while handling a request can be correlated.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if info := requestInfoFrom(ctx); info != nil {
		r.AddAttrs(slog.String("request_id", info.requestID))
		if info.traceID != "" {
			r.AddAttrs(slog.String("trace_id", info.traceID))
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}

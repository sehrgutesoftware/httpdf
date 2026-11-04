package httpdf

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/sehrgutesoftware/httpdf/internal/logging"
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

// requestInfo holds per-request data for the summary log line
type requestInfo struct {
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
// logs a summary line once the request is complete. It also recovers from
// panics in the handler, so that they are logged with the request.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		reqID := requestID(r.Header.Get(requestIDHeader))
		info := &requestInfo{}
		ctx := context.WithValue(r.Context(), requestInfoKey{}, info)
		ctx = logging.ContextWithIDs(ctx, reqID, traceID(r.Header.Values("traceparent")))
		r = r.WithContext(ctx)
		w.Header().Set(requestIDHeader, reqID)

		rec := &statusRecorder{ResponseWriter: w}
		recovered, stack := serveRecovering(next, rec, r)
		wroteHeader := rec.status != 0
		if recovered != nil {
			addLogAttrs(ctx,
				slog.String("error", fmt.Sprintf("panic: %v", recovered)),
				slog.String("stack", string(stack)),
			)
			if !wroteHeader {
				http.Error(rec, "internal server error", http.StatusInternalServerError)
			}
		}

		status := rec.statusCode()
		canceled := errors.Is(ctx.Err(), context.Canceled)
		if canceled {
			status = statusClientClosedRequest
		}

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Float64("duration_ms", logging.MillisSince(start)),
			slog.Int64("bytes", rec.bytes),
		}
		info.mu.Lock()
		attrs = append(attrs, info.attrs...)
		quiet := info.quiet
		info.mu.Unlock()

		level := requestLogLevel(status, canceled, quiet)
		if recovered != nil {
			level = slog.LevelError
		}
		logger.LogAttrs(ctx, level, "request", attrs...)

		// If the response was already started, aborting the connection is the
		// only way to tell the client that it is incomplete.
		if recovered != nil && wroteHeader {
			panic(http.ErrAbortHandler)
		}
	})
}

// serveRecovering calls next and recovers from a panic. It returns the
// recovered value and the stack trace, or nil if next didn't panic.
func serveRecovering(next http.Handler, w http.ResponseWriter, r *http.Request) (recovered any, stack []byte) {
	defer func() {
		if recovered = recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			stack = debug.Stack()
		}
	}()

	next.ServeHTTP(w, r)
	return nil, nil
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

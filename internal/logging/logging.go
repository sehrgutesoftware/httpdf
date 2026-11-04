// Package logging contains logging helpers shared by the httpdf packages.
package logging

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// LevelTrace is the level for very verbose output, such as the browser's
const LevelTrace = slog.LevelDebug - 4

// ParseLevel parses a level name: trace, debug, info, warn or error
// (case-insensitive)
func ParseLevel(s string) (slog.Level, error) {
	if strings.EqualFold(s, "trace") {
		return LevelTrace, nil
	}

	var level slog.Level
	err := level.UnmarshalText([]byte(s))
	return level, err
}

// LevelName returns the name of the level, using "TRACE" for LevelTrace
func LevelName(level slog.Level) string {
	if level == LevelTrace {
		return "TRACE"
	}
	return level.String()
}

// ReplaceLevelName is meant for slog.HandlerOptions.ReplaceAttr, so that
// records at LevelTrace are written as "TRACE" instead of "DEBUG-4".
func ReplaceLevelName(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.LevelKey && len(groups) == 0 {
		if level, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(LevelName(level))
		}
	}
	return a
}

// Millis returns the duration in milliseconds
func Millis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// MillisSince returns the time elapsed since start in milliseconds
func MillisSince(start time.Time) float64 {
	return Millis(time.Since(start))
}

type idsKey struct{}

type ids struct {
	requestID string
	traceID   string
}

// ContextWithIDs returns a context whose log records carry the given request
// and trace IDs, if logged through a handler from NewContextHandler. An empty
// trace ID is omitted.
func ContextWithIDs(ctx context.Context, requestID, traceID string) context.Context {
	return context.WithValue(ctx, idsKey{}, ids{requestID: requestID, traceID: traceID})
}

// NewContextHandler wraps h, so that records logged with a context from
// ContextWithIDs carry its IDs. Wrapping an already wrapped handler has no
// effect.
func NewContextHandler(h slog.Handler) slog.Handler {
	if _, ok := h.(contextHandler); ok {
		return h
	}
	return contextHandler{h}
}

type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ids, ok := ctx.Value(idsKey{}).(ids); ok {
		r.AddAttrs(slog.String("request_id", ids.requestID))
		if ids.traceID != "" {
			r.AddAttrs(slog.String("trace_id", ids.traceID))
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

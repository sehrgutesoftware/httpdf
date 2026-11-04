package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
)

// LineWriter is an io.Writer that logs each line written to it as a separate
// record, using the line as the message. Blank lines are skipped. It is safe
// for concurrent use, e.g. as both stdout and stderr of a process.
type LineWriter struct {
	ctx    context.Context
	logger *slog.Logger
	level  slog.Level
	attrs  []slog.Attr

	mu  sync.Mutex
	buf []byte
}

// NewLineWriter creates a LineWriter that logs at the given level with the
// given attributes
func NewLineWriter(ctx context.Context, logger *slog.Logger, level slog.Level, attrs ...slog.Attr) *LineWriter {
	return &LineWriter{
		ctx:    ctx,
		logger: logger,
		level:  level,
		attrs:  attrs,
	}
}

func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	rest := w.buf
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		w.log(rest[:i])
		rest = rest[i+1:]
	}
	w.buf = append(w.buf[:0], rest...)

	return len(p), nil
}

// Close logs any remaining incomplete line
func (w *LineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.log(w.buf)
	w.buf = nil

	return nil
}

func (w *LineWriter) log(line []byte) {
	msg := strings.TrimRight(string(line), "\r")
	if strings.TrimSpace(msg) == "" {
		return
	}
	w.logger.LogAttrs(w.ctx, w.level, msg, w.attrs...)
}

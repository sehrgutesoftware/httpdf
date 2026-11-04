package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/sehrgutesoftware/httpdf/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]struct {
		value   string
		want    slog.Level
		wantErr bool
	}{
		"trace":             {value: "trace", want: logging.LevelTrace},
		"trace_uppercase":   {value: "TRACE", want: logging.LevelTrace},
		"debug":             {value: "debug", want: slog.LevelDebug},
		"info":              {value: "info", want: slog.LevelInfo},
		"warn":              {value: "warn", want: slog.LevelWarn},
		"error_uppercase":   {value: "ERROR", want: slog.LevelError},
		"rejects_unknown":   {value: "verbose", wantErr: true},
		"rejects_empty_str": {value: "", wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := logging.ParseLevel(tc.value)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLevelName(t *testing.T) {
	assert.Equal(t, "TRACE", logging.LevelName(logging.LevelTrace))
	assert.Equal(t, "DEBUG", logging.LevelName(slog.LevelDebug))
	assert.Equal(t, "WARN", logging.LevelName(slog.LevelWarn))
}

func TestReplaceLevelName(t *testing.T) {
	t.Run("it_writes_trace_records_as_TRACE", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level:       logging.LevelTrace,
			ReplaceAttr: logging.ReplaceLevelName,
		}))

		logger.Log(context.Background(), logging.LevelTrace, "hello")

		var record map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
		assert.Equal(t, "TRACE", record["level"])
	})

	t.Run("it_leaves_attributes_named_level_in_groups_alone", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: logging.ReplaceLevelName}))

		logger.WithGroup("g").Info("hello", "level", logging.LevelTrace)

		assert.Contains(t, buf.String(), "level=INFO")
		assert.Contains(t, buf.String(), "g.level=DEBUG-4")
	})
}

func TestContextHandler(t *testing.T) {
	newLogger := func(buf *bytes.Buffer) *slog.Logger {
		return slog.New(logging.NewContextHandler(slog.NewJSONHandler(buf, nil)))
	}

	t.Run("it_adds_the_ids_from_the_context", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := logging.ContextWithIDs(context.Background(), "req-1", "trace-1")

		newLogger(&buf).InfoContext(ctx, "hello")

		var record map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
		assert.Equal(t, "req-1", record["request_id"])
		assert.Equal(t, "trace-1", record["trace_id"])
	})

	t.Run("it_omits_an_empty_trace_id", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := logging.ContextWithIDs(context.Background(), "req-1", "")

		newLogger(&buf).InfoContext(ctx, "hello")

		assert.Contains(t, buf.String(), `"request_id":"req-1"`)
		assert.NotContains(t, buf.String(), "trace_id")
	})

	t.Run("it_adds_nothing_without_ids_in_the_context", func(t *testing.T) {
		var buf bytes.Buffer

		newLogger(&buf).InfoContext(context.Background(), "hello")

		assert.NotContains(t, buf.String(), "request_id")
	})

	t.Run("it_keeps_adding_ids_to_derived_loggers", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := logging.ContextWithIDs(context.Background(), "req-1", "")

		newLogger(&buf).With("component", "x").InfoContext(ctx, "hello")

		assert.Contains(t, buf.String(), `"request_id":"req-1"`)
	})

	t.Run("it_does_not_wrap_twice", func(t *testing.T) {
		var buf bytes.Buffer
		handler := logging.NewContextHandler(newLogger(&buf).With("component", "x").Handler())
		ctx := logging.ContextWithIDs(context.Background(), "req-1", "")

		slog.New(handler).InfoContext(ctx, "hello")

		assert.Equal(t, 1, strings.Count(buf.String(), "request_id"))
	})
}

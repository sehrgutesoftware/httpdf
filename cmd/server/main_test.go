package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sehrgutesoftware/httpdf/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarnMissingChromium(t *testing.T) {
	notExecutable := filepath.Join(t.TempDir(), "chromium")
	require.NoError(t, os.WriteFile(notExecutable, nil, 0o644))

	cases := map[string]struct {
		chromium string
		warning  string
	}{
		"bare_name_in_path":         {chromium: "sh"},
		"executable_path":           {chromium: "/bin/sh"},
		"bare_name_not_in_path":     {chromium: "no-such-chromium", warning: "not found in $PATH"},
		"missing_path":              {chromium: "/no/such/chromium", warning: "no such file or directory"},
		"directory":                 {chromium: t.TempDir(), warning: "is a directory"},
		"file_that_isnt_executable": {chromium: notExecutable, warning: "permission denied"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			warnMissingChromium(newLogger(&buf, "json", slog.LevelInfo), tc.chromium)

			if tc.warning == "" {
				assert.Empty(t, buf.String())
				return
			}
			var record map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
			assert.Equal(t, "WARN", record["level"])
			assert.Equal(t, "chromium not found, rendering PDFs will fail", record["msg"])
			assert.Equal(t, tc.chromium, record["chromium"])
			assert.Contains(t, record["error"], tc.warning)
		})
	}
}

func TestNewHTTPServer(t *testing.T) {
	t.Run("it_sets_timeouts_that_leave_room_for_the_render", func(t *testing.T) {
		server := newHTTPServer(http.NotFoundHandler(), slog.New(slog.DiscardHandler), 20*time.Second)

		assert.Equal(t, 10*time.Second, server.ReadHeaderTimeout)
		assert.Equal(t, 30*time.Second, server.ReadTimeout)
		assert.Equal(t, 80*time.Second, server.WriteTimeout)
		assert.Equal(t, 120*time.Second, server.IdleTimeout)
	})
}

func TestNewLogger(t *testing.T) {
	t.Run("it_writes_json", func(t *testing.T) {
		var buf bytes.Buffer
		newLogger(&buf, "json", slog.LevelInfo).Info("hello", "key", "value")

		var record map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
		assert.Equal(t, "hello", record["msg"])
		assert.Equal(t, "value", record["key"])
	})

	t.Run("it_writes_text", func(t *testing.T) {
		var buf bytes.Buffer
		newLogger(&buf, "text", slog.LevelInfo).Info("hello", "key", "value")

		assert.Contains(t, buf.String(), "msg=hello key=value")
	})

	t.Run("it_filters_below_the_level", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newLogger(&buf, "json", slog.LevelWarn)
		logger.Info("dropped")
		logger.Warn("kept")

		assert.NotContains(t, buf.String(), "dropped")
		assert.Contains(t, buf.String(), "kept")
	})

	t.Run("it_writes_trace_records_as_TRACE", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newLogger(&buf, "text", logging.LevelTrace)
		logger.Log(context.Background(), logging.LevelTrace, "hello")

		assert.Contains(t, buf.String(), "level=TRACE msg=hello")
	})

	t.Run("it_drops_trace_records_at_debug", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newLogger(&buf, "text", slog.LevelDebug)
		logger.Log(context.Background(), logging.LevelTrace, "hello")

		assert.Empty(t, buf.String())
	})
}

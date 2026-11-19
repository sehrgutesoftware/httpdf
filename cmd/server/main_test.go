package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/sehrgutesoftware/httpdf/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvLogFormat(t *testing.T) {
	cases := map[string]struct {
		value   string
		want    string
		wantErr bool
	}{
		"defaults_to_json":    {value: "", want: "json"},
		"accepts_json":        {value: "json", want: "json"},
		"accepts_text":        {value: "text", want: "text"},
		"is_case_insensitive": {value: "TEXT", want: "text"},
		"rejects_unknown_format_and_falls_back_to_json": {value: "xml", want: "json", wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_LOG_FORMAT", tc.value)

			got, err := envLogFormat("TEST_LOG_FORMAT")

			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				assert.ErrorContains(t, err, "TEST_LOG_FORMAT")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnvLogLevel(t *testing.T) {
	cases := map[string]struct {
		value   string
		want    slog.Level
		wantErr bool
	}{
		"defaults_to_info": {value: "", want: slog.LevelInfo},
		"accepts_trace":    {value: "trace", want: logging.LevelTrace},
		"accepts_debug":    {value: "debug", want: slog.LevelDebug},
		"accepts_warn":     {value: "warn", want: slog.LevelWarn},
		"accepts_error":    {value: "ERROR", want: slog.LevelError},
		"rejects_unknown_level_and_falls_back_to_info": {value: "verbose", want: slog.LevelInfo, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_LOG_LEVEL", tc.value)

			got, err := envLogLevel("TEST_LOG_LEVEL")

			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				assert.ErrorContains(t, err, "TEST_LOG_LEVEL")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnvPositiveInt(t *testing.T) {
	cases := map[string]struct {
		value   string
		want    int
		wantErr bool
	}{
		"defaults_to_the_default":                    {value: "", want: 7},
		"accepts_a_positive_integer":                 {value: "3", want: 3},
		"rejects_zero_and_falls_back_to_the_default": {value: "0", want: 7, wantErr: true},
		"rejects_negative_numbers":                   {value: "-1", want: 7, wantErr: true},
		"rejects_fractions":                          {value: "1.5", want: 7, wantErr: true},
		"rejects_non_numeric_values":                 {value: "many", want: 7, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_INT", tc.value)

			got, err := envPositiveInt("TEST_INT", 7)

			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				assert.ErrorContains(t, err, "TEST_INT: must be a positive integer")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnvPositiveDuration(t *testing.T) {
	cases := map[string]struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		"defaults_to_the_default":                    {value: "", want: 20 * time.Second},
		"accepts_a_duration":                         {value: "1m30s", want: 90 * time.Second},
		"rejects_zero_and_falls_back_to_the_default": {value: "0s", want: 20 * time.Second, wantErr: true},
		"rejects_negative_durations":                 {value: "-5s", want: 20 * time.Second, wantErr: true},
		"rejects_numbers_without_unit":               {value: "20", want: 20 * time.Second, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_DURATION", tc.value)

			got, err := envPositiveDuration("TEST_DURATION", 20*time.Second)

			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				assert.ErrorContains(t, err, "TEST_DURATION: must be a positive duration")
			} else {
				assert.NoError(t, err)
			}
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

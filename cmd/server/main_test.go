package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

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

func TestEnvBool(t *testing.T) {
	cases := map[string]struct {
		value   string
		want    bool
		wantErr bool
	}{
		"defaults_to_false": {value: "", want: false},
		"accepts_true":      {value: "true", want: true},
		"accepts_1":         {value: "1", want: true},
		"accepts_false":     {value: "false", want: false},
		"rejects_garbage":   {value: "yes please", want: false, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_BOOL", tc.value)

			got, err := envBool("TEST_BOOL")

			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				assert.ErrorContains(t, err, "TEST_BOOL")
			} else {
				assert.NoError(t, err)
			}
		})
	}
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
}

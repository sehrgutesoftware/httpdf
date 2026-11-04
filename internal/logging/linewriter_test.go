package logging_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/sehrgutesoftware/httpdf/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLineWriter(buf *bytes.Buffer) *logging.LineWriter {
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: logging.LevelTrace}))
	return logging.NewLineWriter(context.Background(), logger, logging.LevelTrace, slog.String("component", "test"))
}

// messages returns the messages of the JSON log records in buf
func messages(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	var msgs []string
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for scanner.Scan() {
		var record map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
		msgs = append(msgs, record["msg"].(string))
	}
	return msgs
}

func TestLineWriter(t *testing.T) {
	t.Run("it_logs_each_line_with_level_and_attributes", func(t *testing.T) {
		var buf bytes.Buffer
		w := newLineWriter(&buf)

		_, err := w.Write([]byte("first\nsecond\n"))
		require.NoError(t, err)

		assert.Equal(t, []string{"first", "second"}, messages(t, &buf))
		var record map[string]any
		require.NoError(t, json.Unmarshal(bytes.SplitN(buf.Bytes(), []byte("\n"), 2)[0], &record))
		assert.Equal(t, "DEBUG-4", record["level"])
		assert.Equal(t, "test", record["component"])
	})

	t.Run("it_joins_lines_split_across_writes", func(t *testing.T) {
		var buf bytes.Buffer
		w := newLineWriter(&buf)

		for _, part := range []string{"fir", "st\nsec", "ond", "\n"} {
			_, err := w.Write([]byte(part))
			require.NoError(t, err)
		}

		assert.Equal(t, []string{"first", "second"}, messages(t, &buf))
	})

	t.Run("it_skips_blank_lines_and_strips_carriage_returns", func(t *testing.T) {
		var buf bytes.Buffer
		w := newLineWriter(&buf)

		_, err := w.Write([]byte("\n  \nfirst\r\n\r\n"))
		require.NoError(t, err)

		assert.Equal(t, []string{"first"}, messages(t, &buf))
	})

	t.Run("it_logs_an_incomplete_last_line_on_close", func(t *testing.T) {
		var buf bytes.Buffer
		w := newLineWriter(&buf)

		_, err := w.Write([]byte("first\nunfinished"))
		require.NoError(t, err)
		assert.Equal(t, []string{"first"}, messages(t, &buf))

		require.NoError(t, w.Close())
		assert.Equal(t, []string{"first", "unfinished"}, messages(t, &buf))
	})

	t.Run("it_is_safe_for_concurrent_writes", func(t *testing.T) {
		var buf bytes.Buffer
		w := newLineWriter(&buf)

		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range 10 {
					_, _ = fmt.Fprintf(w, "line %d-%d\n", i, j)
				}
			}()
		}
		wg.Wait()

		assert.Len(t, messages(t, &buf), 100)
	})
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingHandler signals on entered when a request arrives, and responds
// once release is closed
type blockingHandler struct {
	entered chan struct{}
	release chan struct{}
}

func newBlockingHandler() *blockingHandler {
	return &blockingHandler{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (h *blockingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	<-h.release
	_, _ = w.Write([]byte("done"))
}

type serveResult struct {
	url      string
	logs     *bytes.Buffer
	shutdown context.CancelCauseFunc
	done     chan error
}

// startServe runs serve in the background on a random local port
func startServe(t *testing.T, handler http.Handler, timeout time.Duration) serveResult {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })

	res := serveResult{
		url:      "http://" + listener.Addr().String(),
		logs:     &bytes.Buffer{},
		shutdown: cancel,
		done:     make(chan error, 1),
	}
	logger := newLogger(res.logs, "json", slog.LevelInfo)
	go func() {
		res.done <- serve(ctx, &http.Server{Handler: handler}, listener, logger, timeout)
	}()

	return res
}

// get requests the URL in the background and sends the body or error
func get(url string) <-chan any {
	result := make(chan any, 1)
	go func() {
		res, err := http.Get(url)
		if err != nil {
			result <- err
			return
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			result <- err
			return
		}
		result <- string(body)
	}()
	return result
}

func waitFor[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		panic("unreachable")
	}
}

func TestServe(t *testing.T) {
	t.Run("it_returns_the_error_if_the_server_fails", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		require.NoError(t, listener.Close())

		done := make(chan error, 1)
		go func() {
			done <- serve(context.Background(), &http.Server{}, listener, slog.New(slog.DiscardHandler), time.Second)
		}()

		assert.ErrorIs(t, waitFor(t, done), net.ErrClosed)
	})

	t.Run("it_waits_for_in_flight_requests_on_shutdown", func(t *testing.T) {
		handler := newBlockingHandler()
		srv := startServe(t, handler, 5*time.Second)

		response := get(srv.url)
		waitFor(t, handler.entered)
		srv.shutdown(errors.New("test signal"))

		select {
		case <-srv.done:
			t.Fatal("serve returned before the in-flight request completed")
		case <-time.After(100 * time.Millisecond):
		}
		_, err := http.Get(srv.url)
		assert.Error(t, err, "new connections must be refused")

		close(handler.release)
		assert.Equal(t, "done", waitFor(t, response))
		require.NoError(t, waitFor(t, srv.done))

		var record map[string]any
		require.NoError(t, json.Unmarshal(srv.logs.Bytes(), &record))
		assert.Equal(t, "server shutting down", record["msg"])
		assert.Equal(t, "test signal", record["reason"])
		assert.Equal(t, float64(5000), record["timeout_ms"])
	})

	t.Run("it_aborts_requests_that_exceed_the_timeout", func(t *testing.T) {
		handler := newBlockingHandler()
		t.Cleanup(func() { close(handler.release) })
		srv := startServe(t, handler, 50*time.Millisecond)

		response := get(srv.url)
		waitFor(t, handler.entered)
		srv.shutdown(errors.New("test signal"))

		err := waitFor(t, srv.done)
		assert.ErrorContains(t, err, "did not complete within 50ms")
		assert.Implements(t, (*error)(nil), waitFor(t, response), "the client must see an error")
	})
}

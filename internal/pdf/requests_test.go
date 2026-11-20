package pdf

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestTracker(t *testing.T) {
	const pageURL = "http://127.0.0.1:45685"

	newTracker := func(t *testing.T) (*requestTracker, *bytes.Buffer) {
		t.Helper()
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		return newRequestTracker(t.Context(), pageURL, logger), &buf
	}

	records := func(t *testing.T, buf *bytes.Buffer) []map[string]any {
		t.Helper()
		var records []map[string]any
		scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
		for scanner.Scan() {
			var record map[string]any
			require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
			records = append(records, record)
		}
		return records
	}

	request := func(id, url string, at proto.MonotonicTime) *proto.NetworkRequestWillBeSent {
		return &proto.NetworkRequestWillBeSent{
			RequestID: proto.NetworkRequestID(id),
			Request:   &proto.NetworkRequest{URL: url},
			Type:      proto.NetworkResourceTypeImage,
			Timestamp: at,
		}
	}

	response := func(id string, status int, at proto.MonotonicTime) *proto.NetworkResponseReceived {
		return &proto.NetworkResponseReceived{
			RequestID: proto.NetworkRequestID(id),
			Response:  &proto.NetworkResponse{Status: status, Protocol: "h2", RemoteIPAddress: "1.2.3.4"},
			Timestamp: at,
		}
	}

	failure := func(id, errorText string, at proto.MonotonicTime) *proto.NetworkLoadingFailed {
		return &proto.NetworkLoadingFailed{
			RequestID: proto.NetworkRequestID(id),
			ErrorText: errorText,
			Timestamp: at,
		}
	}

	finished := func(id string) *proto.NetworkLoadingFinished {
		return &proto.NetworkLoadingFinished{RequestID: proto.NetworkRequestID(id)}
	}

	t.Run("it_logs_responses_with_an_error_status", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onResponse(response("1", 503, 10.25))
		tracker.onFinished(finished("1"))

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, "WARN", logs[0]["level"])
		assert.Equal(t, "page request failed", logs[0]["msg"])
		assert.Equal(t, "network", logs[0]["component"])
		assert.Equal(t, "https://bucket.example/a.jpg", logs[0]["url"])
		assert.Equal(t, "Image", logs[0]["type"])
		assert.Equal(t, float64(503), logs[0]["status"])
		assert.Equal(t, "h2", logs[0]["protocol"])
		assert.Equal(t, "1.2.3.4", logs[0]["remote_ip"])
		assert.Equal(t, float64(250), logs[0]["duration_ms"])
		total, failed := tracker.counts()
		assert.Equal(t, 1, total)
		assert.Equal(t, 1, failed)
	})

	t.Run("it_logs_network_errors", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onFailed(&proto.NetworkLoadingFailed{
			RequestID:     "1",
			ErrorText:     "net::ERR_BLOCKED_BY_ORB",
			BlockedReason: proto.NetworkBlockedReasonOrigin,
			Timestamp:     10.5,
		})

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, "page request failed", logs[0]["msg"])
		assert.Equal(t, "net::ERR_BLOCKED_BY_ORB", logs[0]["error"])
		assert.Equal(t, "origin", logs[0]["blocked_reason"])
		assert.NotContains(t, logs[0], "canceled")
		assert.NotContains(t, logs[0], "status")
		assert.Equal(t, float64(500), logs[0]["duration_ms"])
	})

	t.Run("it_logs_the_status_of_responses_that_chromium_blocked", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onResponseHeaders(&proto.NetworkResponseReceivedExtraInfo{RequestID: "1", StatusCode: 503})
		tracker.onFailed(failure("1", "net::ERR_BLOCKED_BY_ORB", 10))
		tracker.onRequest(request("2", "https://bucket.example/b.jpg", 10))
		tracker.onResponseHeaders(&proto.NetworkResponseReceivedExtraInfo{RequestID: "2", StatusCode: 403})
		tracker.onFailed(failure("2", "net::ERR_BLOCKED_BY_ORB", 10))

		logs := records(t, buf)
		require.Len(t, logs, 2)
		assert.Equal(t, float64(503), logs[0]["status"])
		assert.Equal(t, "net::ERR_BLOCKED_BY_ORB", logs[0]["error"])
		assert.Equal(t, float64(403), logs[1]["status"])
	})

	t.Run("it_summarizes_failures_by_status_and_error", func(t *testing.T) {
		tracker, buf := newTracker(t)

		for i := range maxLoggedFailures + 3 {
			id := fmt.Sprint(i)
			tracker.onRequest(request(id, fmt.Sprintf("https://bucket.example/%d.jpg", i), 10))
			switch {
			case i < 2:
				tracker.onFailed(failure(id, "net::ERR_CONNECTION_RESET", 10))
			case i < 5:
				tracker.onResponse(response(id, 404, 10))
			default:
				tracker.onResponseHeaders(&proto.NetworkResponseReceivedExtraInfo{RequestID: proto.NetworkRequestID(id), StatusCode: 503})
				tracker.onFailed(failure(id, "net::ERR_BLOCKED_BY_ORB", 10))
			}
		}
		tracker.logSummary()

		logs := records(t, buf)
		summary := logs[len(logs)-1]
		assert.Equal(t, "page requests failed", summary["msg"])
		assert.Equal(t, map[string]any{
			"404":                         float64(3),
			"503 net::ERR_BLOCKED_BY_ORB": float64(maxLoggedFailures - 2),
			"net::ERR_CONNECTION_RESET":   float64(2),
		}, summary["reasons"])
	})

	t.Run("it_keeps_a_status_that_arrives_before_its_request", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onResponseHeaders(&proto.NetworkResponseReceivedExtraInfo{RequestID: "1", StatusCode: 503})
		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onFailed(failure("1", "net::ERR_BLOCKED_BY_ORB", 10))

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, float64(503), logs[0]["status"])
		assert.Empty(t, tracker.statuses)
	})

	t.Run("it_forgets_the_status_of_finished_requests", func(t *testing.T) {
		tracker, _ := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onResponseHeaders(&proto.NetworkResponseReceivedExtraInfo{RequestID: "1", StatusCode: 200})
		tracker.onFinished(finished("1"))

		assert.Empty(t, tracker.statuses)
	})

	t.Run("it_ignores_the_status_of_a_redirect", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onResponseHeaders(&proto.NetworkResponseReceivedExtraInfo{RequestID: "1", StatusCode: 302})
		redirect := request("1", "https://elsewhere.example/a.jpg", 10)
		redirect.RedirectResponse = &proto.NetworkResponse{Status: 302}
		tracker.onRequest(redirect)
		tracker.onFailed(failure("1", "net::ERR_CONNECTION_REFUSED", 10))

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, "https://elsewhere.example/a.jpg", logs[0]["url"])
		assert.NotContains(t, logs[0], "status")
	})

	t.Run("it_logs_canceled_requests", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onFailed(&proto.NetworkLoadingFailed{RequestID: "1", ErrorText: "net::ERR_ABORTED", Canceled: true, Timestamp: 10})

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, true, logs[0]["canceled"])
	})

	t.Run("it_counts_a_request_that_fails_after_an_error_status_once", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onResponse(response("1", 500, 10))
		tracker.onFailed(failure("1", "net::ERR_CONNECTION_RESET", 11))

		assert.Len(t, records(t, buf), 1)
		_, failed := tracker.counts()
		assert.Equal(t, 1, failed)
	})

	t.Run("it_does_not_log_successful_requests", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onResponse(response("1", 200, 10))
		tracker.onFinished(finished("1"))
		tracker.onRequest(request("2", "https://bucket.example/b.jpg", 10))
		tracker.onResponse(response("2", 304, 10))
		tracker.onFinished(finished("2"))
		tracker.logPending()
		tracker.logSummary()

		assert.Empty(t, records(t, buf))
		total, failed := tracker.counts()
		assert.Equal(t, 2, total)
		assert.Equal(t, 0, failed)
	})

	t.Run("it_counts_a_redirected_request_once", func(t *testing.T) {
		tracker, _ := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		redirect := request("1", "https://bucket.example/b.jpg", 10)
		redirect.RedirectResponse = &proto.NetworkResponse{Status: 302}
		tracker.onRequest(redirect)
		tracker.onFinished(finished("1"))

		total, _ := tracker.counts()
		assert.Equal(t, 1, total)
	})

	t.Run("it_logs_the_page_servers_urls_as_paths", func(t *testing.T) {
		cases := map[string]string{
			pageURL + "/assets/missing.css":         "/assets/missing.css",
			"http://127.0.0.1:456851/assets/x.css":  "http://127.0.0.1:456851/assets/x.css",
			"https://bucket.example/assets/x.css":   "https://bucket.example/assets/x.css",
			"data:image/png;base64,iVBORw0KGgoAAAA": "data:image/png;base64,…",
		}

		for url, want := range cases {
			t.Run(url, func(t *testing.T) {
				tracker, buf := newTracker(t)

				tracker.onRequest(request("1", url, 10))
				tracker.onFailed(failure("1", "net::ERR_FAILED", 10))

				logs := records(t, buf)
				require.Len(t, logs, 1)
				assert.Equal(t, want, logs[0]["url"])
			})
		}
	})

	t.Run("it_keeps_urls_of_a_page_without_host", func(t *testing.T) {
		var buf bytes.Buffer
		tracker := newRequestTracker(t.Context(), "data:text/html,<p>hi</p>", slog.New(slog.NewJSONHandler(&buf, nil)))

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onFailed(failure("1", "net::ERR_FAILED", 10))

		logs := records(t, &buf)
		require.Len(t, logs, 1)
		assert.Equal(t, "https://bucket.example/a.jpg", logs[0]["url"])
	})

	t.Run("it_logs_the_first_failures_and_summarizes_all", func(t *testing.T) {
		tracker, buf := newTracker(t)

		for i := range maxLoggedFailures + 5 {
			id := fmt.Sprint(i)
			tracker.onRequest(request(id, fmt.Sprintf("https://bucket.example/%d.jpg", i), 10))
			if i%10 == 0 {
				tracker.onFailed(failure(id, "net::ERR_CONNECTION_RESET", 10))
			} else {
				tracker.onResponse(response(id, 503, 10))
			}
		}
		tracker.logSummary()

		logs := records(t, buf)
		require.Len(t, logs, maxLoggedFailures+1)
		for _, record := range logs[:maxLoggedFailures] {
			assert.Equal(t, "page request failed", record["msg"])
		}
		summary := logs[maxLoggedFailures]
		assert.Equal(t, "WARN", summary["level"])
		assert.Equal(t, "page requests failed", summary["msg"])
		assert.Equal(t, "network", summary["component"])
		assert.Equal(t, float64(maxLoggedFailures+5), summary["count"])
		assert.Equal(t, float64(maxLoggedFailures), summary["logged"])
		assert.Equal(t, map[string]any{"503": float64(22), "net::ERR_CONNECTION_RESET": float64(3)}, summary["reasons"])
	})

	t.Run("it_does_not_summarize_if_all_failures_were_logged", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/a.jpg", 10))
		tracker.onResponse(response("1", 404, 10))
		tracker.logSummary()

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, "page request failed", logs[0]["msg"])
	})

	t.Run("it_logs_pending_requests_in_the_order_they_were_sent", func(t *testing.T) {
		tracker, buf := newTracker(t)

		tracker.onRequest(request("1", "https://bucket.example/late.jpg", 12))
		tracker.onRequest(request("2", "https://bucket.example/done.jpg", 10))
		tracker.onRequest(request("3", pageURL+"/assets/early.css", 11))
		tracker.onFinished(finished("2"))
		tracker.logPending()

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, "WARN", logs[0]["level"])
		assert.Equal(t, "page requests pending", logs[0]["msg"])
		assert.Equal(t, "network", logs[0]["component"])
		assert.Equal(t, float64(2), logs[0]["count"])
		assert.Equal(t, []any{"/assets/early.css", "https://bucket.example/late.jpg"}, logs[0]["urls"])
	})

	t.Run("it_lists_a_limited_number_of_pending_requests", func(t *testing.T) {
		tracker, buf := newTracker(t)

		for i := range maxListedPending + 5 {
			tracker.onRequest(request(fmt.Sprint(i), fmt.Sprintf("https://bucket.example/%d.jpg", i), proto.MonotonicTime(i)))
		}
		tracker.logPending()

		logs := records(t, buf)
		require.Len(t, logs, 1)
		assert.Equal(t, float64(maxListedPending+5), logs[0]["count"])
		assert.Len(t, logs[0]["urls"], maxListedPending)
	})
}

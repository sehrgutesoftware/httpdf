package httpdf_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// logRecords parses the JSON log lines written to buf
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
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

// logRecord returns the single log record with the given message
func logRecord(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, record := range logRecords(t, buf) {
		if record["msg"] == msg {
			found = append(found, record)
		}
	}
	require.Len(t, found, 1, "expected exactly one %q log record in:\n%s", msg, buf.String())
	return found[0]
}

var assetModTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// testAssets is an assets filesystem with a single stylesheet
var testAssets = fstest.MapFS{
	"style.css": {Data: []byte("body {}"), ModTime: assetModTime},
}

// panickingHTTPDF panics while generating, optionally after starting the
// response
type panickingHTTPDF struct {
	afterWrite bool
}

func (h *panickingHTTPDF) Generate(ctx context.Context, t *template.Template, locale string, v map[string]any, w io.Writer) error {
	if h.afterWrite {
		_, _ = w.Write([]byte("%PDF-1.4"))
	}
	panic("kaboom")
}

func TestServer_RequestLog(t *testing.T) {
	loader := &stubLoader{templates: map[string]*template.Template{
		"known":       {},
		"with_assets": {Assets: testAssets},
	}}

	serve := func(generator httpdf.HTTPDF, req *http.Request) (*httptest.ResponseRecorder, *bytes.Buffer) {
		var buf bytes.Buffer
		server := httpdf.NewServer(generator, loader, httpdf.WithLogger(newTestLogger(&buf)))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec, &buf
	}

	renderRequest := func(tmpl string, body string) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/templates/"+tmpl+"/render?lang=de", strings.NewReader(body))
	}

	t.Run("it_logs_successful_requests_at_info", func(t *testing.T) {
		rec, buf := serve(&stubHTTPDF{}, renderRequest("known", `{}`))

		record := logRecord(t, buf, "request")
		assert.Equal(t, "INFO", record["level"])
		assert.Equal(t, "POST", record["method"])
		assert.Equal(t, "/templates/known/render", record["path"])
		assert.Equal(t, float64(200), record["status"])
		assert.Equal(t, float64(len("%PDF-1.4")), record["bytes"])
		assert.Contains(t, record, "duration_ms")
		assert.Equal(t, "known", record["template"])
		assert.Equal(t, "de", record["locale"])
		assert.NotContains(t, record, "error")
		assert.NotContains(t, record, "trace_id")
		assert.NotEmpty(t, record["request_id"])
		assert.Equal(t, rec.Header().Get("X-Request-ID"), record["request_id"])
	})

	t.Run("it_logs_client_errors_at_warn", func(t *testing.T) {
		invalidValues := fmt.Errorf("%w: name is required", httpdf.ErrInvalidValues)
		cases := map[string]struct {
			generator httpdf.HTTPDF
			req       *http.Request
			status    float64
			err       string
		}{
			"invalid_json":     {&stubHTTPDF{}, renderRequest("known", `{not json`), 400, "invalid character"},
			"unknown_template": {&stubHTTPDF{}, renderRequest("unknown", `{}`), 404, "template not found"},
			"invalid_values":   {&stubHTTPDF{err: invalidValues}, renderRequest("known", `{}`), 422, "name is required"},
		}

		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				_, buf := serve(tc.generator, tc.req)

				record := logRecord(t, buf, "request")
				assert.Equal(t, "WARN", record["level"])
				assert.Equal(t, tc.status, record["status"])
				assert.Contains(t, record["error"], tc.err)
			})
		}
	})

	t.Run("it_logs_server_errors_at_error", func(t *testing.T) {
		_, buf := serve(&stubHTTPDF{err: errors.New("browser exploded")}, renderRequest("known", `{}`))

		record := logRecord(t, buf, "request")
		assert.Equal(t, "ERROR", record["level"])
		assert.Equal(t, float64(500), record["status"])
		assert.Equal(t, "browser exploded", record["error"])
	})

	t.Run("it_recovers_from_panics_and_logs_them_at_error", func(t *testing.T) {
		rec, buf := serve(&panickingHTTPDF{}, renderRequest("known", `{}`))

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.NotEqual(t, "application/pdf", rec.Header().Get("Content-Type"))
		record := logRecord(t, buf, "request")
		assert.Equal(t, "ERROR", record["level"])
		assert.Equal(t, float64(500), record["status"])
		assert.Equal(t, "panic: kaboom", record["error"])
		assert.Contains(t, record["stack"], "panickingHTTPDF")
	})

	t.Run("it_aborts_the_response_on_panics_after_it_was_started", func(t *testing.T) {
		var buf bytes.Buffer
		server := httpdf.NewServer(&panickingHTTPDF{afterWrite: true}, loader, httpdf.WithLogger(newTestLogger(&buf)))

		assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
			server.ServeHTTP(httptest.NewRecorder(), renderRequest("known", `{}`))
		})

		record := logRecord(t, &buf, "request")
		assert.Equal(t, "ERROR", record["level"])
		assert.Equal(t, float64(200), record["status"])
		assert.Equal(t, "panic: kaboom", record["error"])
	})

	t.Run("it_logs_canceled_requests_at_info_with_status_499", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := renderRequest("known", `{}`).WithContext(ctx)

		_, buf := serve(&stubHTTPDF{err: context.Canceled}, req)

		record := logRecord(t, buf, "request")
		assert.Equal(t, "INFO", record["level"])
		assert.Equal(t, float64(499), record["status"])
	})

	t.Run("it_logs_health_checks_at_debug", func(t *testing.T) {
		_, buf := serve(&stubHTTPDF{}, httptest.NewRequest(http.MethodGet, "/healthz", nil))

		record := logRecord(t, buf, "request")
		assert.Equal(t, "DEBUG", record["level"])
		assert.Equal(t, float64(200), record["status"])
	})

	t.Run("it_logs_successful_asset_requests_at_debug", func(t *testing.T) {
		_, buf := serve(&stubHTTPDF{}, httptest.NewRequest(http.MethodGet, "/templates/with_assets/assets/style.css", nil))

		record := logRecord(t, buf, "request")
		assert.Equal(t, "DEBUG", record["level"])
		assert.Equal(t, float64(200), record["status"])
		assert.Equal(t, "with_assets", record["template"])
		assert.NotContains(t, record, "component")
	})

	t.Run("it_logs_not_modified_asset_requests_at_debug", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/templates/with_assets/assets/style.css", nil)
		req.Header.Set("If-Modified-Since", assetModTime.Format(http.TimeFormat))

		_, buf := serve(&stubHTTPDF{}, req)

		record := logRecord(t, buf, "request")
		assert.Equal(t, "DEBUG", record["level"])
		assert.Equal(t, float64(304), record["status"])
	})

	t.Run("it_logs_canceled_asset_requests_at_debug", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/templates/with_assets/assets/style.css", nil).WithContext(ctx)

		_, buf := serve(&stubHTTPDF{}, req)

		record := logRecord(t, buf, "request")
		assert.Equal(t, "DEBUG", record["level"])
		assert.Equal(t, float64(499), record["status"])
	})

	t.Run("it_logs_failed_asset_requests_at_warn", func(t *testing.T) {
		cases := map[string]string{
			"missing_asset":       "/templates/with_assets/assets/missing.css",
			"template_has_none":   "/templates/known/assets/style.css",
			"template_is_unknown": "/templates/unknown/assets/style.css",
		}

		for name, path := range cases {
			t.Run(name, func(t *testing.T) {
				_, buf := serve(&stubHTTPDF{}, httptest.NewRequest(http.MethodGet, path, nil))

				record := logRecord(t, buf, "request")
				assert.Equal(t, "WARN", record["level"])
				assert.Equal(t, float64(404), record["status"])
			})
		}
	})

	t.Run("it_keeps_a_valid_client_request_id", func(t *testing.T) {
		req := renderRequest("known", `{}`)
		req.Header.Set("X-Request-ID", "abc-123")

		rec, buf := serve(&stubHTTPDF{}, req)

		assert.Equal(t, "abc-123", rec.Header().Get("X-Request-ID"))
		assert.Equal(t, "abc-123", logRecord(t, buf, "request")["request_id"])
	})

	t.Run("it_replaces_invalid_client_request_ids", func(t *testing.T) {
		cases := map[string]string{
			"too_long":           strings.Repeat("a", 129),
			"contains_space":     "abc 123",
			"contains_non_ascii": "abc-ü",
		}

		for name, id := range cases {
			t.Run(name, func(t *testing.T) {
				req := renderRequest("known", `{}`)
				req.Header.Set("X-Request-ID", id)

				rec, buf := serve(&stubHTTPDF{}, req)

				got := rec.Header().Get("X-Request-ID")
				assert.NotEmpty(t, got)
				assert.NotEqual(t, id, got)
				assert.Equal(t, got, logRecord(t, buf, "request")["request_id"])
			})
		}
	})

	t.Run("it_logs_the_trace_id_from_traceparent", func(t *testing.T) {
		const trace = "4bf92f3577b34da6a3ce929d0e0e4736"
		cases := map[string]struct {
			headers []string
			want    string
		}{
			"valid":                  {[]string{"00-" + trace + "-00f067aa0ba902b7-01"}, trace},
			"future_version_suffix":  {[]string{"01-" + trace + "-00f067aa0ba902b7-01-extra"}, trace},
			"surrounding_whitespace": {[]string{" 00-" + trace + "-00f067aa0ba902b7-01 "}, trace},
			"version_00_with_suffix": {[]string{"00-" + trace + "-00f067aa0ba902b7-01-extra"}, ""},
			"invalid_version_ff":     {[]string{"ff-" + trace + "-00f067aa0ba902b7-01"}, ""},
			"all_zero_trace_id":      {[]string{"00-" + strings.Repeat("0", 32) + "-00f067aa0ba902b7-01"}, ""},
			"all_zero_parent_id":     {[]string{"00-" + trace + "-" + strings.Repeat("0", 16) + "-01"}, ""},
			"uppercase_hex":          {[]string{"00-" + strings.ToUpper(trace) + "-00f067aa0ba902b7-01"}, ""},
			"malformed":              {[]string{"not-a-traceparent"}, ""},
			"multiple_headers":       {[]string{"00-" + trace + "-00f067aa0ba902b7-01", "00-" + trace + "-00f067aa0ba902b7-01"}, ""},
		}

		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				req := renderRequest("known", `{}`)
				for _, h := range tc.headers {
					req.Header.Add("traceparent", h)
				}

				_, buf := serve(&stubHTTPDF{}, req)

				record := logRecord(t, buf, "request")
				if tc.want == "" {
					assert.NotContains(t, record, "trace_id")
				} else {
					assert.Equal(t, tc.want, record["trace_id"])
				}
			})
		}
	})

	t.Run("it_allows_tracing_headers_in_cors_preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/templates/known/render", nil)
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "content-type, x-request-id, traceparent")

		rec, _ := serve(&stubHTTPDF{}, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestServer_RequestLogCorrelation(t *testing.T) {
	t.Run("it_adds_request_and_trace_ids_to_logs_emitted_during_the_request", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf)
		loader := &stubLoader{templates: map[string]*template.Template{
			"hello": newTemplate(t, `Hello`, `{}`),
		}}
		app := httpdf.New(&fetchingRenderer{}, httpdf.WithLogger(logger))
		server := httpdf.NewServer(app, loader, httpdf.WithLogger(logger))

		req := httptest.NewRequest(http.MethodPost, "/templates/hello/render", strings.NewReader(`{}`))
		req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		request := logRecord(t, &buf, "request")
		inner := logRecord(t, &buf, "temporary server started")
		assert.Equal(t, request["request_id"], inner["request_id"])
		assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", inner["trace_id"])
	})

	t.Run("it_logs_the_time_spent_waiting_for_a_render_slot", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf)
		loader := &stubLoader{templates: map[string]*template.Template{
			"hello": newTemplate(t, `Hello`, `{}`),
		}}
		app := httpdf.New(&fetchingRenderer{}, httpdf.WithLogger(logger))
		server := httpdf.NewServer(app, loader, httpdf.WithLogger(logger))

		req := httptest.NewRequest(http.MethodPost, "/templates/hello/render", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		assert.Contains(t, logRecord(t, &buf, "request"), "queue_ms")
	})
}

func TestServer_TemplateLoadedLog(t *testing.T) {
	render := func(server http.Handler, tmpl string) {
		req := httptest.NewRequest(http.MethodPost, "/templates/"+tmpl+"/render", strings.NewReader(`{}`))
		server.ServeHTTP(httptest.NewRecorder(), req)
	}

	t.Run("it_logs_when_a_template_is_loaded_into_the_cache", func(t *testing.T) {
		var buf bytes.Buffer
		loader := &stubLoader{templates: map[string]*template.Template{"known": {}}}
		server := httpdf.NewServer(&stubHTTPDF{}, loader, httpdf.WithLogger(newTestLogger(&buf)))

		render(server, "known")
		render(server, "known")

		record := logRecord(t, &buf, "template loaded")
		assert.Equal(t, "INFO", record["level"])
		assert.Equal(t, "known", record["template"])
		assert.Contains(t, record, "duration_ms")
		var requests []map[string]any
		for _, r := range logRecords(t, &buf) {
			if r["msg"] == "request" {
				requests = append(requests, r)
			}
		}
		require.Len(t, requests, 2)
		assert.Equal(t, requests[0]["request_id"], record["request_id"])
	})

	t.Run("it_does_not_log_failed_loads", func(t *testing.T) {
		var buf bytes.Buffer
		loader := &stubLoader{templates: map[string]*template.Template{}}
		server := httpdf.NewServer(&stubHTTPDF{}, loader, httpdf.WithLogger(newTestLogger(&buf)))

		render(server, "unknown")

		for _, record := range logRecords(t, &buf) {
			assert.NotEqual(t, "template loaded", record["msg"])
		}
	})
}

func TestHTTPDF_AssetLog(t *testing.T) {
	withAssets := newTemplate(t, `<link rel="stylesheet" href="/assets/style.css">`, `{}`)
	withAssets.Assets = testAssets
	loader := &stubLoader{templates: map[string]*template.Template{
		"with_assets": withAssets,
		"no_assets":   newTemplate(t, `<link rel="stylesheet" href="/assets/style.css">`, `{}`),
	}}

	// render renders the template with a renderer that fetches the given path
	// from the temporary server
	render := func(t *testing.T, renderer pdf.Renderer, tmpl string) *bytes.Buffer {
		t.Helper()
		var buf bytes.Buffer
		logger := newTestLogger(&buf)
		app := httpdf.New(renderer, httpdf.WithLogger(logger))
		server := httpdf.NewServer(app, loader, httpdf.WithLogger(logger))

		req := httptest.NewRequest(http.MethodPost, "/templates/"+tmpl+"/render", strings.NewReader(`{}`))
		req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		return &buf
	}

	t.Run("it_logs_successful_asset_requests_at_debug", func(t *testing.T) {
		buf := render(t, &assetRenderer{path: "/assets/style.css"}, "with_assets")

		record := logRecord(t, buf, "asset request")
		assert.Equal(t, "DEBUG", record["level"])
		assert.Equal(t, "assets", record["component"])
		assert.Equal(t, "/assets/style.css", record["path"])
		assert.Equal(t, float64(200), record["status"])
		assert.Equal(t, float64(len("body {}")), record["bytes"])
		assert.Contains(t, record, "duration_ms")
		assert.Equal(t, logRecord(t, buf, "request")["request_id"], record["request_id"])
		assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", record["trace_id"])
	})

	t.Run("it_logs_failed_asset_requests_at_debug", func(t *testing.T) {
		cases := map[string]struct {
			path string
			tmpl string
		}{
			"missing_asset":     {"/assets/missing.css", "with_assets"},
			"template_has_none": {"/assets/style.css", "no_assets"},
		}

		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				buf := render(t, &assetRenderer{path: tc.path}, tc.tmpl)

				record := logRecord(t, buf, "asset request")
				assert.Equal(t, "DEBUG", record["level"])
				assert.Equal(t, "assets", record["component"])
				assert.Equal(t, tc.path, record["path"])
				assert.Equal(t, float64(404), record["status"])
				assert.Equal(t, logRecord(t, buf, "request")["request_id"], record["request_id"])
			})
		}
	})

	t.Run("it_does_not_log_requests_for_the_page", func(t *testing.T) {
		buf := render(t, &fetchingRenderer{}, "with_assets")

		for _, record := range logRecords(t, buf) {
			assert.NotEqual(t, "asset request", record["msg"])
		}
	})
}

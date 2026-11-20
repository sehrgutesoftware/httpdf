package pdf

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sehrgutesoftware/httpdf/internal/logging"
)

// requestTracker follows the page's requests through the DevTools protocol and
// logs those that fail, because Chromium prints the PDF without them. Each one
// is logged at debug level, and summarized in a warning, so that an
// unreachable server doesn't flood the log with a warning per image.
type requestTracker struct {
	ctx    context.Context
	logger *slog.Logger
	origin string

	mu       sync.Mutex
	pending  map[proto.NetworkRequestID]trackedRequest
	statuses map[proto.NetworkRequestID]int
	total    int
	failures map[string]int
	failed   int
}

type trackedRequest struct {
	url     string
	typ     proto.NetworkResourceType
	started proto.MonotonicTime
}

// trackRequests starts tracking the requests of the page, which is going to
// be navigated to pageURL. It must be called before navigating, so that no
// request is missed. Tracking ends with ctx or when stop is called.
func trackRequests(ctx context.Context, page *rod.Page, pageURL string, logger *slog.Logger) (t *requestTracker, stop func(), err error) {
	// Chromium keeps the responses for DevTools while the Network domain is
	// enabled, which httpdf never reads. Enabled here, the domain also stays
	// enabled when EachEvent or rod's WaitRequestIdle finish.
	noBuffer := 0
	err = proto.NetworkEnable{MaxTotalBufferSize: &noBuffer, MaxResourceBufferSize: &noBuffer}.Call(page)
	if err != nil {
		return nil, nil, fmt.Errorf("enable network events: %w", err)
	}

	t = newRequestTracker(ctx, pageURL, logger)
	eventsCtx, stop := context.WithCancel(ctx)
	// EachEvent subscribes right away, wait only dispatches the events
	wait := page.Context(eventsCtx).EachEvent(t.onRequest, t.onResponseHeaders, t.onResponse, t.onFinished, t.onFailed)
	go wait()

	return t, stop, nil
}

func newRequestTracker(ctx context.Context, pageURL string, logger *slog.Logger) *requestTracker {
	t := &requestTracker{
		ctx:      ctx,
		logger:   logger,
		pending:  map[proto.NetworkRequestID]trackedRequest{},
		statuses: map[proto.NetworkRequestID]int{},
		failures: map[string]int{},
	}
	if u, err := url.Parse(pageURL); err == nil && u.Host != "" {
		t.origin = u.Scheme + "://" + u.Host
	}
	return t
}

func (t *requestTracker) onRequest(e *proto.NetworkRequestWillBeSent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Redirects are sent with the request ID of the original request
	if e.RedirectResponse == nil {
		t.total++
	}
	t.pending[e.RequestID] = trackedRequest{url: e.Request.URL, typ: e.Type, started: e.Timestamp}
}

// onResponseHeaders records the status of a response as it was received.
// Chromium doesn't pass some responses on to the page, which then fails with
// an error instead, e.g. net::ERR_BLOCKED_BY_ORB for cross-origin images with
// an error status and an XML body, as S3 sends them for missing objects or
// throttled requests. Their status is only known from here.
//
// The event comes from Chromium's network service rather than the page, so
// it may arrive before the request's onRequest.
func (t *requestTracker) onResponseHeaders(e *proto.NetworkResponseReceivedExtraInfo) {
	// A redirect is followed by another request, whose failure isn't the
	// redirect's
	if e.StatusCode >= 300 && e.StatusCode < 400 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.statuses[e.RequestID] = e.StatusCode
}

func (t *requestTracker) onResponse(e *proto.NetworkResponseReceived) {
	if e.Response.Status < 400 {
		return
	}

	t.fail(e.RequestID, e.Timestamp, e.Response.Status, "",
		slog.String("protocol", e.Response.Protocol),
		slog.String("remote_ip", e.Response.RemoteIPAddress),
	)
}

func (t *requestTracker) onFinished(e *proto.NetworkLoadingFinished) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.pending, e.RequestID)
	delete(t.statuses, e.RequestID)
}

func (t *requestTracker) onFailed(e *proto.NetworkLoadingFailed) {
	var attrs []slog.Attr
	if e.Canceled {
		attrs = append(attrs, slog.Bool("canceled", true))
	}
	if e.BlockedReason != "" {
		attrs = append(attrs, slog.String("blocked_reason", string(e.BlockedReason)))
	}

	t.fail(e.RequestID, e.Timestamp, 0, e.ErrorText, attrs...)
}

// fail counts a failed request under its status and error, and logs it.
// Without a status, the one recorded by onResponseHeaders is used, if any. A
// request is only counted once, e.g. if its body fails to load after an error
// status.
func (t *requestTracker) fail(id proto.NetworkRequestID, at proto.MonotonicTime, status int, errorText string, attrs ...slog.Attr) {
	t.mu.Lock()
	r, ok := t.pending[id]
	status = cmp.Or(status, t.statuses[id])
	delete(t.pending, id)
	delete(t.statuses, id)
	if !ok {
		t.mu.Unlock()
		return
	}

	var reason []string
	if status != 0 {
		reason = append(reason, strconv.Itoa(status))
		attrs = append(attrs, slog.Int("status", status))
	}
	if errorText != "" {
		reason = append(reason, errorText)
		attrs = append(attrs, slog.String("error", errorText))
	}

	t.failed++
	t.failures[strings.Join(reason, " ")]++
	t.mu.Unlock()

	t.logger.LogAttrs(t.ctx, slog.LevelDebug, "page request failed", append([]slog.Attr{
		slog.String("component", "network"),
		slog.String("url", t.logURL(r.url)),
		slog.String("type", string(r.typ)),
		slog.Float64("duration_ms", logging.Millis((at - r.started).Duration())),
	}, attrs...)...)
}

// logPending logs the requests that haven't finished yet, each at debug level,
// and their number as a warning. Meant to be called right before printing,
// because the PDF won't contain their resources.
func (t *requestTracker) logPending() {
	t.mu.Lock()
	pending := make([]trackedRequest, 0, len(t.pending))
	for _, r := range t.pending {
		pending = append(pending, r)
	}
	t.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	slices.SortFunc(pending, func(a, b trackedRequest) int { return cmp.Compare(a.started, b.started) })
	for _, r := range pending {
		t.logger.LogAttrs(t.ctx, slog.LevelDebug, "page request pending",
			slog.String("component", "network"),
			slog.String("url", t.logURL(r.url)),
			slog.String("type", string(r.typ)),
		)
	}

	t.logger.LogAttrs(t.ctx, slog.LevelWarn, "page requests pending", t.withDebugHint("pending",
		slog.String("component", "network"),
		slog.Int("count", len(pending)),
	)...)
}

// logSummary logs the number of failed requests by status and error as a
// warning, if any failed
func (t *requestTracker) logSummary() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.failed == 0 {
		return
	}

	reasons := make([]string, 0, len(t.failures))
	for reason := range t.failures {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)
	counts := make([]any, len(reasons))
	for i, reason := range reasons {
		counts[i] = slog.Int(reason, t.failures[reason])
	}

	t.logger.LogAttrs(t.ctx, slog.LevelWarn, "page requests failed", t.withDebugHint("failed",
		slog.String("component", "network"),
		slog.Int("count", t.failed),
		slog.Int("requests", t.total),
		slog.Group("reasons", counts...),
	)...)
}

// withDebugHint adds a hint to the attributes of a warning that summarizes
// requests, which are logged one by one at debug level, unless debug level is
// enabled already
func (t *requestTracker) withDebugHint(requests string, attrs ...slog.Attr) []slog.Attr {
	if t.logger.Enabled(t.ctx, slog.LevelDebug) {
		return attrs
	}
	return append(attrs, slog.String("hint", "Each "+requests+" request is logged at debug level, see HTTPDF_LOG_LEVEL"))
}

// counts returns the number of requests so far and how many of them failed
func (t *requestTracker) counts() (total, failed int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.total, t.failed
}

// logURL shortens URLs of the page's own server to their path, which, unlike
// the server's port, stays the same across renders, and data URLs to their
// media type
func (t *requestTracker) logURL(u string) string {
	if t.origin != "" {
		if path, ok := strings.CutPrefix(u, t.origin); ok && strings.HasPrefix(path, "/") {
			return path
		}
	}
	if mediaType, _, ok := strings.Cut(u, ","); ok && strings.HasPrefix(u, "data:") {
		return mediaType + ",…"
	}
	return u
}

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

// maxLoggedFailures is how many failed requests per render are logged one by
// one. The rest are only counted in a summary, so that an unreachable server
// doesn't flood the log with a line per image.
const maxLoggedFailures = 20

// maxListedPending is how many URLs of pending requests are logged when the
// PDF is printed
const maxListedPending = 20

// requestTracker follows the page's requests through the DevTools protocol and
// logs those that fail, because Chromium prints the PDF without them.
type requestTracker struct {
	ctx    context.Context
	logger *slog.Logger
	origin string

	mu       sync.Mutex
	pending  map[proto.NetworkRequestID]trackedRequest
	total    int
	failures map[string]int
	failed   int
	logged   int
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
	wait := page.Context(eventsCtx).EachEvent(t.onRequest, t.onResponse, t.onFinished, t.onFailed)
	go wait()

	return t, stop, nil
}

func newRequestTracker(ctx context.Context, pageURL string, logger *slog.Logger) *requestTracker {
	t := &requestTracker{
		ctx:      ctx,
		logger:   logger,
		pending:  map[proto.NetworkRequestID]trackedRequest{},
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

func (t *requestTracker) onResponse(e *proto.NetworkResponseReceived) {
	if e.Response.Status < 400 {
		return
	}

	t.fail(e.RequestID, strconv.Itoa(e.Response.Status), e.Timestamp,
		slog.Int("status", e.Response.Status),
		slog.String("protocol", e.Response.Protocol),
		slog.String("remote_ip", e.Response.RemoteIPAddress),
	)
}

func (t *requestTracker) onFinished(e *proto.NetworkLoadingFinished) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.pending, e.RequestID)
}

func (t *requestTracker) onFailed(e *proto.NetworkLoadingFailed) {
	attrs := []slog.Attr{slog.String("error", e.ErrorText)}
	if e.Canceled {
		attrs = append(attrs, slog.Bool("canceled", true))
	}
	if e.BlockedReason != "" {
		attrs = append(attrs, slog.String("blocked_reason", string(e.BlockedReason)))
	}

	t.fail(e.RequestID, e.ErrorText, e.Timestamp, attrs...)
}

// fail counts a failed request under the given reason and logs it, unless
// maxLoggedFailures have been logged already. A request is only counted once,
// e.g. if its body fails to load after an error status.
func (t *requestTracker) fail(id proto.NetworkRequestID, reason string, at proto.MonotonicTime, attrs ...slog.Attr) {
	t.mu.Lock()
	r, ok := t.pending[id]
	if !ok {
		t.mu.Unlock()
		return
	}
	delete(t.pending, id)
	t.failed++
	t.failures[reason]++
	log := t.logged < maxLoggedFailures
	if log {
		t.logged++
	}
	t.mu.Unlock()

	if !log {
		return
	}

	t.logger.LogAttrs(t.ctx, slog.LevelWarn, "page request failed", append([]slog.Attr{
		slog.String("component", "network"),
		slog.String("url", t.logURL(r.url)),
		slog.String("type", string(r.typ)),
		slog.Float64("duration_ms", logging.Millis((at - r.started).Duration())),
	}, attrs...)...)
}

// logPending logs the requests that haven't finished yet. Meant to be called
// right before printing, because the PDF won't contain their resources.
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
	urls := make([]string, min(len(pending), maxListedPending))
	for i := range urls {
		urls[i] = t.logURL(pending[i].url)
	}

	t.logger.LogAttrs(t.ctx, slog.LevelWarn, "page requests pending",
		slog.String("component", "network"),
		slog.Int("count", len(pending)),
		slog.Any("urls", urls),
	)
}

// logSummary logs the number of failed requests by reason, if more of them
// failed than were logged one by one
func (t *requestTracker) logSummary() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.failed <= t.logged {
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

	t.logger.LogAttrs(t.ctx, slog.LevelWarn, "page requests failed",
		slog.String("component", "network"),
		slog.Int("count", t.failed),
		slog.Int("logged", t.logged),
		slog.Group("reasons", counts...),
	)
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

package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sehrgutesoftware/httpdf/internal/logging"
)

// RenderOpts contains options for rendering a PDF
type RenderOpts struct {
	// The width of the PDF page in mm
	Width float64
	// The height of the PDF page in mm
	Height float64
	// GenerateTaggedPDF indicates whether to generate a tagged PDF, see
	// https://chromedevtools.github.io/devtools-protocol/tot/Page/#method-printToPDF
	GenerateTaggedPDF bool
	// GenerateDocumentOutline indicates whether to generate a document outline,
	// see https://chromedevtools.github.io/devtools-protocol/tot/Page/#method-printToPDF
	// Chromium derives the outline from the tags, so it implies GenerateTaggedPDF.
	GenerateDocumentOutline bool
}

// Renderer is an interface for rendering PDFs from HTML content
type Renderer interface {
	Render(ctx context.Context, url string, pdf io.Writer, opts RenderOpts) error
}

// rodRenderer is a Renderer implementation that uses rod to render PDFs
type rodRenderer struct {
	chromium      string
	creatorPrefix string
	logger        *slog.Logger
}

// NewRodRenderer creates a new rodRenderer
func NewRodRenderer(chromium string, opts ...RodRendererOption) Renderer {
	r := &rodRenderer{
		chromium: chromium,
		logger:   slog.Default(),
	}

	for _, opt := range opts {
		opt(r)
	}

	r.logger = slog.New(logging.NewContextHandler(r.logger.Handler()))

	return r
}

// RodRendererOption is a function that configures the rodRenderer
type RodRendererOption func(*rodRenderer)

// WithLogger sets the logger. Defaults to slog.Default(). The browser's
// stdout and stderr are logged at logging.LevelTrace.
func WithLogger(logger *slog.Logger) RodRendererOption {
	return func(r *rodRenderer) {
		r.logger = logger
	}
}

// WithCreatorPrefix prefixes the Creator in the PDFs' document information,
// separated by a space, e.g. "httpdf/1.0.0" turns "Chromium" into
// "httpdf/1.0.0 Chromium". Prefix must be printable ASCII.
func WithCreatorPrefix(prefix string) RodRendererOption {
	return func(r *rodRenderer) {
		r.creatorPrefix = prefix
	}
}

// Render renders a PDF from HTML content
func (r *rodRenderer) Render(ctx context.Context, url string, pdf io.Writer, opts RenderOpts) error {
	start := time.Now()

	browserLog := io.Discard
	if r.logger.Enabled(ctx, logging.LevelTrace) {
		w := logging.NewLineWriter(ctx, r.logger, logging.LevelTrace, slog.String("component", "chromium"))
		defer w.Close()
		browserLog = w
	}

	// Launch a new browser with default options
	l := launcher.New().
		Context(ctx).
		Bin(r.chromium).
		Logger(browserLog).
		Headless(true).
		// The Dockerfile removes Mesa and LLVM from the image, which is only
		// safe because GPU rendering is disabled here.
		Set("disable-gpu").
		Set("disable-extensions").
		Set("disable-dev-shm-usage").
		Set("disable-software-rasterizer").
		Set("no-sandbox").
		Set("no-zygote")
	controlURL, err := l.Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}
	launched := time.Now()

	// Connect to the browser
	browser := rod.New().Context(ctx).ControlURL(controlURL)
	err = browser.Connect()
	if err != nil {
		r.stopBrowser(ctx, l, nil)
		return fmt.Errorf("failed to connect to browser: %w", err)
	}
	defer r.stopBrowser(ctx, l, browser)

	// Load the HTML content into a new page
	page, err := browser.Page(proto.TargetCreateTarget{
		URL: url,
	})
	if err != nil {
		return fmt.Errorf("failed to create new page: %w", err)
	}
	err = page.WaitStable(50 * time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to wait for page load: %w", err)
	}
	loaded := time.Now()

	// Save the page as a PDF
	width := dumbify(opts.Width)
	height := dumbify(opts.Height)
	margin := 0.0
	pdfStream, err := printToPDF(page, proto.PagePrintToPDF{
		PrintBackground:         true,
		PaperWidth:              &width,
		PaperHeight:             &height,
		MarginTop:               &margin,
		MarginBottom:            &margin,
		MarginLeft:              &margin,
		MarginRight:             &margin,
		PreferCSSPageSize:       true,
		TransferMode:            proto.PagePrintToPDFTransferModeReturnAsStream,
		GenerateTaggedPDF:       opts.GenerateTaggedPDF || opts.GenerateDocumentOutline,
		GenerateDocumentOutline: opts.GenerateDocumentOutline,
	})
	if err != nil {
		return fmt.Errorf("failed generate PDF from HTML: %w", err)
	}

	// Write the PDF to the output stream
	out := pdf
	var stamper *creatorStamper
	if r.creatorPrefix != "" {
		stamper = newCreatorStamper(pdf, r.creatorPrefix)
		out = stamper
	}
	_, err = io.Copy(out, pdfStream)
	pdfStream.Close()
	if err != nil {
		return fmt.Errorf("failed to write PDF to output stream: %w", err)
	}
	if stamper != nil {
		err = stamper.Close()
		if errors.Is(err, errUnstamped) {
			r.logger.WarnContext(ctx, "pdf passed through without creator prefix", "error", err)
		} else if err != nil {
			return fmt.Errorf("failed to write PDF to output stream: %w", err)
		}
	}

	r.logger.LogAttrs(ctx, slog.LevelDebug, "pdf rendered",
		slog.Float64("launch_ms", logging.Millis(launched.Sub(start))),
		slog.Float64("load_ms", logging.Millis(loaded.Sub(launched))),
		slog.Float64("pdf_ms", logging.MillisSince(loaded)),
	)

	return nil
}

// browserExitTimeout is how long the browser may take to close or exit before
// it's killed
const browserExitTimeout = 5 * time.Second

// stopBrowser closes the browser, or kills it if it's not connected or can't
// be closed. Then it waits until the browser has exited and removes its
// profile directory, which would otherwise remain in the temp directory. The
// render's context may be done already, e.g. because the client went away, so
// the browser is closed with a context of its own.
func (r *rodRenderer) stopBrowser(ctx context.Context, l *launcher.Launcher, browser *rod.Browser) {
	if browser == nil {
		l.Kill()
	} else {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), browserExitTimeout)
		defer cancel()
		if err := browser.Context(closeCtx).Close(); err != nil {
			r.logger.WarnContext(ctx, "closing the browser failed, killing it", "error", err)
			l.Kill()
		}
	}

	// Cleanup waits until the browser has exited
	cleanedUp := make(chan struct{})
	go func() {
		l.Cleanup()
		close(cleanedUp)
	}()
	select {
	case <-cleanedUp:
	case <-time.After(browserExitTimeout):
		r.logger.WarnContext(ctx, "browser didn't exit, killing it")
		l.Kill()
		<-cleanedUp
	}
}

// printToPDF is like rod's Page.PDF, but always passes the tagged PDF and
// document outline options to Chromium. rod omits them when they're false,
// which leaves the choice to Chromium, and Chromium tags PDFs by default.
func printToPDF(page *rod.Page, req proto.PagePrintToPDF) (*rod.StreamReader, error) {
	raw, err := page.Call(page.GetContext(), string(page.GetSessionID()), req.ProtoReq(), printToPDFParams(req))
	if err != nil {
		return nil, err
	}

	var res proto.PagePrintToPDFResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode Page.printToPDF result: %w", err)
	}
	return rod.NewStreamReader(page, res.Stream), nil
}

// printToPDFParams returns the parameters of a Page.printToPDF call. The
// fields declared here take precedence over the embedded ones with the same
// JSON names, but aren't omitted when false.
func printToPDFParams(req proto.PagePrintToPDF) any {
	return struct {
		proto.PagePrintToPDF
		GenerateTaggedPDF       bool `json:"generateTaggedPDF"`
		GenerateDocumentOutline bool `json:"generateDocumentOutline"`
	}{req, req.GenerateTaggedPDF, req.GenerateDocumentOutline}
}

// dumbify strips all reason off a distance measure
func dumbify(mm float64) float64 {
	return mm / 25.4
}

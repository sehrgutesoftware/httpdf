package pdf

import (
	"context"
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
	GenerateDocumentOutline bool
}

// Renderer is an interface for rendering PDFs from HTML content
type Renderer interface {
	Render(ctx context.Context, url string, pdf io.Writer, opts RenderOpts) error
}

// rodRenderer is a Renderer implementation that uses rod to render PDFs
type rodRenderer struct {
	chromium string
	logger   *slog.Logger
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
	l, err := launcher.New().
		Context(ctx).
		Bin(r.chromium).
		Logger(browserLog).
		Headless(true).
		Set("disable-gpu").
		Set("disable-extensions").
		Set("disable-dev-shm-usage").
		Set("disable-software-rasterizer").
		Set("no-sandbox").
		Set("no-zygote").
		Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}
	launched := time.Now()

	// Connect to the browser
	browser := rod.New().Context(ctx).ControlURL(l)
	defer browser.Close()
	err = browser.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

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
	pdfStream, err := page.PDF(&proto.PagePrintToPDF{
		PrintBackground:         true,
		PaperWidth:              &width,
		PaperHeight:             &height,
		MarginTop:               &margin,
		MarginBottom:            &margin,
		MarginLeft:              &margin,
		MarginRight:             &margin,
		PreferCSSPageSize:       true,
		TransferMode:            proto.PagePrintToPDFTransferModeReturnAsStream,
		GenerateTaggedPDF:       opts.GenerateTaggedPDF,
		GenerateDocumentOutline: opts.GenerateDocumentOutline,
	})
	if err != nil {
		return fmt.Errorf("failed generate PDF from HTML: %w", err)
	}

	// Write the PDF to the output stream
	_, err = io.Copy(pdf, pdfStream)
	pdfStream.Close()
	if err != nil {
		return fmt.Errorf("failed to write PDF to output stream: %w", err)
	}

	r.logger.LogAttrs(ctx, slog.LevelDebug, "pdf rendered",
		slog.Float64("launch_ms", logging.Millis(launched.Sub(start))),
		slog.Float64("load_ms", logging.Millis(loaded.Sub(launched))),
		slog.Float64("pdf_ms", logging.MillisSince(loaded)),
	)

	return nil
}

// dumbify strips all reason off a distance measure
func dumbify(mm float64) float64 {
	return mm / 25.4
}

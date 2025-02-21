package pdf

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// RenderOpts contains options for rendering a PDF
type RenderOpts struct {
	// The width of the PDF page in mm
	Width float64
	// The height of the PDF page in mm
	Height float64
}

// Renderer is an interface for rendering PDFs from HTML content
type Renderer interface {
	Render(ctx context.Context, html io.Reader, pdf io.Writer, opts RenderOpts) error
}

// rodRenderer is a Renderer implementation that uses rod to render PDFs
type rodRenderer struct {
	chromium string
}

// NewRodRenderer creates a new rodRenderer
func NewRodRenderer(chromium string) Renderer {
	return &rodRenderer{
		chromium: chromium,
	}
}

// Render renders a PDF from HTML content
func (r *rodRenderer) Render(ctx context.Context, html io.Reader, pdf io.Writer, opts RenderOpts) error {
	// Launch a new browser with default options
	l, err := launcher.New().Bin(r.chromium).Context(ctx).Logger(os.Stderr).Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	// Connect to the browser
	browser := rod.New().Context(ctx).ControlURL(l)
	defer browser.Close()
	err = browser.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

	// Load the HTML content into a new page
	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return fmt.Errorf("failed to create new page: %w", err)
	}
	htmlContent, err := io.ReadAll(html)
	if err != nil {
		return fmt.Errorf("failed to read HTML content: %w", err)
	}
	err = page.SetDocumentContent(string(htmlContent))
	if err != nil {
		return fmt.Errorf("failed to set HTML content: %w", err)
	}
	err = page.WaitStable(1000 * time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Save the page as a PDF
	width := dumbify(opts.Width)
	height := dumbify(opts.Height)
	margin := 0.0
	pdfStream, err := page.PDF(&proto.PagePrintToPDF{
		PrintBackground:   true,
		PaperWidth:        &width,
		PaperHeight:       &height,
		MarginTop:         &margin,
		MarginBottom:      &margin,
		MarginLeft:        &margin,
		MarginRight:       &margin,
		PreferCSSPageSize: true,
		TransferMode:      proto.PagePrintToPDFTransferModeReturnAsStream,
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

	return nil
}

// dumbify strips all reason off a distance measure
func dumbify(mm float64) float64 {
	return mm / 25.4
}

package httpdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sehrgutesoftware/httpdf/internal/pdf"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

var (
	// ErrInvalidValues is returned when the values are invalid
	ErrInvalidValues = errors.New("invalid values")
)

// HTTPDF is the interface for the HTTPDF service.
type HTTPDF interface {
	// Generate renders a PDF from the given template and values.
	Generate(template string, values map[string]any, out io.Writer) error
	// Preview renders an HTML preview of the given template with its example values.
	Preview(template string, out io.Writer) error
}

// httPDF is the core implementation of the httPDF service.
type httPDF struct {
	templates    template.Loader
	validator    template.Validator
	htmlRenderer template.Renderer
	pdfRenderer  pdf.Renderer
}

// New creates a new httpdf service.
func New(
	templates template.Loader,
	validator template.Validator,
	htmlRenderer template.Renderer,
	pdfRenderer pdf.Renderer,
) HTTPDF {
	return &httPDF{
		templates:    templates,
		validator:    validator,
		htmlRenderer: htmlRenderer,
		pdfRenderer:  pdfRenderer,
	}
}

// Generate generates a PDF from the given template and values.
func (w *httPDF) Generate(template string, values map[string]any, out io.Writer) error {
	tmpl, err := w.templates.Load(template)
	if err != nil {
		return fmt.Errorf("failed to load template: %w", err)
	}

	if valid := w.validator.Validate(tmpl, values); !valid.Valid {
		return fmt.Errorf("%w: %v", ErrInvalidValues, valid.Errors)
	}

	html := bytes.NewBuffer(nil)
	if err := w.htmlRenderer.Render(tmpl, values, html); err != nil {
		return fmt.Errorf("failed to render HTML: %w", err)
	}

	if err := w.pdfRenderer.Render(context.TODO(), html, out, pdf.RenderOpts{
		Width:  tmpl.Config.Page.Width,
		Height: tmpl.Config.Page.Height,
	}); err != nil {
		return fmt.Errorf("failed to render PDF: %w", err)
	}

	return nil
}

// Preview renders an HTML preview of the given template with its example values.
func (w *httPDF) Preview(template string, out io.Writer) error {
	tmpl, err := w.templates.Load(template)
	if err != nil {
		return fmt.Errorf("failed to load template: %w", err)
	}

	exampleData, err := w.templates.ExampleData(template)
	if err != nil {
		return fmt.Errorf("failed to load example data: %w", err)
	}

	if valid := w.validator.Validate(tmpl, exampleData); !valid.Valid {
		return fmt.Errorf("%w: %v", ErrInvalidValues, valid.Errors)
	}

	if err := w.htmlRenderer.Render(tmpl, exampleData, out); err != nil {
		return fmt.Errorf("failed to render HTML: %w", err)
	}

	return nil
}

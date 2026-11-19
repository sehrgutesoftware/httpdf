package template

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kaptinlin/jsonschema"
)

// Validate reports errors that would otherwise only surface when the template
// is rendered: syntax errors and undefined functions in the template and its
// partials. If the template has example data, the example must match the
// schema and render without errors in every configured locale.
func (t *Template) Validate() error {
	if _, err := t.parse("/assets", ""); err != nil {
		return err
	}

	if t.Example == nil {
		return nil
	}

	if t.Schema != nil {
		if result := t.Schema.Validate(t.Example); !result.Valid {
			if errs := schemaErrors(result, ""); len(errs) > 0 {
				return fmt.Errorf("example data doesn't match the schema:\n%s", strings.Join(errs, "\n"))
			}
			return fmt.Errorf("example data doesn't match the schema: %v", result.Errors)
		}
	}

	locales := []string{""}
	if t.Config.Locale != nil && len(t.Config.Locale.Locales) > 0 {
		locales = t.Config.Locale.Locales
	}

	// The same error usually occurs in every locale, so it's reported once
	// along with all the locales it occurred in.
	type failure struct {
		err     error
		locales []string
	}
	var failures []*failure
	byMessage := make(map[string]*failure)
	for _, locale := range locales {
		err := t.Render(t.Example, "/assets", locale, io.Discard)
		if err == nil {
			continue
		}
		if f, ok := byMessage[err.Error()]; ok {
			f.locales = append(f.locales, locale)
			continue
		}
		f := &failure{err: err, locales: []string{locale}}
		byMessage[err.Error()] = f
		failures = append(failures, f)
	}

	errs := make([]error, 0, len(failures))
	for _, f := range failures {
		if f.locales[0] == "" {
			errs = append(errs, fmt.Errorf("render example data: %w", f.err))
		} else {
			errs = append(errs, fmt.Errorf("render example data (locale %s): %w", strings.Join(f.locales, ", "), f.err))
		}
	}

	return errors.Join(errs...)
}

// schemaErrors lists the errors of a failed schema validation, along with the
// location of the offending value. The top-level errors alone only say which
// property is invalid, not why. The instance locations of nested results are
// relative to their parent's, hence the parent location is passed along.
func schemaErrors(result *jsonschema.EvaluationResult, parent string) []string {
	if result.Valid {
		return nil
	}

	location := parent + result.InstanceLocation
	invalidDetails := slices.ContainsFunc(result.Details, func(d *jsonschema.EvaluationResult) bool {
		return !d.Valid
	})

	var errs []string
	for _, err := range result.Errors {
		// These only summarize the errors of the nested results
		if invalidDetails && slices.Contains([]string{"properties", "items", "prefixItems", "$ref"}, err.Keyword) {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s: %s", cmp.Or(location, "/"), err.Error()))
	}
	slices.Sort(errs)

	for _, detail := range result.Details {
		errs = append(errs, schemaErrors(detail, location)...)
	}

	return errs
}

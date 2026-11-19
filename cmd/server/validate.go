package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

// validationResult is the outcome of validating a single template
type validationResult struct {
	name string
	err  error
}

// validate checks the named templates in dir, or all of them if no names are
// given, and writes a report to out. It returns the process exit code.
func validate(out io.Writer, dir string, names []string) int {
	results, err := validateTemplates(dir, names)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}

	valid := 0
	for _, result := range results {
		if result.err == nil {
			valid++
			fmt.Fprintf(out, "ok    %s\n", result.name)
			continue
		}

		fmt.Fprintf(out, "FAIL  %s\n", result.name)
		for line := range strings.SplitSeq(result.err.Error(), "\n") {
			fmt.Fprintf(out, "      %s\n", line)
		}
	}

	fmt.Fprintf(out, "%d of %d templates valid\n", valid, len(results))
	if valid < len(results) {
		return 1
	}
	return 0
}

// warnInvalidTemplates validates all templates in dir and logs a warning for
// each invalid one. It doesn't stop the server, because templates that fail
// the strict checks may still render.
func warnInvalidTemplates(logger *slog.Logger, dir string) {
	results, err := validateTemplates(dir, nil)
	if err != nil {
		logger.Warn("templates not validated", "error", err)
		return
	}

	invalid := 0
	for _, result := range results {
		if result.err != nil {
			invalid++
			logger.Warn("template invalid", "template", result.name, "error", result.err)
		}
	}

	level := slog.LevelInfo
	if invalid > 0 {
		level = slog.LevelWarn
	}
	logger.Log(context.Background(), level, "templates validated", "total", len(results), "invalid", invalid)
}

// validateTemplates checks the named templates in dir, or all of them if no
// names are given. It fails if the templates can't be listed or there are none.
func validateTemplates(dir string, names []string) ([]validationResult, error) {
	root := subdirfs.New(dir)

	if len(names) == 0 {
		var err error
		names, err = templateNames(root)
		if err != nil {
			return nil, err
		}
		if len(names) == 0 {
			// Most likely, the templates were copied to the wrong directory
			abs, _ := filepath.Abs(dir)
			return nil, fmt.Errorf("no templates found in %s", abs)
		}
	}

	loader := template.NewFSLoader(root, template.WithStrict())
	results := make([]validationResult, len(names))
	for i, name := range names {
		results[i] = validationResult{name: name, err: validateTemplate(loader, name)}
	}
	return results, nil
}

func validateTemplate(loader template.Loader, name string) error {
	t, err := loader.Load(name)
	if err != nil {
		return err
	}
	return t.Validate()
}

// templateNames lists the template directories in root, skipping hidden ones
func templateNames(root fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, fmt.Errorf("read templates directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

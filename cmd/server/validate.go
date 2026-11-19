package main

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/sehrgutesoftware/httpdf/internal/subdirfs"
	"github.com/sehrgutesoftware/httpdf/internal/template"
)

// validate checks the named templates in dir, or all of them if no names are
// given, and writes a report to out. It returns the process exit code.
func validate(out io.Writer, dir string, names []string) int {
	root := subdirfs.New(dir)

	if len(names) == 0 {
		var err error
		names, err = templateNames(root)
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1
		}
		if len(names) == 0 {
			// Most likely, the templates were copied to the wrong directory
			abs, _ := filepath.Abs(dir)
			fmt.Fprintf(out, "error: no templates found in %s\n", abs)
			return 1
		}
	}

	loader := template.NewFSLoader(root, template.WithStrict())
	valid := 0
	for _, name := range names {
		err := validateTemplate(loader, name)
		if err == nil {
			valid++
			fmt.Fprintf(out, "ok    %s\n", name)
			continue
		}

		fmt.Fprintf(out, "FAIL  %s\n", name)
		for line := range strings.SplitSeq(err.Error(), "\n") {
			fmt.Fprintf(out, "      %s\n", line)
		}
	}

	fmt.Fprintf(out, "%d of %d templates valid\n", valid, len(names))
	if valid < len(names) {
		return 1
	}
	return 0
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

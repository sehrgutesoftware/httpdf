package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTemplate creates a valid template in dir. The given files are added
// to it or replace the defaults.
func writeTemplate(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()
	all := map[string]string{
		"template.html": `Hello {{.name}}`,
		"schema.json":   `{"type": "object", "properties": {"name": {"type": "string"}}}`,
		"config.yaml":   "page:\n  width: 210\n  height: 297\n",
		"example.json":  `{"name": "World"}`,
	}
	for file, content := range files {
		all[file] = content
	}

	for file, content := range all {
		path := filepath.Join(dir, name, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

func TestValidate(t *testing.T) {
	t.Run("it_succeeds_when_all_templates_are_valid", func(t *testing.T) {
		dir := t.TempDir()
		writeTemplate(t, dir, "invoice", nil)
		writeTemplate(t, dir, "label", map[string]string{
			"template.html":        `{{template "header.html" .}}`,
			"partials/header.html": `<h1>{{.name}}</h1>`,
		})
		var out bytes.Buffer

		code := validate(&out, dir, nil)

		assert.Equal(t, 0, code)
		assert.Equal(t, "ok    invoice\nok    label\n2 of 2 templates valid\n", out.String())
	})

	t.Run("it_fails_and_reports_all_invalid_templates", func(t *testing.T) {
		dir := t.TempDir()
		writeTemplate(t, dir, "bad-config", map[string]string{"config.yaml": "page:\n  width: 210\n  heigth: 297\n"})
		writeTemplate(t, dir, "bad-syntax", map[string]string{"template.html": "{{if .name}}"})
		writeTemplate(t, dir, "valid", nil)
		var out bytes.Buffer

		code := validate(&out, dir, nil)

		assert.Equal(t, 1, code)
		assert.Equal(t, `FAIL  bad-config
      decode config file: yaml: unmarshal errors:
        line 3: unknown key "heigth"
FAIL  bad-syntax
      parse template: template: template.html:1: unexpected EOF
ok    valid
1 of 3 templates valid
`, out.String())
	})

	t.Run("it_only_validates_the_named_templates", func(t *testing.T) {
		dir := t.TempDir()
		writeTemplate(t, dir, "invoice", nil)
		writeTemplate(t, dir, "broken", map[string]string{"template.html": "{{if .name}}"})
		var out bytes.Buffer

		code := validate(&out, dir, []string{"invoice", "missing"})

		assert.Equal(t, 1, code)
		assert.Equal(t, `ok    invoice
FAIL  missing
      template not found: missing missing/template.html
1 of 2 templates valid
`, out.String())
	})

	t.Run("it_skips_hidden_directories_and_files", func(t *testing.T) {
		dir := t.TempDir()
		writeTemplate(t, dir, "invoice", nil)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("docs"), 0o644))
		var out bytes.Buffer

		code := validate(&out, dir, nil)

		assert.Equal(t, 0, code)
		assert.Equal(t, "ok    invoice\n1 of 1 templates valid\n", out.String())
	})

	t.Run("it_fails_when_there_are_no_templates", func(t *testing.T) {
		dir := t.TempDir()
		var out bytes.Buffer

		code := validate(&out, dir, nil)

		assert.Equal(t, 1, code)
		assert.Equal(t, "error: no templates found in "+dir+"\n", out.String())
	})

	t.Run("it_fails_when_the_templates_directory_doesnt_exist", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing")
		var out bytes.Buffer

		code := validate(&out, dir, nil)

		assert.Equal(t, 1, code)
		assert.Contains(t, out.String(), "error: read templates directory:")
	})
}

func TestWarnInvalidTemplates(t *testing.T) {
	// logRecords runs warnInvalidTemplates on dir and returns the log records
	logRecords := func(t *testing.T, dir string) []map[string]any {
		t.Helper()
		var buf bytes.Buffer
		warnInvalidTemplates(newLogger(&buf, "json", slog.LevelInfo), dir)

		var records []map[string]any
		for line := range bytes.Lines(buf.Bytes()) {
			var record map[string]any
			require.NoError(t, json.Unmarshal(line, &record))
			delete(record, "time")
			records = append(records, record)
		}
		return records
	}

	t.Run("it_logs_a_summary_when_all_templates_are_valid", func(t *testing.T) {
		dir := t.TempDir()
		writeTemplate(t, dir, "invoice", nil)
		writeTemplate(t, dir, "label", nil)

		records := logRecords(t, dir)

		assert.Equal(t, []map[string]any{
			{"level": "INFO", "msg": "templates validated", "total": 2.0, "invalid": 0.0},
		}, records)
	})

	t.Run("it_warns_about_each_invalid_template", func(t *testing.T) {
		dir := t.TempDir()
		writeTemplate(t, dir, "bad-config", map[string]string{"config.yaml": "page: {width: 210}\n"})
		writeTemplate(t, dir, "bad-syntax", map[string]string{"template.html": "{{if .name}}"})
		writeTemplate(t, dir, "valid", nil)

		records := logRecords(t, dir)

		assert.Equal(t, []map[string]any{
			{
				"level":    "WARN",
				"msg":      "template invalid",
				"template": "bad-config",
				"error":    "invalid config: page.height must be greater than 0",
			},
			{
				"level":    "WARN",
				"msg":      "template invalid",
				"template": "bad-syntax",
				"error":    "parse template: template: template.html:1: unexpected EOF",
			},
			{"level": "WARN", "msg": "templates validated", "total": 3.0, "invalid": 2.0},
		}, records)
	})

	t.Run("it_warns_when_there_are_no_templates", func(t *testing.T) {
		dir := t.TempDir()

		records := logRecords(t, dir)

		assert.Equal(t, []map[string]any{
			{"level": "WARN", "msg": "templates not validated", "error": "no templates found in " + dir},
		}, records)
	})
}

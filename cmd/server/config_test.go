package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateEnv clears the HTTPDF_* variables of the environment the tests run in.
// Empty variables are ignored like unset ones.
func isolateEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, envPrefix) {
			t.Setenv(name, "")
		}
	}
}

// writeConfigFile writes a config file and returns its path
func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "httpdf.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestDefaultConfig(t *testing.T) {
	t.Run("it_is_valid", func(t *testing.T) {
		cfg := DefaultConfig()

		assert.NoError(t, cfg.Validate())
		assert.Equal(t, ":8080", cfg.Listen)
		assert.Equal(t, "templates", cfg.Templates)
		assert.Equal(t, "/usr/bin/chromium-headless-shell", cfg.Chromium)
		assert.Equal(t, LogConfig{Format: "json", Level: "info"}, cfg.Log)
		assert.Equal(t, LimitsConfig{
			MaxRequestMB:         10,
			MaxConcurrentRenders: httpdf.DefaultMaxConcurrentRenders(),
			RenderTimeout:        20 * time.Second,
		}, cfg.Limits)
	})
}

func TestConfig_LoadFile(t *testing.T) {
	t.Run("it_overrides_only_the_values_set_in_the_file", func(t *testing.T) {
		path := writeConfigFile(t, `
listen: ":9000"
log:
  level: debug
limits:
  renderTimeout: 45s
`)
		cfg := DefaultConfig()

		require.NoError(t, cfg.LoadFile(path))

		want := DefaultConfig()
		want.Listen = ":9000"
		want.Log.Level = "debug"
		want.Limits.RenderTimeout = 45 * time.Second
		assert.Equal(t, want, cfg)
	})

	t.Run("it_keeps_the_defaults_for_a_file_without_settings", func(t *testing.T) {
		files := map[string]string{
			"empty":         "",
			"comments_only": "# listen: \":9000\"\n# log:\n#   level: debug\n",
		}

		for name, content := range files {
			t.Run(name, func(t *testing.T) {
				path := writeConfigFile(t, content)
				cfg := DefaultConfig()

				require.NoError(t, cfg.LoadFile(path))

				assert.Equal(t, DefaultConfig(), cfg)
			})
		}
	})

	t.Run("it_rejects_unknown_keys", func(t *testing.T) {
		path := writeConfigFile(t, "limits:\n  renderTimeOut: 5s\n")
		cfg := DefaultConfig()

		err := cfg.LoadFile(path)

		assert.ErrorContains(t, err, "line 2: field renderTimeOut not found")
	})

	t.Run("it_fails_if_the_file_doesnt_exist", func(t *testing.T) {
		cfg := DefaultConfig()

		err := cfg.LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))

		assert.ErrorContains(t, err, "config file:")
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestConfig_LoadEnv(t *testing.T) {
	t.Run("it_overrides_only_the_values_set_in_the_environment", func(t *testing.T) {
		isolateEnv(t)
		t.Setenv("HTTPDF_LISTEN", ":9000")
		t.Setenv("HTTPDF_LOG_FORMAT", "text")
		t.Setenv("HTTPDF_MAX_REQUEST_MB", "5")
		t.Setenv("HTTPDF_RENDER_TIMEOUT", "1m")
		cfg := DefaultConfig()

		require.NoError(t, cfg.LoadEnv())

		want := DefaultConfig()
		want.Listen = ":9000"
		want.Log.Format = "text"
		want.Limits.MaxRequestMB = 5
		want.Limits.RenderTimeout = time.Minute
		assert.Equal(t, want, cfg)
	})

	t.Run("it_names_the_variable_in_parse_errors", func(t *testing.T) {
		isolateEnv(t)
		t.Setenv("HTTPDF_RENDER_TIMEOUT", "20")
		t.Setenv("HTTPDF_MAX_CONCURRENT_RENDERS", "many")
		cfg := DefaultConfig()

		err := cfg.LoadEnv()

		assert.ErrorContains(t, err, `HTTPDF_RENDER_TIMEOUT: unable to parse duration: time: missing unit in duration "20"`)
		assert.ErrorContains(t, err, "HTTPDF_MAX_CONCURRENT_RENDERS: ")
		assert.NotContains(t, err.Error(), "RenderTimeout")
	})
}

func TestEnvNames(t *testing.T) {
	t.Run("it_maps_every_field_to_its_variable", func(t *testing.T) {
		assert.Equal(t, map[string]string{
			"Listen":               "HTTPDF_LISTEN",
			"Templates":            "HTTPDF_TEMPLATES",
			"Chromium":             "HTTPDF_CHROMIUM",
			"Format":               "HTTPDF_LOG_FORMAT",
			"Level":                "HTTPDF_LOG_LEVEL",
			"MaxRequestMB":         "HTTPDF_MAX_REQUEST_MB",
			"MaxConcurrentRenders": "HTTPDF_MAX_CONCURRENT_RENDERS",
			"RenderTimeout":        "HTTPDF_RENDER_TIMEOUT",
		}, envNames(reflect.TypeFor[Config](), envPrefix))
	})
}

func TestConfig_Validate(t *testing.T) {
	cases := map[string]struct {
		modify func(*Config)
		err    string
	}{
		"empty_listen_address": {
			modify: func(c *Config) { c.Listen = "" },
			err:    "listen (HTTPDF_LISTEN) must not be empty",
		},
		"empty_templates_directory": {
			modify: func(c *Config) { c.Templates = "" },
			err:    "templates (HTTPDF_TEMPLATES) must not be empty",
		},
		"empty_chromium_path": {
			modify: func(c *Config) { c.Chromium = "" },
			err:    "chromium (HTTPDF_CHROMIUM) must not be empty",
		},
		"unknown_log_format": {
			modify: func(c *Config) { c.Log.Format = "xml" },
			err:    `log.format (HTTPDF_LOG_FORMAT) must be json or text, got "xml"`,
		},
		"unknown_log_level": {
			modify: func(c *Config) { c.Log.Level = "verbose" },
			err:    `log.level (HTTPDF_LOG_LEVEL) must be trace, debug, info, warn or error, got "verbose"`,
		},
		"zero_request_size": {
			modify: func(c *Config) { c.Limits.MaxRequestMB = 0 },
			err:    "limits.maxRequestMB (HTTPDF_MAX_REQUEST_MB) must be greater than 0, got 0",
		},
		"negative_concurrency": {
			modify: func(c *Config) { c.Limits.MaxConcurrentRenders = -1 },
			err:    "limits.maxConcurrentRenders (HTTPDF_MAX_CONCURRENT_RENDERS) must be greater than 0, got -1",
		},
		"zero_render_timeout": {
			modify: func(c *Config) { c.Limits.RenderTimeout = 0 },
			err:    "limits.renderTimeout (HTTPDF_RENDER_TIMEOUT) must be greater than 0, got 0s",
		},
	}

	for name, tc := range cases {
		t.Run("it_rejects_"+name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.modify(&cfg)

			assert.EqualError(t, cfg.Validate(), tc.err)
		})
	}

	t.Run("it_reports_all_invalid_values", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Listen = ""
		cfg.Limits.MaxRequestMB = 0

		err := cfg.Validate()

		assert.ErrorContains(t, err, "listen (HTTPDF_LISTEN)")
		assert.ErrorContains(t, err, "limits.maxRequestMB (HTTPDF_MAX_REQUEST_MB)")
	})

	t.Run("it_accepts_log_settings_regardless_of_case_and_the_trace_level", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Log = LogConfig{Format: "TEXT", Level: "Trace"}

		assert.NoError(t, cfg.Validate())
	})
}

func TestCommand(t *testing.T) {
	// run runs the command line interface with the given arguments and
	// returns the config the server would have been started with
	run := func(t *testing.T, args ...string) (cfg Config, configFile string, out string, err error) {
		t.Helper()
		var buf bytes.Buffer
		cmd := newCommand(&buf, func(ctx context.Context, c Config, f string) error {
			cfg, configFile = c, f
			return nil
		})
		err = cmd.Run(t.Context(), append([]string{"httpdf-server"}, args...))
		return cfg, configFile, buf.String(), err
	}

	t.Run("it_runs_the_server_with_the_defaults", func(t *testing.T) {
		isolateEnv(t)

		cfg, configFile, _, err := run(t)

		require.NoError(t, err)
		assert.Equal(t, DefaultConfig(), cfg)
		assert.Empty(t, configFile)
	})

	t.Run("it_layers_defaults_file_environment_and_flags", func(t *testing.T) {
		isolateEnv(t)
		path := writeConfigFile(t, `
listen: ":1000"
templates: from-file
chromium: /from/file
limits:
  maxRequestMB: 5
  renderTimeout: 30s
`)
		t.Setenv("HTTPDF_LISTEN", ":2000")
		t.Setenv("HTTPDF_TEMPLATES", "from-env")
		t.Setenv("HTTPDF_RENDER_TIMEOUT", "40s")

		cfg, configFile, _, err := run(t, "--config", path, "--listen", ":3000")

		require.NoError(t, err)
		assert.Equal(t, path, configFile)
		assert.Equal(t, ":3000", cfg.Listen, "flag over environment")
		assert.Equal(t, "from-env", cfg.Templates, "environment over file")
		assert.Equal(t, "/from/file", cfg.Chromium, "file over default")
		assert.Equal(t, 5, cfg.Limits.MaxRequestMB, "file over default")
		assert.Equal(t, 40*time.Second, cfg.Limits.RenderTimeout, "environment over file")
		assert.Equal(t, "json", cfg.Log.Format, "default")
	})

	t.Run("it_reads_the_config_file_named_by_the_environment", func(t *testing.T) {
		isolateEnv(t)
		path := writeConfigFile(t, `listen: ":1000"`)
		t.Setenv("HTTPDF_CONFIG", path)

		cfg, configFile, _, err := run(t)

		require.NoError(t, err)
		assert.Equal(t, path, configFile)
		assert.Equal(t, ":1000", cfg.Listen)
	})

	t.Run("it_doesnt_let_flag_defaults_override_the_environment", func(t *testing.T) {
		isolateEnv(t)
		t.Setenv("HTTPDF_LISTEN", ":2000")
		t.Setenv("HTTPDF_CHROMIUM", "/from/env")

		cfg, _, _, err := run(t)

		require.NoError(t, err)
		assert.Equal(t, ":2000", cfg.Listen)
		assert.Equal(t, "/from/env", cfg.Chromium)
	})

	t.Run("it_accepts_short_flags", func(t *testing.T) {
		isolateEnv(t)

		cfg, _, _, err := run(t, "-l", ":3000", "-t", "elsewhere")

		require.NoError(t, err)
		assert.Equal(t, ":3000", cfg.Listen)
		assert.Equal(t, "elsewhere", cfg.Templates)
	})

	t.Run("it_doesnt_run_the_server_with_an_invalid_config", func(t *testing.T) {
		isolateEnv(t)
		t.Setenv("HTTPDF_MAX_REQUEST_MB", "0")

		cfg, _, _, err := run(t)

		assert.EqualError(t, err, "invalid configuration: limits.maxRequestMB (HTTPDF_MAX_REQUEST_MB) must be greater than 0, got 0")
		assert.Equal(t, Config{}, cfg, "the server must not run")
	})

	t.Run("it_fails_if_the_named_config_file_doesnt_exist", func(t *testing.T) {
		isolateEnv(t)

		_, _, _, err := run(t, "--config", filepath.Join(t.TempDir(), "missing.yaml"))

		assert.ErrorContains(t, err, "invalid configuration: config file:")
	})

	t.Run("it_rejects_unexpected_arguments", func(t *testing.T) {
		isolateEnv(t)

		_, _, _, err := run(t, "valdiate")

		assert.EqualError(t, err, `unexpected argument "valdiate", see --help`)
	})

	t.Run("it_returns_usage_errors_instead_of_printing_them", func(t *testing.T) {
		isolateEnv(t)

		_, _, out, err := run(t, "--listn", ":3000")

		assert.EqualError(t, err, "flag provided but not defined: -listn, see --help")
		assert.Empty(t, out)
	})

	t.Run("it_returns_usage_errors_of_the_validate_command", func(t *testing.T) {
		isolateEnv(t)

		_, _, out, err := run(t, "validate", "--strict")

		assert.EqualError(t, err, "flag provided but not defined: -strict, see --help")
		assert.Empty(t, out)
	})

	t.Run("it_prints_the_version", func(t *testing.T) {
		isolateEnv(t)

		_, _, out, err := run(t, "--version")

		require.NoError(t, err)
		assert.Contains(t, out, "httpdf-server version "+version)
	})

	t.Run("it_validates_the_templates_in_the_directory_given_by_flag", func(t *testing.T) {
		isolateEnv(t)
		dir := t.TempDir()
		writeTemplate(t, dir, "invoice", nil)

		_, _, out, err := run(t, "validate", "--templates", dir)

		require.NoError(t, err)
		assert.Equal(t, "ok    invoice\n1 of 1 templates valid\n", out)
	})

	t.Run("it_validates_only_the_named_templates", func(t *testing.T) {
		isolateEnv(t)
		dir := t.TempDir()
		writeTemplate(t, dir, "invoice", nil)
		writeTemplate(t, dir, "broken", map[string]string{"template.html": "{{if .name}}"})

		_, _, out, err := run(t, "--templates", dir, "validate", "invoice")

		require.NoError(t, err)
		assert.Equal(t, "ok    invoice\n1 of 1 templates valid\n", out)
	})

	t.Run("it_fails_if_validation_fails", func(t *testing.T) {
		isolateEnv(t)
		dir := t.TempDir()
		writeTemplate(t, dir, "broken", map[string]string{"template.html": "{{if .name}}"})

		_, _, out, err := run(t, "validate", "--templates", dir)

		assert.ErrorIs(t, err, errReported)
		assert.Contains(t, out, "FAIL  broken")
	})
}

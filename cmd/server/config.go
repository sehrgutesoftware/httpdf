package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/sehrgutesoftware/httpdf"
	"github.com/sehrgutesoftware/httpdf/internal/logging"
	"gopkg.in/yaml.v3"
)

// envPrefix is prepended to the names of all environment variables
const envPrefix = "HTTPDF_"

// Config of the httpdf server. Its values come from DefaultConfig, an optional
// config file, environment variables and command line flags, each overriding
// the previous.
type Config struct {
	// Listen is the address the server listens on
	Listen string `yaml:"listen" env:"LISTEN"`
	// Templates is the directory containing the templates
	Templates string `yaml:"templates" env:"TEMPLATES"`
	// Chromium is the path of the Chromium binary used for rendering
	Chromium string       `yaml:"chromium" env:"CHROMIUM"`
	Log      LogConfig    `yaml:"log" envPrefix:"LOG_"`
	Limits   LimitsConfig `yaml:"limits"`
}

// LogConfig configures the logs written to stdout
type LogConfig struct {
	// Format is json or text
	Format string `yaml:"format" env:"FORMAT"`
	// Level is trace, debug, info, warn or error
	Level string `yaml:"level" env:"LEVEL"`
}

// LimitsConfig limits the resources a render may use
type LimitsConfig struct {
	// MaxRequestMB is the maximum size of a render request's body
	MaxRequestMB int `yaml:"maxRequestMB" env:"MAX_REQUEST_MB"`
	// MaxConcurrentRenders is the number of renders running at the same time
	MaxConcurrentRenders int `yaml:"maxConcurrentRenders" env:"MAX_CONCURRENT_RENDERS"`
	// RenderTimeout limits a render, including the wait for a free slot
	RenderTimeout time.Duration `yaml:"renderTimeout" env:"RENDER_TIMEOUT"`
}

// DefaultConfig returns the config used where neither the config file, the
// environment nor the command line set a value.
func DefaultConfig() Config {
	return Config{
		Listen:    ":8080",
		Templates: "templates",
		Chromium:  "/usr/bin/chromium-headless-shell",
		Log: LogConfig{
			Format: "json",
			Level:  "info",
		},
		Limits: LimitsConfig{
			MaxRequestMB:         httpdf.DefaultMaxRequestSize >> 20,
			MaxConcurrentRenders: httpdf.DefaultMaxConcurrentRenders(),
			RenderTimeout:        httpdf.DefaultRenderTimeout,
		},
	}
}

// LoadFile reads the config file at path over the config. Unknown keys are
// rejected, so that typos don't go unnoticed. An empty file, or one with only
// comments, sets nothing.
func (c *Config) LoadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("config file: %w", err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	// The decoder reports a file without a YAML document as io.EOF
	if err := decoder.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	return nil
}

// LoadEnv reads the environment variables over the config. Errors name the
// variable, not the field of the config.
func (c *Config) LoadEnv() error {
	err := env.ParseWithOptions(c, env.Options{Prefix: envPrefix})

	var aggregate env.AggregateError
	if !errors.As(err, &aggregate) {
		return err
	}
	names := envNames(reflect.TypeFor[Config](), envPrefix)
	errs := make([]error, len(aggregate.Errors))
	for i, err := range aggregate.Errors {
		var parseErr env.ParseError
		if errors.As(err, &parseErr) {
			if name, ok := names[parseErr.Name]; ok {
				err = fmt.Errorf("%s: %w", name, parseErr.Err)
			}
		}
		errs[i] = err
	}
	return errors.Join(errs...)
}

// envNames maps the names of the config's fields to their environment
// variables. The config's field names are unique.
func envNames(t reflect.Type, prefix string) map[string]string {
	names := make(map[string]string)
	for field := range t.Fields() {
		if field.Type.Kind() == reflect.Struct {
			for name, variable := range envNames(field.Type, prefix+field.Tag.Get("envPrefix")) {
				names[name] = variable
			}
			continue
		}
		if variable, _, _ := strings.Cut(field.Tag.Get("env"), ","); variable != "" {
			names[field.Name] = prefix + variable
		}
	}
	return names
}

// Validate reports all invalid values. It names the config file's key and the
// environment variable, because the value might come from either.
func (c Config) Validate() error {
	var errs []error
	invalid := func(key, variable, msg string, args ...any) {
		errs = append(errs, fmt.Errorf("%s (%s%s) %s", key, envPrefix, variable, fmt.Sprintf(msg, args...)))
	}

	if c.Listen == "" {
		invalid("listen", "LISTEN", "must not be empty")
	}
	if c.Templates == "" {
		invalid("templates", "TEMPLATES", "must not be empty")
	}
	if c.Chromium == "" {
		invalid("chromium", "CHROMIUM", "must not be empty")
	}
	if f := strings.ToLower(c.Log.Format); f != "json" && f != "text" {
		invalid("log.format", "LOG_FORMAT", "must be json or text, got %q", c.Log.Format)
	}
	if _, err := logging.ParseLevel(c.Log.Level); err != nil {
		invalid("log.level", "LOG_LEVEL", "must be trace, debug, info, warn or error, got %q", c.Log.Level)
	}
	if c.Limits.MaxRequestMB <= 0 {
		invalid("limits.maxRequestMB", "MAX_REQUEST_MB", "must be greater than 0, got %d", c.Limits.MaxRequestMB)
	}
	if c.Limits.MaxConcurrentRenders <= 0 {
		invalid("limits.maxConcurrentRenders", "MAX_CONCURRENT_RENDERS", "must be greater than 0, got %d", c.Limits.MaxConcurrentRenders)
	}
	if c.Limits.RenderTimeout <= 0 {
		invalid("limits.renderTimeout", "RENDER_TIMEOUT", "must be greater than 0, got %s", c.Limits.RenderTimeout)
	}

	return errors.Join(errs...)
}

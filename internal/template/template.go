package template

import (
	"io"

	"github.com/kaptinlin/jsonschema"
)

// Config represents the configuration of a template
type Config struct {
	Page struct {
		Width  float64 `yaml:"width"`
		Height float64 `yaml:"height"`
	} `yaml:"page"`
}

// Template represents a template
type Template struct {
	Content io.ReadCloser
	Config  Config
	Schema  *jsonschema.Schema
}

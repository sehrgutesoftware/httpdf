package template

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"

	"github.com/kaptinlin/jsonschema"
	yaml "gopkg.in/yaml.v3"
)

var (
	// ErrTemplateNotFound is returned when a template is not found
	ErrTemplateNotFound = errors.New("template not found")
)

// Loader allows access to templates
type Loader interface {
	// Load loads a template by name
	Load(name string) (*Template, error)
}

// fsLoader is a Loader implementation that loads templates from a filesystem.
//
// The template is expected to be structured as follows:
// - dir/: Directory named after the template
// - dir/template.html: The actual template file
// - dir/config.yaml: The configuration file
// - dir/schema.json: The JSON schema file
// - dir/assets: (optional) directory containing static assets
type fsLoader struct {
	root   fs.SubFS
	schema *jsonschema.Compiler
}

// NewFSLoader creates a new fsLoader
func NewFSLoader(root fs.SubFS) Loader {
	return &fsLoader{
		root:   root,
		schema: jsonschema.NewCompiler(),
	}
}

// Load loads a template from the filesystem
func (l *fsLoader) Load(name string) (*Template, error) {
	contentPath := path.Join(name, "template.html")
	configPath := path.Join(name, "config.yaml")
	schemaPath := path.Join(name, "schema.json")
	assetsPath := path.Join(name, "assets")
	examplePath := path.Join(name, "example.json")

	// Ensure that all required files exist. Possible TOCTOU issue here, but
	// errors later on will still be handled – though the error message will
	// be different (i.e. will not return ErrTemplateNotFound)
	for _, p := range []string{contentPath, configPath, schemaPath} {
		_, err := fs.Stat(l.root, p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: missing %s", ErrTemplateNotFound, p)
		} else if err != nil {
			return nil, fmt.Errorf("could not stat file: %w", err)
		}
	}

	var err error
	tmpl := &Template{}

	// Load the config
	configFile, err := l.root.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}
	defer configFile.Close()
	err = yaml.NewDecoder(configFile).Decode(&tmpl.Config)
	if err != nil {
		return nil, fmt.Errorf("could not decode config file: %w", err)
	}

	// Load the JSON schema
	schemaFile, err := l.root.Open(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("could not open schema file: %w", err)
	}
	defer schemaFile.Close()
	schemaContent, err := io.ReadAll(schemaFile)
	if err != nil {
		return nil, fmt.Errorf("could not read schema file: %w", err)
	}
	tmpl.Schema, err = l.schema.Compile(schemaContent)
	if err != nil {
		return nil, fmt.Errorf("could not compile schema: %w", err)
	}

	// Load template contents
	fh, err := l.root.Open(contentPath)
	if err != nil {
		return nil, fmt.Errorf("could not open template file: %w", err)
	}
	defer fh.Close()
	tmpl.ReadFrom(fh)

	// Load assets if they exist
	if stat, err := fs.Stat(l.root, assetsPath); err == nil && stat.IsDir() {
		tmpl.Assets, err = l.root.Sub(assetsPath)
		if err != nil {
			return nil, fmt.Errorf("could not load assets: %w", err)
		}
	}

	// Load example data if it exists
	fd, err := l.root.Open(examplePath)
	if errors.Is(err, fs.ErrNotExist) {
		tmpl.Example = nil // No example data available
	} else if err != nil {
		return nil, fmt.Errorf("could not open example data file: %w", err)
	} else {
		defer fd.Close()
		if err := json.NewDecoder(fd).Decode(&tmpl.Example); err != nil {
			return nil, fmt.Errorf("could not decode example data file: %w", err)
		}
	}

	return tmpl, nil
}

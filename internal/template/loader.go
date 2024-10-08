package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sync"

	"github.com/kaptinlin/jsonschema"
	yaml "gopkg.in/yaml.v3"
)

var (
	// ErrTemplateNotFound is returned when a template is not found
	ErrTemplateNotFound = errors.New("template not found")
	// ErrNoExampleData is returned when no example data is found
	ErrNoExampleData = errors.New("no example data found")
)

// Loader allows access to templates
type Loader interface {
	// Load loads a template by name
	Load(name string) (*Template, error)
	// ExampleData loads the example data for a template
	ExampleData(name string) (map[string]any, error)
}

// fsLoader is a Loader implementation that loads templates from a filesystem.
//
// The template is expected to be structured as follows:
// - dir/: Directory named after the template
// - dir/template.html: The actual template file
// - dir/config.yaml: The configuration file
// - dir/schema.json: The JSON schema file
type fsLoader struct {
	root   fs.FS
	schema *jsonschema.Compiler
}

// NewFSLoader creates a new fsLoader
func NewFSLoader(root fs.FS) Loader {
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

	// Open the template file at the very end because otherwise we would have
	// to close it when an error occurs later on in this function
	tmpl.Content, err = l.root.Open(contentPath)
	if err != nil {
		return nil, fmt.Errorf("could not open template file: %w", err)
	}

	return tmpl, nil
}

// ExampleData loads the example data for a template, if it exists
func (l *fsLoader) ExampleData(name string) (map[string]any, error) {
	dataPath := path.Join(name, "example.json")
	fd, err := l.root.Open(dataPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoExampleData
	} else if err != nil {
		return nil, fmt.Errorf("could not open example data file: %w", err)
	}
	defer fd.Close()

	var data map[string]any
	if err := json.NewDecoder(fd).Decode(&data); err != nil {
		return nil, fmt.Errorf("could not decode example data file: %w", err)
	}

	return data, nil
}

// cachingFSLoader is a Loader implementation that caches all templates in memory.
type cachingFSLoader struct {
	fsLoader
	cache map[string]*Template
}

// NewCachingFSLoader creates a new cachingFSLoader
func NewCachingFSLoader(root fs.FS) Loader {
	return &cachingFSLoader{
		fsLoader: fsLoader{
			root:   root,
			schema: jsonschema.NewCompiler(),
		},
		cache: make(map[string]*Template),
	}
}

func (l *cachingFSLoader) Load(name string) (*Template, error) {
	if tmpl, ok := l.cache[name]; ok {
		return tmpl, nil
	}

	tmpl, err := l.fsLoader.Load(name)
	if err != nil {
		return nil, err
	}

	// Copy the content of the template into a buffer so that the file can be
	// closed and the content can be read multiple times
	var buf mutexBuffer
	_, err = io.Copy(&buf, tmpl.Content)
	if err != nil {
		return nil, fmt.Errorf("could not read template content: %w", err)
	}
	tmpl.Content = io.NopCloser(&buf)

	l.cache[name] = tmpl
	return tmpl, nil
}

type mutexBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *mutexBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *mutexBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Read(p)
}

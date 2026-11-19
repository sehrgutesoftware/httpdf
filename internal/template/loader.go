package template

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/kaptinlin/go-i18n"
	"github.com/kaptinlin/jsonschema"
	"golang.org/x/text/language"
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
// - dir/partials/**/*.html: (optional) additional template files
// - dir/assets: (optional) directory containing static assets
// - dir/locales/{locale}.yaml: (optional) translation files
type fsLoader struct {
	root   fs.SubFS
	schema *jsonschema.Compiler
	strict bool
}

// LoaderOption configures a Loader
type LoaderOption func(*fsLoader)

// WithStrict makes the loader reject templates with unknown keys in
// config.yaml, missing page dimensions or an incomplete locale configuration.
func WithStrict() LoaderOption {
	return func(l *fsLoader) {
		l.strict = true
	}
}

// NewFSLoader creates a new fsLoader
func NewFSLoader(root fs.SubFS, opts ...LoaderOption) Loader {
	l := &fsLoader{
		root:   root,
		schema: jsonschema.NewCompiler(),
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Load loads a template from the filesystem
func (l *fsLoader) Load(name string) (*Template, error) {
	contentPath := path.Join(name, "template.html")
	configPath := path.Join(name, "config.yaml")
	schemaPath := path.Join(name, "schema.json")
	partialsPath := path.Join(name, "partials")
	assetsPath := path.Join(name, "assets")
	examplePath := path.Join(name, "example.json")
	localesGlob := path.Join(name, "locales", "*.yaml")

	// Ensure that all required files exist. Possible TOCTOU issue here, but
	// errors later on will still be handled – though the error message will
	// be different (i.e. will not return ErrTemplateNotFound)
	for _, p := range []string{contentPath, configPath, schemaPath} {
		_, err := fs.Stat(l.root, p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: missing %s", ErrTemplateNotFound, p)
		} else if err != nil {
			return nil, fmt.Errorf("stat file: %w", err)
		}
	}

	var err error
	tmpl := &Template{}

	// Load the config
	configFile, err := l.root.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer configFile.Close()
	configDecoder := yaml.NewDecoder(configFile)
	configDecoder.KnownFields(l.strict)
	err = configDecoder.Decode(&tmpl.Config)
	if err != nil {
		return nil, fmt.Errorf("decode config file: %w", describeUnknownKeys(err))
	}
	if l.strict {
		if err := l.checkConfig(name, tmpl.Config); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}

	// Load the JSON schema
	schemaFile, err := l.root.Open(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("open schema file: %w", err)
	}
	defer schemaFile.Close()
	schemaContent, err := io.ReadAll(schemaFile)
	if err != nil {
		return nil, fmt.Errorf("read schema file: %w", err)
	}
	tmpl.Schema, err = l.schema.Compile(schemaContent)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}

	// Load template contents
	fh, err := l.root.Open(contentPath)
	if err != nil {
		return nil, fmt.Errorf("open template file: %w", err)
	}
	defer fh.Close()
	tmpl.ReadFrom(fh)

	// Load partials if they exist. WalkDir visits files in lexical order, so
	// the order in which partials are parsed is deterministic.
	if stat, err := fs.Stat(l.root, partialsPath); err == nil && stat.IsDir() {
		err = fs.WalkDir(l.root, partialsPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || path.Ext(p) != ".html" {
				return err
			}
			src, err := fs.ReadFile(l.root, p)
			if err != nil {
				return err
			}
			tmpl.Partials = append(tmpl.Partials, Partial{
				Name:   strings.TrimPrefix(p, partialsPath+"/"),
				Source: string(src),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("load partials: %w", err)
		}
	}

	// Load assets if they exist
	if stat, err := fs.Stat(l.root, assetsPath); err == nil && stat.IsDir() {
		tmpl.Assets, err = l.root.Sub(assetsPath)
		if err != nil {
			return nil, fmt.Errorf("load assets: %w", err)
		}
	}

	// Load locales if they exist
	if tmpl.Config.Locale != nil {
		if !hasUsableLocale(tmpl.Config.Locale.Default, tmpl.Config.Locale.Locales) {
			return nil, errors.New("invalid config: locale requires a valid locale in locale.default or locale.locales")
		}

		localeOpts := make([]func(*i18n.I18n), 1, 3)
		localeOpts[0] = i18n.WithUnmarshaler(yaml.Unmarshal)

		if tmpl.Config.Locale.Default != "" {
			localeOpts = append(localeOpts, i18n.WithDefaultLocale(tmpl.Config.Locale.Default))
		}
		if len(tmpl.Config.Locale.Locales) > 0 {
			localeOpts = append(localeOpts, i18n.WithLocales(tmpl.Config.Locale.Locales...))
		}
		tmpl.I18n = i18n.NewBundle(localeOpts...)
		err = tmpl.I18n.LoadFS(l.root, localesGlob)
		if err != nil {
			return nil, fmt.Errorf("load locales: %w", err)
		}
	}

	// Load example data if it exists
	fd, err := l.root.Open(examplePath)
	if errors.Is(err, fs.ErrNotExist) {
		tmpl.Example = nil // No example data available
	} else if err != nil {
		return nil, fmt.Errorf("open example data file: %w", err)
	} else {
		defer fd.Close()
		if err := json.NewDecoder(fd).Decode(&tmpl.Example); err != nil {
			return nil, fmt.Errorf("decode example data file: %w", err)
		}
	}

	return tmpl, nil
}

// checkConfig reports config values that are syntactically valid, but would
// lead to broken PDFs or missing translations.
func (l *fsLoader) checkConfig(name string, c Config) error {
	var errs []error

	if c.Page.Width <= 0 {
		errs = append(errs, errors.New("page.width must be greater than 0"))
	}
	if c.Page.Height <= 0 {
		errs = append(errs, errors.New("page.height must be greater than 0"))
	}

	if c.Locale == nil {
		return errors.Join(errs...)
	}

	if len(c.Locale.Locales) == 0 {
		errs = append(errs, errors.New("locale.locales must not be empty"))
	}
	for _, locale := range c.Locale.Locales {
		if !validLocale(locale) {
			errs = append(errs, fmt.Errorf("locale.locales: %q is not a valid locale", locale))
		}
	}
	if c.Locale.Default != "" && !validLocale(c.Locale.Default) {
		errs = append(errs, fmt.Errorf("locale.default: %q is not a valid locale", c.Locale.Default))
	}

	configured := make(map[string]bool, len(c.Locale.Locales))
	for _, locale := range c.Locale.Locales {
		configured[normalizeLocale(locale)] = true
	}
	if c.Locale.Default != "" && len(c.Locale.Locales) > 0 && !configured[normalizeLocale(c.Locale.Default)] {
		errs = append(errs, fmt.Errorf("locale.default %q must be one of locale.locales", c.Locale.Default))
	}

	files, err := fs.Glob(l.root, path.Join(name, "locales", "*.yaml"))
	if err != nil {
		return fmt.Errorf("list translation files: %w", err)
	}
	available := make(map[string]bool, len(files))
	for _, file := range files {
		available[normalizeLocale(file)] = true
	}
	for _, locale := range c.Locale.Locales {
		if !available[normalizeLocale(locale)] {
			errs = append(errs, fmt.Errorf("missing translation file locales/%s.yaml for locale %q", locale, locale))
		}
	}

	return errors.Join(errs...)
}

var unknownKeyPattern = regexp.MustCompile(`^(line \d+): field (\S+) not found in type .*$`)

// describeUnknownKeys rewrites yaml's errors about unknown keys, which include
// the Go type definitions of the config, into ones meant for template authors.
func describeUnknownKeys(err error) error {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		for i, msg := range typeErr.Errors {
			typeErr.Errors[i] = unknownKeyPattern.ReplaceAllString(msg, `$1: unknown key "$2"`)
		}
	}
	return err
}

// hasUsableLocale reports whether go-i18n can determine a default locale from
// the config, mirroring how it parses the values. Without one, i18n.NewBundle
// panics.
func hasUsableLocale(defaultLocale string, locales []string) bool {
	return language.Make(defaultLocale) != language.Und || slices.ContainsFunc(locales, validLocale)
}

// validLocale reports whether go-i18n accepts the locale. It silently ignores
// the ones it doesn't.
func validLocale(locale string) bool {
	tag, err := language.Parse(locale)
	return err == nil && tag != language.Und
}

// normalizeLocale mirrors how go-i18n derives the locale from the name of a
// translation file, so that config values and file names can be compared.
func normalizeLocale(s string) string {
	s, _, _ = strings.Cut(path.Base(s), ".")
	return strings.ReplaceAll(strings.ToLower(s), "_", "-")
}

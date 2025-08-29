package template

import (
	"os"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvFunc(t *testing.T) {
	tests := []struct {
		name           string
		exposedEnvVars []string
		envKey         string
		envValue       string
		expected       string
	}{
		{
			name:           "exposed env var returns value",
			exposedEnvVars: []string{"TEST_VAR"},
			envKey:         "TEST_VAR",
			envValue:       "test_value",
			expected:       "test_value",
		},
		{
			name:           "non-exposed env var returns empty string",
			exposedEnvVars: []string{"EXPOSED_VAR"},
			envKey:         "NON_EXPOSED_VAR",
			envValue:       "secret_value",
			expected:       "",
		},
		{
			name:           "empty exposed list returns empty string",
			exposedEnvVars: []string{},
			envKey:         "ANY_VAR",
			envValue:       "any_value",
			expected:       "",
		},
		{
			name:           "multiple exposed vars work correctly",
			exposedEnvVars: []string{"VAR1", "VAR2", "VAR3"},
			envKey:         "VAR2",
			envValue:       "middle_value",
			expected:       "middle_value",
		},
		{
			name:           "non-existent env var returns empty string",
			exposedEnvVars: []string{"NON_EXISTENT"},
			envKey:         "NON_EXISTENT",
			envValue:       "", // Will not be set
			expected:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up environment before and after test
			originalValue := os.Getenv(tt.envKey)
			defer func() {
				if originalValue != "" {
					os.Setenv(tt.envKey, originalValue)
				} else {
					os.Unsetenv(tt.envKey)
				}
			}()

			// Set environment variable if value is provided
			if tt.envValue != "" {
				err := os.Setenv(tt.envKey, tt.envValue)
				require.NoError(t, err)
			} else {
				os.Unsetenv(tt.envKey)
			}

			// Create the env function
			envFunc := envFunc(tt.exposedEnvVars)

			// Test the function
			result := envFunc(tt.envKey)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnvFuncInTemplate(t *testing.T) {
	// Set up test environment variables
	err := os.Setenv("ALLOWED_VAR", "allowed_value")
	require.NoError(t, err)
	defer os.Unsetenv("ALLOWED_VAR")

	err = os.Setenv("DISALLOWED_VAR", "secret_value")
	require.NoError(t, err)
	defer os.Unsetenv("DISALLOWED_VAR")

	// Create template with env function
	exposedEnvVars := []string{"ALLOWED_VAR"}
	funcs := templateFuncs("")
	envTemplateFuncs(funcs, exposedEnvVars)

	tmpl := template.New("test").Funcs(funcs)
	tmpl, err = tmpl.Parse(`
Allowed: {{env "ALLOWED_VAR"}}
Disallowed: {{env "DISALLOWED_VAR"}}
NonExistent: {{env "NON_EXISTENT_VAR"}}
`)
	require.NoError(t, err)

	// Execute template
	var result strings.Builder
	err = tmpl.Execute(&result, nil)
	require.NoError(t, err)

	output := result.String()
	assert.Contains(t, output, "Allowed: allowed_value")
	assert.Contains(t, output, "Disallowed: ")
	assert.Contains(t, output, "NonExistent: ")
	assert.NotContains(t, output, "secret_value")
}

func TestChunk(t *testing.T) {
	tests := []struct {
		name     string
		input    []any
		size     int
		expected [][]any
	}{
		{
			name:     "chunk array of 6 into groups of 2",
			input:    []any{1, 2, 3, 4, 5, 6},
			size:     2,
			expected: [][]any{{1, 2}, {3, 4}, {5, 6}},
		},
		{
			name:     "chunk array of 5 into groups of 2",
			input:    []any{1, 2, 3, 4, 5},
			size:     2,
			expected: [][]any{{1, 2}, {3, 4}, {5}},
		},
		{
			name:     "chunk array of 3 into groups of 5",
			input:    []any{1, 2, 3},
			size:     5,
			expected: [][]any{{1, 2, 3}},
		},
		{
			name:     "chunk empty array",
			input:    []any{},
			size:     2,
			expected: [][]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chunk(tt.input, tt.size)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChunkInvalidArgs(t *testing.T) {
	// Test with less than 2 arguments
	result := chunk([]any{1, 2, 3})
	assert.Nil(t, result)

	result = chunk()
	assert.Nil(t, result)
}

func TestAssetFunc(t *testing.T) {
	tests := []struct {
		name         string
		assetsPrefix string
		paths        []string
		expected     string
	}{
		{
			name:         "single path with prefix",
			assetsPrefix: "/static",
			paths:        []string{"css", "style.css"},
			expected:     "/static/css/style.css",
		},
		{
			name:         "multiple paths with prefix",
			assetsPrefix: "/assets",
			paths:        []string{"js", "lib", "jquery.js"},
			expected:     "/assets/js/lib/jquery.js",
		},
		{
			name:         "empty prefix",
			assetsPrefix: "",
			paths:        []string{"images", "logo.png"},
			expected:     "images/logo.png",
		},
		{
			name:         "single path no additional paths",
			assetsPrefix: "/static",
			paths:        []string{"favicon.ico"},
			expected:     "/static/favicon.ico",
		},
		{
			name:         "empty paths",
			assetsPrefix: "/static",
			paths:        []string{},
			expected:     "/static",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcs := templateFuncs(tt.assetsPrefix)
			assetFunc := funcs["asset"].(func(...string) string)
			result := assetFunc(tt.paths...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAssetFuncInTemplate(t *testing.T) {
	funcs := templateFuncs("/static")

	tmpl := template.New("test").Funcs(funcs)
	tmpl, err := tmpl.Parse(`
CSS: {{asset "css" "style.css"}}
JS: {{asset "js" "app.js"}}
Image: {{asset "images" "logo.png"}}
`)
	require.NoError(t, err)

	var result strings.Builder
	err = tmpl.Execute(&result, nil)
	require.NoError(t, err)

	output := result.String()
	assert.Contains(t, output, "CSS: /static/css/style.css")
	assert.Contains(t, output, "JS: /static/js/app.js")
	assert.Contains(t, output, "Image: /static/images/logo.png")
}

func TestTemplateFuncsIntegration(t *testing.T) {
	// Test that all base functions are available
	funcs := templateFuncs("/assets")

	// Check that sprig functions are included
	assert.NotNil(t, funcs["upper"])
	assert.NotNil(t, funcs["lower"])
	assert.NotNil(t, funcs["title"])

	// Check that custom functions are included
	assert.NotNil(t, funcs["chunk"])
	assert.NotNil(t, funcs["asset"])

	// Note: sprig includes an env function by default, but our custom env function
	// is added separately by envTemplateFuncs to provide security controls
	assert.NotNil(t, funcs["env"]) // sprig's env function

	// Check that i18n functions are not included by default
	assert.Nil(t, funcs["tr"])
	assert.Nil(t, funcs["locale"])
	assert.Nil(t, funcs["trLocale"])
}

func TestI18nTemplateFuncs(t *testing.T) {
	// Test with nil bundle
	funcs := templateFuncs("")
	i18nTemplateFuncs(funcs, nil, "en")

	// Check that functions are added
	assert.NotNil(t, funcs["locale"])
	assert.NotNil(t, funcs["tr"])
	assert.NotNil(t, funcs["trLocale"])

	// Test locale function with nil bundle
	localeFunc := funcs["locale"].(func() string)
	assert.Equal(t, "en", localeFunc())

	// Test translate function with nil bundle
	trFunc := funcs["tr"].(func(string, ...any) string)
	assert.Equal(t, "!(localizer is nil)", trFunc("test.key"))

	// Test translate locale function with nil bundle
	trLocaleFunc := funcs["trLocale"].(func(string, string, ...any) string)
	assert.Equal(t, "!(bundle is nil)", trLocaleFunc("fr", "test.key"))
}

func TestI18nVars(t *testing.T) {
	tests := []struct {
		name     string
		args     []any
		expected map[string]any
	}{
		{
			name:     "even number of args",
			args:     []any{"name", "John", "age", 30},
			expected: map[string]any{"name": "John", "age": 30},
		},
		{
			name:     "odd number of args",
			args:     []any{"name", "John", "age"},
			expected: map[string]any{"name": "John", "age": nil},
		},
		{
			name:     "empty args",
			args:     []any{},
			expected: map[string]any{},
		},
		{
			name:     "non-string key",
			args:     []any{123, "value", "valid", "test"},
			expected: map[string]any{"valid": "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := i18nVars(tt.args)
			assert.Equal(t, len(tt.expected), len(result))
			for k, v := range tt.expected {
				assert.Equal(t, v, result[k])
			}
		})
	}
}

func TestEnvTemplateFuncs(t *testing.T) {
	// Set up test environment
	err := os.Setenv("TEST_VAR", "test_value")
	require.NoError(t, err)
	defer os.Unsetenv("TEST_VAR")

	// Create base funcs and add env funcs
	funcs := templateFuncs("")
	exposedEnvVars := []string{"TEST_VAR"}
	envTemplateFuncs(funcs, exposedEnvVars)

	// Check that env function is added
	assert.NotNil(t, funcs["env"])

	// Test env function
	envFunc := funcs["env"].(func(string) string)
	assert.Equal(t, "test_value", envFunc("TEST_VAR"))
	assert.Equal(t, "", envFunc("NON_EXPOSED_VAR"))
}

func TestModularTemplateFuncsIntegration(t *testing.T) {
	// Set up test environment
	err := os.Setenv("EXPOSED_VAR", "exposed_value")
	require.NoError(t, err)
	defer os.Unsetenv("EXPOSED_VAR")

	// Create template with all function types
	funcs := templateFuncs("/static")
	envTemplateFuncs(funcs, []string{"EXPOSED_VAR"})
	i18nTemplateFuncs(funcs, nil, "en")

	tmpl := template.New("test").Funcs(funcs)
	tmpl, err = tmpl.Parse(`
Asset: {{asset "css" "style.css"}}
Chunk: {{range chunk .items 2}}{{.}}{{end}}
Env: {{env "EXPOSED_VAR"}}
Locale: {{locale}}
Upper: {{upper "hello"}}
`)
	require.NoError(t, err)

	var result strings.Builder
	templateData := map[string]any{
		"items": []any{1, 2, 3, 4},
	}
	err = tmpl.Execute(&result, templateData)
	require.NoError(t, err)

	output := result.String()
	assert.Contains(t, output, "Asset: /static/css/style.css")
	assert.Contains(t, output, "Chunk: [1 2][3 4]")
	assert.Contains(t, output, "Env: exposed_value")
	assert.Contains(t, output, "Locale: en")
	assert.Contains(t, output, "Upper: HELLO")
}

func TestEnvFunctionOverride(t *testing.T) {
	// Set up test environment variable
	err := os.Setenv("TEST_OVERRIDE", "secret_value")
	require.NoError(t, err)
	defer os.Unsetenv("TEST_OVERRIDE")

	// Test sprig's env function (unrestricted access)
	sprigFuncs := templateFuncs("")
	sprigEnvFunc := sprigFuncs["env"].(func(string) string)
	assert.Equal(t, "secret_value", sprigEnvFunc("TEST_OVERRIDE"))

	// Test our custom env function (restricted access)
	customFuncs := templateFuncs("")
	envTemplateFuncs(customFuncs, []string{}) // Empty whitelist
	customEnvFunc := customFuncs["env"].(func(string) string)
	assert.Equal(t, "", customEnvFunc("TEST_OVERRIDE")) // Should be blocked

	// Test our custom env function with variable in whitelist
	allowedFuncs := templateFuncs("")
	envTemplateFuncs(allowedFuncs, []string{"TEST_OVERRIDE"})
	allowedEnvFunc := allowedFuncs["env"].(func(string) string)
	assert.Equal(t, "secret_value", allowedEnvFunc("TEST_OVERRIDE")) // Should be allowed
}

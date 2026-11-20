package template

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"html/template"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

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
		input    any
		size     any
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
		{
			name:     "size from JSON",
			input:    []any{1, 2, 3},
			size:     2.0,
			expected: [][]any{{1, 2}, {3}},
		},
		{
			name:     "size of another integer type",
			input:    []any{1, 2, 3},
			size:     uint8(2),
			expected: [][]any{{1, 2}, {3}},
		},
		{
			name:     "list of another type",
			input:    []string{"a", "b", "c"},
			size:     2,
			expected: [][]any{{"a", "b"}, {"c"}},
		},
		{
			name:     "nil list",
			input:    nil,
			size:     2,
			expected: [][]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := chunk(tt.input, tt.size)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChunkInvalidArgs(t *testing.T) {
	// Test with less than 2 arguments
	result, err := chunk([]any{1, 2, 3})
	assert.NoError(t, err)
	assert.Nil(t, result)

	result, err = chunk()
	assert.NoError(t, err)
	assert.Nil(t, result)

	tests := []struct {
		name  string
		input any
		size  any
		err   string
	}{
		{name: "zero size", input: []any{1}, size: 0, err: "chunk: size must be greater than 0, got 0"},
		{name: "negative size", input: []any{1}, size: -1, err: "chunk: size must be greater than 0, got -1"},
		{name: "fractional size", input: []any{1}, size: 2.5, err: "chunk: size: expected an integer, got 2.5"},
		{name: "size out of range", input: []any{1}, size: 1e300, err: "chunk: size: expected an integer, got 1e+300"},
		{name: "non-numeric size", input: []any{1}, size: "2", err: "chunk: size: expected an integer, got string"},
		{name: "non-list input", input: "abc", size: 2, err: "chunk: expected a list, got string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A size of 0 used to loop forever, so a regression must fail
			// instead of hanging the test
			done := make(chan error, 1)
			go func() {
				_, err := chunk(tt.input, tt.size)
				done <- err
			}()

			select {
			case err := <-done:
				assert.EqualError(t, err, tt.err)
			case <-time.After(time.Second):
				t.Fatal("chunk didn't return")
			}
		})
	}
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

	// Sprig's unrestricted env function is removed; our custom env function is
	// added separately by envTemplateFuncs to provide security controls
	assert.Nil(t, funcs["env"])

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

	// Sprig's env function (unrestricted access) is removed
	sprigFuncs := templateFuncs("")
	assert.NotContains(t, sprigFuncs, "env")

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

func TestUnsafeSprigFuncsAreRemoved(t *testing.T) {
	funcs := templateFuncs("")

	for _, name := range []string{"env", "expandenv", "getHostByName"} {
		assert.NotContains(t, funcs, name)
	}
	assert.Contains(t, funcs, "upper", "other Sprig functions should remain")
}
func TestQrCodeFunc(t *testing.T) {
	tests := []struct {
		name         string
		size         any
		data         string
		expectedSize int // The size we expect to be used (after default handling)
	}{
		{
			name:         "size from JSON",
			size:         128.0,
			data:         "Hello World",
			expectedSize: 128,
		},
		{
			name:         "size of another integer type",
			size:         uint16(128),
			data:         "Hello World",
			expectedSize: 128,
		},
		{
			name:         "maximum size",
			size:         maxQrCodeSize,
			data:         "Hello World",
			expectedSize: maxQrCodeSize,
		},
		{
			name:         "positive size with simple data",
			size:         128,
			data:         "Hello World",
			expectedSize: 128,
		},
		{
			name:         "zero size uses default",
			size:         0,
			data:         "Test Data",
			expectedSize: 256, // default size
		},
		{
			name:         "negative size uses default",
			size:         -10,
			data:         "Test Data",
			expectedSize: 256, // default size
		},
		{
			name:         "large size",
			size:         512,
			data:         "Large QR Code",
			expectedSize: 512,
		},
		{
			name:         "empty data",
			size:         100,
			data:         "",
			expectedSize: 100,
		},
		{
			name:         "special characters",
			size:         200,
			data:         "Hello! @#$%^&*()_+ 世界",
			expectedSize: 200,
		},
		{
			name:         "URL data",
			size:         150,
			data:         "https://example.com/path?param=value",
			expectedSize: 150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := qrCodeFunc(tt.size, tt.data)
			require.NoError(t, err)
			result := string(url)

			// Check that result starts with the correct data URL prefix
			assert.True(t, strings.HasPrefix(result, "data:image/png;base64,"),
				"QR code should return a PNG data URL")

			// Extract and validate base64 part
			base64Part := strings.TrimPrefix(result, "data:image/png;base64,")
			assert.NotEmpty(t, base64Part, "Base64 part should not be empty")

			// Verify that the base64 is valid
			decodedData, err := base64.RawStdEncoding.DecodeString(base64Part)
			require.NoError(t, err, "Base64 should be valid")
			assert.NotEmpty(t, decodedData, "Decoded data should not be empty")

			// Check that it's a valid PNG by checking the PNG signature
			// PNG files start with these 8 bytes: 137 80 78 71 13 10 26 10
			if len(decodedData) >= 8 {
				pngSignature := []byte{137, 80, 78, 71, 13, 10, 26, 10}
				assert.Equal(t, pngSignature, decodedData[:8],
					"Decoded data should have PNG signature")
			}

			img, err := png.DecodeConfig(bytes.NewReader(decodedData))
			require.NoError(t, err)
			assert.Equal(t, tt.expectedSize, img.Width)
			assert.Equal(t, tt.expectedSize, img.Height)
		})
	}
}

func TestQrCodeFuncInTemplate(t *testing.T) {
	// Create template with barcode function
	funcs := templateFuncs("")
	barcodeTemplateFuncs(funcs)

	// Check that qrCode function is available
	assert.NotNil(t, funcs["qrCode"])

	// The QR codes are used as image sources, where html/template would
	// replace any untyped data URL with a placeholder
	tmpl := template.New("test").Funcs(funcs)
	tmpl, err := tmpl.Parse(`
QR Small: <img src="{{qrCode 64 "Small QR"}}">
QR Default: <img src="{{qrCode 0 "Default Size"}}">
QR Large: <img src="{{qrCode 256 "Large QR Code"}}">
QR URL: <img src="{{qrCode 128 "https://example.com"}}">
`)
	require.NoError(t, err)

	var result strings.Builder
	err = tmpl.Execute(&result, nil)
	require.NoError(t, err)

	// Characters such as "+" are escaped as HTML entities in the attributes,
	// which the browser decodes again
	output := html.UnescapeString(result.String())

	// Check that all QR codes are generated as proper data URLs
	assert.NotContains(t, output, "ZgotmplZ")
	assert.Contains(t, output, `QR Small: <img src="data:image/png;base64,`)
	assert.Contains(t, output, `QR Default: <img src="data:image/png;base64,`)
	assert.Contains(t, output, `QR Large: <img src="data:image/png;base64,`)
	assert.Contains(t, output, `QR URL: <img src="data:image/png;base64,`)

	// Verify that we have different base64 content for different inputs
	lines := strings.Split(output, "\n")
	qrLines := make([]string, 0)
	for _, line := range lines {
		if strings.Contains(line, "data:image/png;base64,") {
			qrLines = append(qrLines, line)
		}
	}

	// Should have 4 different QR codes
	assert.Len(t, qrLines, 4)

	// Extract base64 parts and verify they're different
	base64Parts := make([]string, len(qrLines))
	for i, line := range qrLines {
		parts := strings.Split(line, "data:image/png;base64,")
		require.Len(t, parts, 2, "Should have exactly one data URL per line")
		base64Parts[i] = strings.TrimSuffix(strings.TrimSpace(parts[1]), `">`)

		// Verify each base64 is valid
		_, err := base64.RawStdEncoding.DecodeString(base64Parts[i])
		assert.NoError(t, err, "Base64 should be valid for line: %s", line)
	}

	// Verify they're all different (different data should produce different QR codes)
	for i := 0; i < len(base64Parts); i++ {
		for j := i + 1; j < len(base64Parts); j++ {
			assert.NotEqual(t, base64Parts[i], base64Parts[j],
				"Different QR code data should produce different base64")
		}
	}
}

func TestBarcodeTemplateFuncsIntegration(t *testing.T) {
	// Create base functions and add barcode functions
	funcs := templateFuncs("")
	barcodeTemplateFuncs(funcs)

	// Check that qrCode function is properly added
	assert.NotNil(t, funcs["qrCode"])

	// Verify the function signature
	qrCodeFunc, ok := funcs["qrCode"].(func(any, string) (template.URL, error))
	require.True(t, ok, "qrCode function should have correct signature")

	// Test the function directly
	result, err := qrCodeFunc(100, "test")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(result), "data:image/png;base64,"))
}

func TestQrCodeFuncErrors(t *testing.T) {
	tests := []struct {
		name string
		size any
		data string
		err  string
	}{
		{name: "fractional size", size: 128.5, data: "hi", err: "qrCode: size: expected an integer, got 128.5"},
		{name: "non-numeric size", size: "128", data: "hi", err: "qrCode: size: expected an integer, got string"},
		{name: "missing size", size: nil, data: "hi", err: "qrCode: size: expected an integer, got <nil>"},
		{
			name: "size too large",
			size: 1e6,
			data: "hi",
			err:  fmt.Sprintf("qrCode: size 1000000 is too large, the maximum is %d", maxQrCodeSize),
		},
		{
			// A version 40 QR code holds at most 2331 bytes at level M
			name: "data too long",
			size: 0,
			data: strings.Repeat("x", 3000),
			err:  "qrCode: data is too long for a QR code (3000 bytes)",
		},
		{
			// The smallest QR code has 21x21 modules
			name: "size smaller than the code",
			size: 10,
			data: "hi",
			err:  "qrCode: size 10 is too small for the data, it needs at least 21",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := qrCodeFunc(tt.size, tt.data)
			assert.EqualError(t, err, tt.err)
			assert.Empty(t, url)
		})
	}
}

func TestQrCodeFuncErrorInTemplate(t *testing.T) {
	funcs := templateFuncs("")
	barcodeTemplateFuncs(funcs)

	tmpl, err := template.New("test").Funcs(funcs).Parse(`<img src="{{qrCode 0 .data}}">`)
	require.NoError(t, err)

	err = tmpl.Execute(&strings.Builder{}, map[string]any{"data": strings.Repeat("x", 3000)})
	assert.ErrorContains(t, err, "qrCode: data is too long for a QR code (3000 bytes)")
	assert.NotContains(t, err.Error(), "xxx", "the error shouldn't contain the data")
}

package template_test

import (
	"os"
	"strings"
	"testing"

	"github.com/sehrgutesoftware/httpdf/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvFunctionIntegration(t *testing.T) {
	t.Run("complete_template_with_env_variables", func(t *testing.T) {
		// Set up test environment variables
		testEnvVars := map[string]string{
			"API_BASE_URL":   "https://api.example.com",
			"APP_VERSION":    "1.2.3",
			"ENVIRONMENT":    "production",
			"SECRET_API_KEY": "super-secret-key",
			"DEBUG_MODE":     "false",
		}

		// Set environment variables
		for key, value := range testEnvVars {
			err := os.Setenv(key, value)
			require.NoError(t, err)
			defer os.Unsetenv(key)
		}

		// Create a template with configuration
		tmpl := &template.Template{
			Config: template.Config{
				Page: struct {
					Width  float64 `yaml:"width"`
					Height float64 `yaml:"height"`
				}{
					Width:  210,
					Height: 297,
				},
				ExposedEnvVars: []string{
					"API_BASE_URL",
					"APP_VERSION",
					"ENVIRONMENT",
					// Intentionally exclude SECRET_API_KEY to test security
				},
			},
		}

		// Template content that uses env function
		templateContent := `<!DOCTYPE html>
<html>
<head>
    <title>Environment Test Document</title>
</head>
<body>
    <h1>{{.title}}</h1>

    <div class="config-section">
        <h2>Configuration</h2>
        <p>API URL: {{env "API_BASE_URL" | default "Not configured"}}</p>
        <p>Version: {{env "APP_VERSION" | default "Unknown"}}</p>
        <p>Environment: {{env "ENVIRONMENT" | default "development"}}</p>
        <p>Debug: {{env "DEBUG_MODE" | default "false"}}</p>
    </div>

    <div class="security-section">
        <h2>Security Test</h2>
        <p>Secret (should be empty): "{{env "SECRET_API_KEY"}}"</p>
        <p>Non-existent var: "{{env "NON_EXISTENT_VAR"}}"</p>
    </div>

    <div class="data-section">
        <h2>Template Data</h2>
        <p>User: {{.user.name}}</p>
        <p>Email: {{.user.email}}</p>
        <ul>
        {{range .items}}
            <li>{{.name}}: {{.value}}</li>
        {{end}}
        </ul>
    </div>

    <footer>
        <p>Generated on {{env "ENVIRONMENT"}} environment</p>
        <p>Version {{env "APP_VERSION"}}</p>
    </footer>
</body>
</html>`

		tmpl.WriteString(templateContent)

		// Test data
		testData := map[string]interface{}{
			"title": "Integration Test Document",
			"user": map[string]interface{}{
				"name":  "John Doe",
				"email": "john@example.com",
			},
			"items": []map[string]interface{}{
				{"name": "Item 1", "value": "Value 1"},
				{"name": "Item 2", "value": "Value 2"},
			},
		}

		// Render template
		var output strings.Builder
		err := tmpl.Render(testData, "/assets", "en", &output)
		require.NoError(t, err)

		result := output.String()

		// Verify that exposed environment variables are accessible
		assert.Contains(t, result, "API URL: https://api.example.com")
		assert.Contains(t, result, "Version: 1.2.3")
		assert.Contains(t, result, "Environment: production")

		// Verify that non-exposed variables are blocked (should be empty)
		assert.Contains(t, result, `Secret (should be empty): ""`)
		assert.NotContains(t, result, "super-secret-key")

		// Verify that non-existent variables return empty string
		assert.Contains(t, result, `Non-existent var: ""`)

		// Verify that default values work for non-exposed vars
		assert.Contains(t, result, "Debug: false") // DEBUG_MODE not in exposed list, so default used

		// Verify template data still works normally
		assert.Contains(t, result, "User: John Doe")
		assert.Contains(t, result, "Email: john@example.com")
		assert.Contains(t, result, "Item 1: Value 1")
		assert.Contains(t, result, "Item 2: Value 2")

		// Verify multiple uses of env function work
		assert.Contains(t, result, "Generated on production environment")
		assert.Contains(t, result, "Version 1.2.3")
	})

	t.Run("template_with_no_exposed_env_vars", func(t *testing.T) {
		// Set up environment variable
		err := os.Setenv("TEST_VAR", "test_value")
		require.NoError(t, err)
		defer os.Unsetenv("TEST_VAR")

		// Create template with empty exposedEnvVars
		tmpl := &template.Template{
			Config: template.Config{
				ExposedEnvVars: []string{}, // Empty exposed list
			},
		}

		tmpl.WriteString(`<p>Value: {{env "TEST_VAR" | default "denied"}}</p>`)

		var output strings.Builder
		err = tmpl.Render(map[string]interface{}{}, "/assets", "en", &output)
		require.NoError(t, err)

		// Should use default value since no env vars are allowed
		assert.Contains(t, output.String(), "Value: denied")
		assert.NotContains(t, output.String(), "test_value")
	})

	t.Run("template_with_nil_exposed_env_vars", func(t *testing.T) {
		// Set up environment variable
		err := os.Setenv("TEST_VAR", "test_value")
		require.NoError(t, err)
		defer os.Unsetenv("TEST_VAR")

		// Create template with nil exposedEnvVars (should work same as empty slice)
		tmpl := &template.Template{
			Config: template.Config{
				ExposedEnvVars: nil,
			},
		}

		tmpl.WriteString(`<p>Value: {{env "TEST_VAR" | default "denied"}}</p>`)

		var output strings.Builder
		err = tmpl.Render(map[string]interface{}{}, "/assets", "en", &output)
		require.NoError(t, err)

		// Should use default value since no env vars are allowed
		assert.Contains(t, output.String(), "Value: denied")
		assert.NotContains(t, output.String(), "test_value")
	})

	t.Run("env_function_with_sprig_functions", func(t *testing.T) {
		// Set up environment variables
		err := os.Setenv("SERVICE_NAME", "user-service")
		require.NoError(t, err)
		defer os.Unsetenv("SERVICE_NAME")

		err = os.Setenv("BASE_URL", "https://api.example.com")
		require.NoError(t, err)
		defer os.Unsetenv("BASE_URL")

		tmpl := &template.Template{
			Config: template.Config{
				ExposedEnvVars: []string{"SERVICE_NAME", "BASE_URL"},
			},
		}

		// Template using env with Sprig functions
		tmpl.WriteString(`
<p>Service: {{env "SERVICE_NAME" | title}}</p>
<p>URL: {{env "BASE_URL" | lower}}</p>
<p>Combined: {{env "SERVICE_NAME" | upper}}-{{env "BASE_URL" | replace "https://" "" | replace "." "-"}}</p>
`)

		var output strings.Builder
		err = tmpl.Render(map[string]interface{}{}, "/assets", "en", &output)
		require.NoError(t, err)

		result := output.String()
		assert.Contains(t, result, "Service: User-Service")
		assert.Contains(t, result, "URL: https://api.example.com")
		assert.Contains(t, result, "Combined: USER-SERVICE-api-example-com")
	})
}

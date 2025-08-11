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
	funcs := templateFuncs(nil, exposedEnvVars)

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

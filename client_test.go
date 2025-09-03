package httpdf_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sehrgutesoftware/httpdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Run("it_creates_a_new_client_with_provided_base_url", func(t *testing.T) {
		baseURL := "http://localhost:8080"
		client := httpdf.NewClient(baseURL)

		assert.NotNil(t, client)
	})

	t.Run("it_creates_a_new_client_with_custom_http_client_option", func(t *testing.T) {
		baseURL := "http://localhost:8080"
		customHTTPClient := &http.Client{}

		client := httpdf.NewClient(baseURL, httpdf.WithHTTPClient(customHTTPClient))

		assert.NotNil(t, client)
	})
}

func TestClient_Render(t *testing.T) {
	t.Run("it_renders_template_successfully_with_valid_request", func(t *testing.T) {
		expectedResponse := []byte("rendered PDF content")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request method and path
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/templates/test-template/render", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			// Verify request body
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var requestData map[string]any
			err = json.Unmarshal(body, &requestData)
			require.NoError(t, err)
			assert.Equal(t, "World", requestData["name"])

			// Send successful response
			w.WriteHeader(http.StatusOK)
			w.Write(expectedResponse)
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "test-template", values)
		defer result.Close()

		assert.NoError(t, err)

		// Read and verify response content
		content, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, expectedResponse, content)

		// Close the response body
		err = result.Close()
		assert.NoError(t, err)
	})

	t.Run("it_renders_template_with_lang_query_parameter", func(t *testing.T) {
		expectedResponse := []byte("rendered PDF content in German")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request method and path
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/templates/test-template/render", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			// Verify lang query parameter
			assert.Equal(t, "de", r.URL.Query().Get("lang"))

			// Verify request body
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var requestData map[string]any
			err = json.Unmarshal(body, &requestData)
			require.NoError(t, err)
			assert.Equal(t, "World", requestData["name"])

			// Send successful response
			w.WriteHeader(http.StatusOK)
			w.Write(expectedResponse)
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "test-template", values, "de")
		defer result.Close()

		assert.NoError(t, err)

		// Read and verify response content
		content, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, expectedResponse, content)

		// Close the response body
		err = result.Close()
		assert.NoError(t, err)
	})

	t.Run("it_renders_template_without_lang_when_empty_string_provided", func(t *testing.T) {
		expectedResponse := []byte("rendered PDF content")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request method and path
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/templates/test-template/render", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			// Verify no lang query parameter when empty string is provided
			assert.Empty(t, r.URL.Query().Get("lang"))

			// Send successful response
			w.WriteHeader(http.StatusOK)
			w.Write(expectedResponse)
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "test-template", values, "")
		defer result.Close()

		assert.NoError(t, err)

		// Read and verify response content
		content, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, expectedResponse, content)

		// Close the response body
		err = result.Close()
		assert.NoError(t, err)
	})

	t.Run("it_renders_template_with_multiple_lang_parameters_uses_first", func(t *testing.T) {
		expectedResponse := []byte("rendered PDF content")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify only first lang parameter is used
			assert.Equal(t, "en", r.URL.Query().Get("lang"))

			// Send successful response
			w.WriteHeader(http.StatusOK)
			w.Write(expectedResponse)
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "test-template", values, "en", "de", "fr")
		defer result.Close()

		assert.NoError(t, err)

		// Close the response body
		err = result.Close()
		assert.NoError(t, err)
	})

	t.Run("it_handles_special_characters_in_lang_parameter", func(t *testing.T) {
		expectedResponse := []byte("rendered PDF content")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify lang parameter with special characters is properly encoded
			assert.Equal(t, "zh-CN", r.URL.Query().Get("lang"))

			// Send successful response
			w.WriteHeader(http.StatusOK)
			w.Write(expectedResponse)
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "test-template", values, "zh-CN")
		defer result.Close()

		assert.NoError(t, err)

		// Close the response body
		err = result.Close()
		assert.NoError(t, err)
	})

	t.Run("it_renders_template_with_nil_values", func(t *testing.T) {
		expectedResponse := []byte("rendered PDF content")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request body
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			// nil values should be encoded as "null"
			assert.Equal(t, "null\n", string(body))

			w.WriteHeader(http.StatusOK)
			w.Write(expectedResponse)
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)

		result, err := client.Render(context.Background(), "test-template", nil)
		defer result.Close()

		assert.NoError(t, err)

		// Read and verify response content
		content, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, expectedResponse, content)

		// Close the response body
		err = result.Close()
		assert.NoError(t, err)

	})

	t.Run("it_renders_template_with_complex_data_structure", func(t *testing.T) {
		expectedResponse := []byte("success")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var requestData map[string]any
			err = json.Unmarshal(body, &requestData)
			require.NoError(t, err)

			// Verify complex structure
			assert.Equal(t, "John Doe", requestData["name"])
			assert.Equal(t, float64(30), requestData["age"]) // JSON numbers are float64

			items, ok := requestData["items"].([]any)
			require.True(t, ok)
			assert.Len(t, items, 2)
			assert.Equal(t, "item1", items[0])
			assert.Equal(t, "item2", items[1])

			metadata, ok := requestData["metadata"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "test", metadata["type"])

			w.WriteHeader(http.StatusOK)
			w.Write(expectedResponse)
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{
			"name":  "John Doe",
			"age":   30,
			"items": []string{"item1", "item2"},
			"metadata": map[string]string{
				"type": "test",
			},
		}

		result, err := client.Render(context.Background(), "complex-template", values)
		defer result.Close()

		assert.NoError(t, err)

		// Read and verify response content
		content, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, expectedResponse, content)

		// Close the response body
		err = result.Close()
		assert.NoError(t, err)
	})

	t.Run("it_returns_error_when_json_encoding_fails", func(t *testing.T) {
		client := httpdf.NewClient("http://localhost:8080")

		// Create a value that cannot be JSON encoded (channel)
		invalidValues := map[string]any{
			"channel": make(chan int),
		}

		result, err := client.Render(context.Background(), "test-template", invalidValues)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "render template")
	})

	t.Run("it_returns_error_when_server_is_unreachable", func(t *testing.T) {
		client := httpdf.NewClient("http://localhost:9999") // Non-existent server
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "test-template", values)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "render template")
	})

	t.Run("it_returns_error_when_server_responds_with_non_ok_status", func(t *testing.T) {
		expectedErrorMessage := "Template not found"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(expectedErrorMessage))
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "nonexistent-template", values)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, httpdf.ErrNotOK)
		assert.Contains(t, err.Error(), "render template")
		assert.Contains(t, err.Error(), "(404)")
		assert.Contains(t, err.Error(), expectedErrorMessage)
	})

	t.Run("it_returns_error_when_server_responds_with_internal_server_error", func(t *testing.T) {
		expectedErrorMessage := "Internal server error occurred"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(expectedErrorMessage))
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		result, err := client.Render(context.Background(), "test-template", values)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, httpdf.ErrNotOK)
		assert.Contains(t, err.Error(), "(500)")
		assert.Contains(t, err.Error(), expectedErrorMessage)
	})

	t.Run("it_handles_context_cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate slow response
			select {
			case <-r.Context().Done():
				return
			}
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"name": "World"}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := client.Render(ctx, "test-template", values)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "render template")
	})

	t.Run("it_constructs_correct_url_path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/templates/my-custom-template/render", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"test": "data"}

		result, err := client.Render(context.Background(), "my-custom-template", values)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		result.Close()
	})

	t.Run("it_handles_template_names_with_special_characters", func(t *testing.T) {
		templateName := "template-with_special.chars"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expectedPath := fmt.Sprintf("/templates/%s/render", templateName)
			assert.Equal(t, expectedPath, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		}))
		defer server.Close()

		client := httpdf.NewClient(server.URL)
		values := map[string]any{"test": "data"}

		result, err := client.Render(context.Background(), templateName, values)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		result.Close()
	})

	t.Run("it_uses_custom_http_client_when_provided", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify custom header set by custom transport
			assert.Equal(t, "custom-client", r.Header.Get("X-Custom-Client"))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		}))
		defer server.Close()

		// Create custom HTTP client with custom transport
		customClient := &http.Client{
			Transport: &customTransport{},
		}

		client := httpdf.NewClient(server.URL, httpdf.WithHTTPClient(customClient))
		values := map[string]any{"test": "data"}

		result, err := client.Render(context.Background(), "test-template", values)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		result.Close()
	})
}

func TestWithHTTPClient(t *testing.T) {
	t.Run("it_creates_client_option_that_sets_custom_http_client", func(t *testing.T) {
		customHTTPClient := &http.Client{}
		option := httpdf.WithHTTPClient(customHTTPClient)

		assert.NotNil(t, option)
		// The option function itself should not be nil
		// The actual functionality is tested in the integration tests above
	})
}

// customTransport is a test transport that adds a custom header
type customTransport struct{}

func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-Custom-Client", "custom-client")
	return http.DefaultTransport.RoundTrip(req)
}

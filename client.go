package httpdf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

var (
	ErrNotOK = fmt.Errorf("non-OK response status")
)

// Client is a simple HTTP client for the HTTPPDF service.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new HTTPPDF client for the given base URL
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{},
		baseURL:    strings.TrimRight(baseURL, "/"),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Render given template using the provided values
func (c *Client) Render(ctx context.Context, template string, values any) (io.ReadCloser, error) {
	url := c.baseURL + "/" + path.Join("templates", template, "render")

	body := bytes.NewBuffer(nil)
	if err := json.NewEncoder(body).Encode(values); err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		resBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("render template: %w (%d): %s", ErrNotOK, res.StatusCode, resBody)
	}

	return res.Body, nil
}

// ClientOption is a function that configures the HTTPPDF client
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client for the HTTPPDF client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

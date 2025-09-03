package httpdf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
func (c *Client) Render(ctx context.Context, template string, values any, lang ...string) (io.ReadCloser, error) {
	u := c.baseURL + "/" + path.Join("templates", template, "render")

	// Add lang query parameter if provided
	if len(lang) > 0 && lang[0] != "" {
		parsedURL, err := url.Parse(u)
		if err != nil {
			return nil, fmt.Errorf("render template: %w", err)
		}
		q := parsedURL.Query()
		q.Set("lang", lang[0])
		parsedURL.RawQuery = q.Encode()
		u = parsedURL.String()
	}

	body := bytes.NewBuffer(nil)
	if err := json.NewEncoder(body).Encode(values); err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
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

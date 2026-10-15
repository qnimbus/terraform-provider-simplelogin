// SPDX-License-Identifier: MPL-2.0

// Package client is a small client for the subset of the SimpleLogin REST API
// used by the Terraform provider.
//
// API reference: https://github.com/simple-login/app/blob/master/docs/api.md
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the hosted SimpleLogin service. API paths are below /api.
	DefaultBaseURL = "https://app.simplelogin.io"

	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
	maxRetryAfter     = 60 * time.Second
	maxResponseBytes  = 10 << 20
)

// Client talks to the SimpleLogin API. It is safe for concurrent use.
type Client struct {
	baseURL    *url.URL
	apiKey     string
	userAgent  string
	httpClient *http.Client

	// maxRetries bounds retries of GET requests on 429/5xx/network errors.
	maxRetries int
	// backoff returns the delay before retry attempt n (1-based).
	backoff func(n int) time.Duration
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithMaxRetries sets how often a GET request is retried.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithBackoff overrides the retry delay function (used by tests).
func WithBackoff(f func(n int) time.Duration) Option { return func(c *Client) { c.backoff = f } }

// New creates a client. baseURL may be empty to use DefaultBaseURL.
func New(baseURL, apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("SimpleLogin API key is empty")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q: must be an absolute http(s) URL", baseURL)
	}

	c := &Client{
		baseURL:    u,
		apiKey:     apiKey,
		userAgent:  "terraform-provider-simplelogin",
		httpClient: &http.Client{Timeout: defaultTimeout},
		maxRetries: defaultMaxRetries,
		backoff: func(n int) time.Duration {
			return time.Duration(1<<(n-1)) * time.Second
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// do performs a request against path (relative to the base URL, starting with
// "/api/"), encoding body as JSON when non-nil and decoding a 2xx response
// into out when non-nil.
//
// Only GET requests are retried. Mutating requests are never retried because
// the original request may have been applied even if the response was lost.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.request(ctx, method, path, nil, body, out, method == http.MethodGet)
}

// request is do with explicit control over retries. Pass retryable=true only
// for requests without side effects.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body, out any, retryable bool) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
	}

	u := *c.baseURL
	u.Path = c.baseURL.Path + path
	u.RawQuery = query.Encode()

	for attempt := 0; ; attempt++ {
		resp, err := c.send(ctx, method, u.String(), payload)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if retryable && attempt < c.maxRetries {
				if werr := c.wait(ctx, c.backoff(attempt+1)); werr != nil {
					return werr
				}
				continue
			}
			return fmt.Errorf("SimpleLogin API %s %s: %w", method, path, err)
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if readErr != nil {
				return fmt.Errorf("SimpleLogin API %s %s: reading response: %w", method, path, readErr)
			}
			if out == nil {
				return nil
			}
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("SimpleLogin API %s %s: decoding response: %w", method, path, err)
			}
			return nil
		}

		apiErr := &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Message: errorMessage(respBody)}
		if retryable && attempt < c.maxRetries && isRetryableStatus(resp.StatusCode) {
			delay := c.backoff(attempt + 1)
			if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
				delay = ra
			}
			if delay > maxRetryAfter {
				return apiErr
			}
			if werr := c.wait(ctx, delay); werr != nil {
				return werr
			}
			continue
		}
		return apiErr
	}
}

func (c *Client) send(ctx context.Context, method, rawURL string, payload []byte) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authentication", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// url.Error includes the URL, which never contains the API key.
		return nil, err
	}
	return resp, nil
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// parseRetryAfter supports both delta-seconds and HTTP-date values.
func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// errorMessage extracts {"error": "..."} from a response body. Non-JSON bodies
// (e.g. HTML error pages from a proxy) are not echoed back.
func errorMessage(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	msg := strings.TrimSpace(e.Error)
	if len(msg) > maxErrorMessageLen {
		msg = msg[:maxErrorMessageLen] + "..."
	}
	return msg
}

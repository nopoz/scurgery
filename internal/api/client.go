// Package api talks to the Tailscale tailnet policy file endpoints. It knows
// nothing about merging: it fetches, validates and writes whole policies.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrPreconditionFailed reports that the policy changed between the read and
// the write, so the ETag no longer matched.
var ErrPreconditionFailed = errors.New("policy changed since it was read")

const defaultBaseURL = "https://api.tailscale.com"

type Client struct {
	BaseURL string
	HTTP    *http.Client

	token   string
	tailnet string
}

func New(token, tailnet string) *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		token:   token,
		tailnet: tailnet,
	}
}

func (c *Client) endpoint(suffix string) string {
	return fmt.Sprintf("%s/api/v2/tailnet/%s/acl%s", c.BaseURL, url.PathEscape(c.tailnet), suffix)
}

func (c *Client) do(ctx context.Context, method, url string, body []byte, hdr map[string]string) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return resp, b, nil
}

// serverMessage pulls the human-readable reason out of an error body.
func serverMessage(b []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &m); err == nil && m.Message != "" {
		return m.Message
	}
	return strings.TrimSpace(string(b))
}

// GetPolicy fetches the policy as HuJSON, preserving comments, along with the
// ETag needed to write it back conditionally.
func (c *Client) GetPolicy(ctx context.Context) ([]byte, string, error) {
	resp, body, err := c.do(ctx, http.MethodGet, c.endpoint(""), nil, map[string]string{
		"Accept": "application/hujson",
	})
	if err != nil {
		return nil, "", fmt.Errorf("fetching policy: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetching policy: HTTP %d: %s", resp.StatusCode, serverMessage(body))
	}
	return body, resp.Header.Get("ETag"), nil
}

// Validate checks a hypothetical policy without modifying anything.
//
// The endpoint returns HTTP 200 whether or not the policy is valid; an empty
// response body means it passed. A client checking only the status code will
// upload a broken policy believing it validated.
func (c *Client) Validate(ctx context.Context, p []byte) error {
	resp, body, err := c.do(ctx, http.MethodPost, c.endpoint("/validate"), p, map[string]string{
		"Content-Type": "application/hujson",
	})
	if err != nil {
		return fmt.Errorf("validating policy: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("validating policy: HTTP %d: %s", resp.StatusCode, serverMessage(body))
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	return fmt.Errorf("policy rejected: %s", serverMessage(body))
}

// SetPolicy writes the policy, conditional on etag still matching.
func (c *Client) SetPolicy(ctx context.Context, p []byte, etag string) error {
	hdr := map[string]string{"Content-Type": "application/hujson"}
	if etag != "" {
		hdr["If-Match"] = etag
	}
	resp, body, err := c.do(ctx, http.MethodPost, c.endpoint(""), p, hdr)
	if err != nil {
		return fmt.Errorf("writing policy: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusPreconditionFailed:
		return fmt.Errorf("%w: %s", ErrPreconditionFailed, serverMessage(body))
	default:
		return fmt.Errorf("writing policy: HTTP %d: %s", resp.StatusCode, serverMessage(body))
	}
}

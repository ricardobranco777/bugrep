// SPDX-License-Identifier: BSD-2-Clause

package redmine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// client is a small wrapper around http.Client for the Redmine REST API: it
// authenticates with the X-Redmine-API-Key header when a key is configured,
// and turns non-2xx responses into readable errors.
type client struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

func (c *client) newRequest(ctx context.Context, method, path string, query url.Values) (*http.Request, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Redmine-API-Key", c.apiKey)
	}
	return req, nil
}

func (c *client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("authentication failed (check the configured api key)")
	case resp.StatusCode == http.StatusForbidden:
		// Redmine also returns 403 for a project that doesn't exist or
		// isn't public, which isn't necessarily a bad credential.
		return fmt.Errorf("access denied (check the project/scope, or that the api key has access)")
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("not found")
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %s: %s", resp.Status, redmineErrorMessage(body))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// redmineErrorMessage extracts messages from Redmine's
// {"errors": ["...", "..."]} error shape, falling back to the raw body.
func redmineErrorMessage(body []byte) string {
	var e struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 {
		return strings.Join(e.Errors, "; ")
	}
	return strings.TrimSpace(string(body))
}

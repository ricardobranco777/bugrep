// SPDX-License-Identifier: BSD-2-Clause

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client is a small wrapper around http.Client for the GitHub REST API: it
// sets the standard headers, authenticates when a token is configured, and
// turns non-2xx responses into readable errors.
type client struct {
	http    *http.Client
	baseURL string // e.g. "https://api.github.com" or a GHE API base
	token   string
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
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// do executes req and decodes a successful JSON response into out (which
// may be nil to discard the body). Non-2xx responses are turned into an
// error describing what went wrong.
func (c *client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("authentication failed (check the configured token)")
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return fmt.Errorf("rate limit exceeded, resets at %s", rateLimitReset(resp))
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("not found")
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

func rateLimitReset(resp *http.Response) string {
	reset := resp.Header.Get("X-RateLimit-Reset")
	if reset == "" {
		return "unknown"
	}
	var unix int64
	if _, err := fmt.Sscanf(reset, "%d", &unix); err != nil {
		return reset
	}
	return time.Unix(unix, 0).Format(time.RFC3339)
}

// SPDX-License-Identifier: BSD-2-Clause

package launchpad

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// client is a small wrapper around http.Client for Launchpad's API.
// Launchpad has no supported authentication scheme in v1, so every request
// is anonymous; only public bugs are visible.
type client struct {
	http    *http.Client
	baseURL string // e.g. "https://api.launchpad.net/1.0"
}

// get issues a GET against baseURL+path with the given query parameters.
func (c *client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return c.getURL(ctx, u, out)
}

// getURL issues a GET against a fully-qualified URL, as used for the
// *_link fields Launchpad's hypermedia responses hand back (including
// next_collection_link for pagination).
func (c *client) getURL(ctx context.Context, fullURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

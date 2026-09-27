// SPDX-License-Identifier: BSD-2-Clause

package bugzilla

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// client is a small wrapper around http.Client for the Bugzilla REST API.
//
// Bugzilla doesn't follow ordinary HTTP error conventions: an auth failure
// or a bad query commonly comes back as HTTP 200 (or occasionally 400) with
// a JSON body of the form {"error": true, "message": "...", "code": N}, so
// do reads the whole body and checks for that shape regardless of status
// code, rather than branching on status codes the way the other backends
// do.
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
		req.Header.Set("X-BUGZILLA-API-KEY", c.apiKey)
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &statusError{Code: resp.StatusCode, Message: fmt.Sprintf("unexpected status %s: %s", resp.Status, bzErrorMessage(body))}
	}

	var probe struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.Error {
		return &statusError{Code: resp.StatusCode, Message: probe.Message}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// statusError carries the HTTP status code for a failed request, so callers
// can react to a specific code (e.g. "this endpoint doesn't exist here")
// without parsing the error text.
type statusError struct {
	Code    int
	Message string
}

func (e *statusError) Error() string { return e.Message }

func bzErrorMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return strings.TrimSpace(string(body))
}

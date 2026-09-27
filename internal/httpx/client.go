// SPDX-License-Identifier: BSD-2-Clause

// Package httpx provides the HTTP client shared by every backend: timeouts,
// retry with backoff on 429/5xx, and optional request/response debug
// logging with credentials redacted.
package httpx

import (
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Options configures a Client.
type Options struct {
	Timeout    time.Duration // overall per-request timeout; zero means no timeout
	MaxRetries int           // retries on 429/5xx; zero disables retrying
	Debug      bool          // log requests/responses to stderr, credentials redacted
	UserAgent  string
}

// DefaultOptions returns the options bugrep uses unless overridden.
func DefaultOptions() Options {
	return Options{
		Timeout:    20 * time.Second,
		MaxRetries: 3,
		UserAgent:  "bugrep/0 (+https://github.com/ricardobranco777/bugrep)",
	}
}

// NewClient builds an *http.Client with retry-with-backoff and (optionally)
// debug logging layered around the default transport.
func NewClient(opts Options) *http.Client {
	rt := http.DefaultTransport
	rt = &userAgentTransport{next: rt, userAgent: opts.UserAgent}
	if opts.Debug {
		rt = &debugTransport{next: rt}
	}
	rt = &retryTransport{next: rt, maxRetries: opts.MaxRetries}
	return &http.Client{
		Transport: rt,
		Timeout:   opts.Timeout,
	}
}

type userAgentTransport struct {
	next      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", t.userAgent)
	}
	return t.next.RoundTrip(req)
}

// retryTransport retries requests that fail with a 429 or 5xx response,
// using exponential backoff with jitter and honoring Retry-After when the
// server sends one.
type retryTransport struct {
	next       http.RoundTripper
	maxRetries int
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}

	var resp *http.Response
	var err error
	for attempt := 0; ; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err = t.next.RoundTrip(req)
		if attempt >= t.maxRetries {
			return resp, err
		}
		if err != nil {
			// Network errors are not retried here; the per-request timeout
			// and the caller's own context handle those.
			return resp, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return resp, nil
		}

		wait := retryDelay(resp, attempt)
		_ = resp.Body.Close()

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
}

func retryDelay(resp *http.Response, attempt int) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	base := time.Duration(1<<attempt) * 250 * time.Millisecond
	jitter := time.Duration(rand.Int64N(int64(base) / 2))
	return base + jitter
}

// redactedHeaders lists the headers that carry credentials across the
// backends bugrep talks to. debugTransport replaces their values with
// "REDACTED" before logging.
var redactedHeaders = []string{
	"Authorization", "X-Bugzilla-Api-Key", "X-Redmine-Api-Key", "Private-Token",
}

// debugTransport logs each request and response to stderr with credential
// headers redacted.
type debugTransport struct {
	next http.RoundTripper
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Fprintf(os.Stderr, "bugrep: %s %s %s\n", req.Method, req.URL, redactedHeaderSummary(req.Header))
	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bugrep: %s %s -> error: %v (%s)\n", req.Method, req.URL, err, time.Since(start))
		return resp, err
	}
	fmt.Fprintf(os.Stderr, "bugrep: %s %s -> %s (%s)\n", req.Method, req.URL, resp.Status, time.Since(start))
	return resp, err
}

// redactedHeaderSummary formats the request headers relevant for debugging
// (currently just User-Agent and Accept), replacing any credential header
// with "REDACTED" if present.
func redactedHeaderSummary(h http.Header) string {
	var parts []string
	for _, name := range redactedHeaders {
		if h.Get(name) != "" {
			parts = append(parts, name+": REDACTED")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

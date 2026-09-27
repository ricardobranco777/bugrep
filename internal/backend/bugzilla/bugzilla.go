// SPDX-License-Identifier: BSD-2-Clause

// Package bugzilla implements the bugrep Backend for Bugzilla (REST API
// 5.x), e.g. bugzilla.redhat.com or bugzilla.mozilla.org.
package bugzilla

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ricardobranco777/bugrep/internal/backend"
	"github.com/ricardobranco777/bugrep/internal/config"
	"github.com/ricardobranco777/bugrep/internal/core"
	"github.com/ricardobranco777/bugrep/internal/httpx"
)

func init() {
	backend.Register("bugzilla", New)
}

// Backend is the Bugzilla tracker implementation.
type Backend struct {
	name   string
	client *client
	login  string // from config `user`; if set, used directly for "me" instead of calling /rest/whoami

	meOnce  sync.Once
	meLogin string
	meErr   error
}

// New constructs a Bugzilla Backend from its configuration.
func New(_ context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	if tc.URL == "" {
		return nil, fmt.Errorf("bugzilla tracker %q: missing url", name)
	}

	apiKey, err := resolveAPIKey(tc)
	if err != nil {
		return nil, fmt.Errorf("bugzilla tracker %q: resolving api key: %w", name, err)
	}

	return &Backend{
		name: name,
		client: &client{
			http:    httpx.NewClient(httpOpts),
			baseURL: strings.TrimSuffix(tc.URL, "/"),
			apiKey:  apiKey,
		},
		login: tc.User,
	}, nil
}

// resolveAPIKey returns "" (unauthenticated) when no credential source is
// configured, since many Bugzilla instances (e.g. bugzilla.mozilla.org)
// allow anonymous read access to public bugs.
func resolveAPIKey(tc config.TrackerConfig) (string, error) {
	if tc.APIKeyEnv == "" && len(tc.APIKeyCmd) == 0 && tc.APIKeyKeyring == "" && tc.APIKey == "" {
		return "", nil
	}
	return config.ResolveCredential(tc.APIKeyEnv, tc.APIKeyCmd, tc.APIKeyKeyring, tc.APIKey)
}

func (b *Backend) Name() string { return b.name }
func (b *Backend) Type() string { return "bugzilla" }

func (b *Backend) Capabilities() core.Caps {
	return core.Caps{Labels: true, Component: true, Reporter: true}
}

// Check verifies connectivity and, when an API key is configured,
// authentication.
func (b *Backend) Check(ctx context.Context) error {
	if b.client.apiKey == "" {
		return b.client.getJSON(ctx, "/rest/version", nil, nil)
	}
	err := b.client.getJSON(ctx, "/rest/whoami", nil, nil)
	if err == nil {
		return nil
	}
	var se *statusError
	if errors.As(err, &se) && se.Code == http.StatusNotFound {
		// Some installs (e.g. bugzilla.suse.com) don't expose /rest/whoami
		// at all and 404 even for a valid key. Fall back to a plain
		// connectivity check rather than reporting a false authentication
		// failure.
		return b.client.getJSON(ctx, "/rest/version", nil, nil)
	}
	return err
}

// resolveMe resolves "me" to a login usable in assigned_to/creator filters.
// If the tracker's config sets `user`, that's used directly — no request
// needed, and it works even on instances (e.g. bugzilla.suse.com) that
// don't expose /rest/whoami at all. Otherwise it calls /rest/whoami once
// and caches the result (or error) for the Backend's lifetime.
func (b *Backend) resolveMe(ctx context.Context) (string, error) {
	if b.login != "" {
		return b.login, nil
	}
	b.meOnce.Do(func() {
		var who struct {
			Name string `json:"name"`
		}
		if err := b.client.getJSON(ctx, "/rest/whoami", nil, &who); err != nil {
			b.meErr = fmt.Errorf(`resolving "me": %w (set "user" in this tracker's config to avoid needing /rest/whoami)`, err)
			return
		}
		if who.Name == "" {
			b.meErr = fmt.Errorf(`resolving "me": bugzilla returned no login (is an api key configured?)`)
			return
		}
		b.meLogin = who.Name
	})
	return b.meLogin, b.meErr
}

const pageSize = 100

// Search runs q against /rest/bug, paginating with limit/offset until a
// page comes back short.
func (b *Backend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	resolved := q
	if q.Raw == "" {
		if q.Assignee == "me" {
			me, err := b.resolveMe(ctx)
			if err != nil {
				return nil, err
			}
			resolved.Assignee = me
		}
		if q.Reporter == "me" {
			me, err := b.resolveMe(ctx)
			if err != nil {
				return nil, err
			}
			resolved.Reporter = me
		}
	}

	vals, err := buildQuery(resolved)
	if err != nil {
		return nil, err
	}
	vals.Set("include_fields", strings.Join(bugzillaFields, ","))

	var issues []core.Issue
	for offset := 0; ; offset += pageSize {
		pageVals := vals.Clone()
		pageVals.Set("limit", strconv.Itoa(pageSize))
		pageVals.Set("offset", strconv.Itoa(offset))

		var resp struct {
			Bugs []bzBug `json:"bugs"`
		}
		if err := b.client.getJSON(ctx, "/rest/bug", pageVals, &resp); err != nil {
			return nil, fmt.Errorf("searching: %w", err)
		}
		for _, bug := range resp.Bugs {
			issues = append(issues, b.convertBug(bug))
		}
		if len(resp.Bugs) < pageSize {
			return issues, nil
		}
	}
}

// Get fetches a single bug by its numeric id.
func (b *Backend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	vals := url.Values{"include_fields": {strings.Join(bugzillaFields, ",")}}
	var resp struct {
		Bugs []bzBug `json:"bugs"`
	}
	if err := b.client.getJSON(ctx, "/rest/bug/"+url.PathEscape(key), vals, &resp); err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}
	if len(resp.Bugs) == 0 {
		return nil, fmt.Errorf("bug %s not found", key)
	}
	issue := b.convertBug(resp.Bugs[0])

	if withComments {
		comments, err := b.getComments(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("getting comments for %s: %w", key, err)
		}
		issue.Comments = comments
	}
	return &issue, nil
}

func (b *Backend) getComments(ctx context.Context, key string) ([]core.Comment, error) {
	var resp struct {
		Bugs map[string]struct {
			Comments []bzComment `json:"comments"`
		} `json:"bugs"`
	}
	if err := b.client.getJSON(ctx, "/rest/bug/"+url.PathEscape(key)+"/comment", nil, &resp); err != nil {
		return nil, err
	}
	entry, ok := resp.Bugs[key]
	if !ok {
		return nil, nil
	}
	comments := make([]core.Comment, len(entry.Comments))
	for i, c := range entry.Comments {
		comments[i] = core.Comment{Author: c.Creator, Created: c.CreationTime, Body: c.Text}
	}
	return comments, nil
}

// bzBug is the bug JSON shape returned by both the search and single-bug
// endpoints (both wrap their result in a top-level "bugs" array).
type bzBug struct {
	ID             int          `json:"id"`
	Summary        string       `json:"summary"`
	Status         string       `json:"status"`
	Resolution     string       `json:"resolution"`
	Priority       string       `json:"priority"`
	Severity       string       `json:"severity"`
	AssignedTo     string       `json:"assigned_to"`
	Creator        string       `json:"creator"`
	Keywords       []string     `json:"keywords"`
	Product        string       `json:"product"`
	Component      bzStringList `json:"component"`
	CreationTime   time.Time    `json:"creation_time"`
	LastChangeTime time.Time    `json:"last_change_time"`
}

// bzStringList unmarshals a field that's a plain string on stock Bugzilla
// (e.g. bugzilla.mozilla.org, bugzilla.suse.com) but a JSON array on Red
// Hat's fork, which supports multiple components per bug
// (bugzilla.redhat.com). Either shape becomes a []string.
type bzStringList []string

func (s *bzStringList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*s = nil
		return nil
	}
	if trimmed[0] == '[' {
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*s = arr
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	if str == "" {
		*s = nil
	} else {
		*s = []string{str}
	}
	return nil
}

type bzComment struct {
	Creator      string    `json:"creator"`
	CreationTime time.Time `json:"creation_time"`
	Text         string    `json:"text"`
}

func (b *Backend) convertBug(bug bzBug) core.Issue {
	// Bugzilla's internal "no resolution yet" value is the literal string
	// "---", though some configurations report it as an empty string.
	state := core.StateOpen
	if bug.Resolution != "" && bug.Resolution != "---" {
		state = core.StateClosed
	}

	status := bug.Status
	if state == core.StateClosed && bug.Resolution != "" {
		status = bug.Status + " (" + bug.Resolution + ")"
	}

	return core.Issue{
		Tracker:  b.name,
		Type:     "bugzilla",
		Key:      strconv.Itoa(bug.ID),
		Title:    bug.Summary,
		State:    state,
		Status:   status,
		Priority: bug.Priority,
		Severity: bug.Severity,
		Assignee: bug.AssignedTo,
		Reporter: bug.Creator,
		Labels:   bug.Keywords,
		Project:  bug.Product,
		Created:  bug.CreationTime,
		Updated:  bug.LastChangeTime,
		URL:      b.client.baseURL + "/show_bug.cgi?id=" + strconv.Itoa(bug.ID),
		Extra:    map[string]any{"components": []string(bug.Component)},
	}
}

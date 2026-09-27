// SPDX-License-Identifier: BSD-2-Clause

// Package redmine implements the bugrep Backend for Redmine.
package redmine

import (
	"context"
	"fmt"
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
	backend.Register("redmine", New)
}

// Backend is the Redmine tracker implementation.
type Backend struct {
	name   string
	client *client

	statusOnce     sync.Once
	closedStatuses map[int]bool // status id -> is_closed, from /issue_statuses.json
	statusErr      error
}

// New constructs a Redmine Backend from its configuration.
func New(_ context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	if tc.URL == "" {
		return nil, fmt.Errorf("redmine tracker %q: missing url", name)
	}

	apiKey, err := resolveAPIKey(tc)
	if err != nil {
		return nil, fmt.Errorf("redmine tracker %q: resolving api key: %w", name, err)
	}

	return &Backend{
		name: name,
		client: &client{
			http:    httpx.NewClient(httpOpts),
			baseURL: strings.TrimSuffix(tc.URL, "/"),
			apiKey:  apiKey,
		},
	}, nil
}

// resolveAPIKey returns "" (unauthenticated) when no credential source is
// configured, since many Redmine instances (e.g. redmine.org itself) allow
// anonymous read access to public projects.
func resolveAPIKey(tc config.TrackerConfig) (string, error) {
	if tc.APIKeyEnv == "" && len(tc.APIKeyCmd) == 0 && tc.APIKeyKeyring == "" && tc.APIKey == "" {
		return "", nil
	}
	return config.ResolveCredential(tc.APIKeyEnv, tc.APIKeyCmd, tc.APIKeyKeyring, tc.APIKey)
}

func (b *Backend) Name() string { return b.name }
func (b *Backend) Type() string { return "redmine" }

// Capabilities reports that labels aren't natively supported: Redmine has
// no built-in labels/tags concept (only per-install custom fields), so
// --label can't be translated into a filter.
func (b *Backend) Capabilities() core.Caps {
	return core.Caps{Labels: false, Component: false, Reporter: true}
}

// Check verifies connectivity and, when an API key is configured,
// authentication.
func (b *Backend) Check(ctx context.Context) error {
	if b.client.apiKey == "" {
		return b.client.getJSON(ctx, "/issue_statuses.json", nil, nil)
	}
	return b.client.getJSON(ctx, "/users/current.json", nil, nil)
}

// resolveClosedStatuses fetches, once, which status ids count as "closed"
// on this instance. Redmine issue objects only carry a {id, name} status,
// not an is_closed flag, so this is needed to label an issue's normalized
// State correctly whenever the search filter itself doesn't already pin it
// down (a state:all search, or a single Get). It's best-effort: on failure
// (e.g. an anonymous instance that doesn't expose this endpoint), callers
// fall back to labeling everything "open" rather than erroring the whole
// request.
func (b *Backend) resolveClosedStatuses(ctx context.Context) (map[int]bool, error) {
	b.statusOnce.Do(func() {
		var resp struct {
			IssueStatuses []struct {
				ID       int  `json:"id"`
				IsClosed bool `json:"is_closed"`
			} `json:"issue_statuses"`
		}
		if err := b.client.getJSON(ctx, "/issue_statuses.json", nil, &resp); err != nil {
			b.statusErr = err
			return
		}
		m := make(map[int]bool, len(resp.IssueStatuses))
		for _, s := range resp.IssueStatuses {
			m[s.ID] = s.IsClosed
		}
		b.closedStatuses = m
	})
	return b.closedStatuses, b.statusErr
}

const pageSize = 100

// Search runs q against /issues.json, paginating with limit/offset using
// the response's total_count.
func (b *Backend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	vals, err := buildQuery(q)
	if err != nil {
		return nil, err
	}

	// The state filter itself (status_id=open/closed) already tells us
	// every result's normalized State, except for state:all and --raw,
	// where individual issues can be either; only then is the extra
	// /issue_statuses.json lookup worth making.
	filterState := q.State
	var closedSet map[int]bool
	if q.Raw != "" || q.State == core.StateAll {
		filterState = core.StateAll
		closedSet, _ = b.resolveClosedStatuses(ctx) // best-effort
	}

	var issues []core.Issue
	for offset := 0; ; offset += pageSize {
		pageVals := vals.Clone()
		pageVals.Set("limit", strconv.Itoa(pageSize))
		pageVals.Set("offset", strconv.Itoa(offset))

		var resp struct {
			Issues     []redmineIssue `json:"issues"`
			TotalCount int            `json:"total_count"`
		}
		if err := b.client.getJSON(ctx, "/issues.json", pageVals, &resp); err != nil {
			return nil, fmt.Errorf("searching: %w", err)
		}
		for _, it := range resp.Issues {
			issues = append(issues, b.convertIssue(it, filterState, closedSet))
		}
		if offset+len(resp.Issues) >= resp.TotalCount || len(resp.Issues) == 0 {
			return issues, nil
		}
	}
}

// Get fetches a single issue by its numeric id. Unlike the other backends,
// Redmine returns comments (as "journals" with notes) inline on the issue
// itself via ?include=journals, so no second request is needed.
func (b *Backend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	vals := url.Values{}
	if withComments {
		vals.Set("include", "journals")
	}

	var resp struct {
		Issue redmineIssue `json:"issue"`
	}
	if err := b.client.getJSON(ctx, "/issues/"+url.PathEscape(key)+".json", vals, &resp); err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}

	closedSet, _ := b.resolveClosedStatuses(ctx) // best-effort
	issue := b.convertIssue(resp.Issue, core.StateAll, closedSet)

	if withComments {
		var comments []core.Comment
		for _, j := range resp.Issue.Journals {
			if j.Notes == "" {
				continue // a pure field-change entry (status/assignee/...), not a comment
			}
			comments = append(comments, core.Comment{Author: j.User.Name, Created: j.CreatedOn, Body: j.Notes})
		}
		issue.Comments = comments
	}
	return &issue, nil
}

type redmineNamed struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (n *redmineNamed) name() string {
	if n == nil {
		return ""
	}
	return n.Name
}

type redmineJournal struct {
	User      redmineNamed `json:"user"`
	Notes     string       `json:"notes"`
	CreatedOn time.Time    `json:"created_on"`
}

// redmineIssue is the issue JSON shape, shared by /issues.json's list and
// /issues/{id}.json's single-issue response (the latter adds Journals when
// ?include=journals is requested).
type redmineIssue struct {
	ID          int              `json:"id"`
	Subject     string           `json:"subject"`
	Description string           `json:"description"`
	Status      redmineNamed     `json:"status"`
	Priority    redmineNamed     `json:"priority"`
	Author      *redmineNamed    `json:"author"`
	AssignedTo  *redmineNamed    `json:"assigned_to"`
	Project     redmineNamed     `json:"project"`
	CreatedOn   time.Time        `json:"created_on"`
	UpdatedOn   time.Time        `json:"updated_on"`
	Journals    []redmineJournal `json:"journals,omitempty"`
}

// convertIssue normalizes a redmineIssue's State. When filterState is Open
// or Closed, every result of that search is known to match it already
// (Redmine applied the filter server-side); otherwise closedSet (which may
// be nil, if the lookup failed) is consulted, defaulting to Open when the
// status id isn't recognized.
func (b *Backend) convertIssue(it redmineIssue, filterState core.State, closedSet map[int]bool) core.Issue {
	state := core.StateOpen
	switch filterState {
	case core.StateOpen:
		state = core.StateOpen
	case core.StateClosed:
		state = core.StateClosed
	default:
		if closedSet != nil && closedSet[it.Status.ID] {
			state = core.StateClosed
		}
	}

	return core.Issue{
		Tracker:  b.name,
		Type:     "redmine",
		Key:      strconv.Itoa(it.ID),
		Title:    it.Subject,
		State:    state,
		Status:   it.Status.Name,
		Priority: it.Priority.Name,
		Assignee: it.AssignedTo.name(),
		Reporter: it.Author.name(),
		Project:  it.Project.Name,
		Created:  it.CreatedOn,
		Updated:  it.UpdatedOn,
		URL:      b.client.baseURL + "/issues/" + strconv.Itoa(it.ID),
		Body:     it.Description,
	}
}

// SPDX-License-Identifier: BSD-2-Clause

// Package gitlab implements the bugrep Backend for GitLab (gitlab.com or
// self-hosted) issues, scoped to configured groups and/or projects.
package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
	backend.Register("gitlab", New)
}

// Backend is the GitLab tracker implementation.
type Backend struct {
	name   string
	client *client
	scope  scope

	meOnce     sync.Once
	meUsername string
	meErr      error
}

// New constructs a GitLab Backend from its configuration. It requires the
// tracker to have at least one group or project configured; config.Validate
// enforces this before New is ever called, but the check here is cheap
// insurance for callers that build a Config by hand (as tests do).
func New(_ context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	if len(tc.Groups) == 0 && len(tc.Projects) == 0 {
		return nil, fmt.Errorf("gitlab tracker %q: no groups or projects configured", name)
	}

	token, err := resolveToken(tc)
	if err != nil {
		return nil, fmt.Errorf("gitlab tracker %q: resolving token: %w", name, err)
	}

	baseURL := strings.TrimSuffix(tc.URL, "/")
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}

	return &Backend{
		name: name,
		client: &client{
			http:    httpx.NewClient(httpOpts),
			baseURL: baseURL,
			token:   token,
		},
		scope: scope{Groups: tc.Groups, Projects: tc.Projects},
	}, nil
}

// resolveToken returns "" (unauthenticated) when no credential source is
// configured, rather than erroring, since public GitLab groups and projects
// can be searched without a token.
func resolveToken(tc config.TrackerConfig) (string, error) {
	if tc.TokenEnv == "" && len(tc.TokenCmd) == 0 && tc.TokenKeyring == "" && tc.Token == "" {
		return "", nil
	}
	return config.ResolveCredential(tc.TokenEnv, tc.TokenCmd, tc.TokenKeyring, tc.Token)
}

func (b *Backend) Name() string { return b.name }
func (b *Backend) Type() string { return "gitlab" }

func (b *Backend) Capabilities() core.Caps {
	return core.Caps{Labels: true, Component: false, Reporter: true}
}

// Check verifies connectivity and, when a token is configured,
// authentication.
func (b *Backend) Check(ctx context.Context) error {
	if b.client.token == "" {
		// /api/v4/version requires auth; list public projects instead so an
		// unauthenticated instance still gets a real connectivity check.
		req, err := b.client.newRequest(ctx, http.MethodGet, "/api/v4/projects", url.Values{"per_page": {"1"}})
		if err != nil {
			return err
		}
		return b.client.do(req, nil)
	}
	req, err := b.client.newRequest(ctx, http.MethodGet, "/api/v4/user", nil)
	if err != nil {
		return err
	}
	return b.client.do(req, nil)
}

// resolveMe resolves "me" to the authenticated user's username, once, and
// caches the result (or error) for the lifetime of the Backend.
func (b *Backend) resolveMe(ctx context.Context) (string, error) {
	b.meOnce.Do(func() {
		req, err := b.client.newRequest(ctx, http.MethodGet, "/api/v4/user", nil)
		if err != nil {
			b.meErr = err
			return
		}
		var u struct {
			Username string `json:"username"`
		}
		if err := b.client.do(req, &u); err != nil {
			b.meErr = fmt.Errorf("resolving \"me\": %w", err)
			return
		}
		if u.Username == "" {
			b.meErr = fmt.Errorf(`resolving "me": gitlab returned no username (is a token configured?)`)
			return
		}
		b.meUsername = u.Username
	})
	return b.meUsername, b.meErr
}

const pageSize = 100

// Search fans out across every configured group and project (or just the
// one named by q.Project) concurrently, and merges the results. A target
// that fails doesn't abort the others; Search only returns an error when
// every target failed.
func (b *Backend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	targets, err := searchTargets(b.scope, q.Project)
	if err != nil {
		return nil, err
	}

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

	vals, err := buildIssuesQuery(resolved)
	if err != nil {
		return nil, err
	}

	type outcome struct {
		issues []core.Issue
		err    error
		target target
	}
	outcomes := make([]outcome, len(targets))
	var wg sync.WaitGroup
	for i, tg := range targets {
		wg.Add(1)
		go func(i int, tg target) {
			defer wg.Done()
			issues, err := b.fetchIssues(ctx, tg, vals)
			outcomes[i] = outcome{issues: issues, err: err, target: tg}
		}(i, tg)
	}
	wg.Wait()

	var allIssues []core.Issue
	var failed int
	for _, o := range outcomes {
		if o.err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "bugrep: warning: %s: %s %s: %v\n", b.name, o.target.kind, o.target.id, o.err)
			continue
		}
		allIssues = append(allIssues, o.issues...)
	}
	if failed == len(targets) {
		return nil, fmt.Errorf("all %d configured group(s)/project(s) failed", len(targets))
	}
	return allIssues, nil
}

func (b *Backend) fetchIssues(ctx context.Context, tg target, vals url.Values) ([]core.Issue, error) {
	var issues []core.Issue
	for page := 1; ; page++ {
		pageVals := vals.Clone()
		pageVals.Set("per_page", strconv.Itoa(pageSize))
		pageVals.Set("page", strconv.Itoa(page))
		if tg.kind == "group" {
			pageVals.Set("include_subgroups", "true")
		}

		path := fmt.Sprintf("/api/v4/%ss/%s/issues", tg.kind, url.PathEscape(tg.id))
		req, err := b.client.newRequest(ctx, http.MethodGet, path, pageVals)
		if err != nil {
			return nil, err
		}
		var items []issueItem
		if err := b.client.do(req, &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			issues = append(issues, convertItem(b.name, it))
		}
		if len(items) < pageSize {
			return issues, nil
		}
	}
}

// Get fetches a single issue by its "group/project#iid" key.
func (b *Backend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	project, iid, err := core.SplitProjectKey(key)
	if err != nil {
		return nil, err
	}

	req, err := b.client.newRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v4/projects/%s/issues/%s", url.PathEscape(project), iid), nil)
	if err != nil {
		return nil, err
	}
	var item issueItem
	if err := b.client.do(req, &item); err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}

	issue := convertItem(b.name, item)

	if withComments {
		comments, err := b.getNotes(ctx, project, iid)
		if err != nil {
			return nil, fmt.Errorf("getting comments for %s: %w", key, err)
		}
		issue.Comments = comments
	}
	return &issue, nil
}

func (b *Backend) getNotes(ctx context.Context, project, iid string) ([]core.Comment, error) {
	var comments []core.Comment
	for page := 1; ; page++ {
		vals := url.Values{}
		vals.Set("per_page", strconv.Itoa(pageSize))
		vals.Set("page", strconv.Itoa(page))

		path := fmt.Sprintf("/api/v4/projects/%s/issues/%s/notes", url.PathEscape(project), iid)
		req, err := b.client.newRequest(ctx, http.MethodGet, path, vals)
		if err != nil {
			return nil, err
		}
		var items []noteItem
		if err := b.client.do(req, &items); err != nil {
			return nil, err
		}
		for _, n := range items {
			if n.System {
				continue // skip "changed status to closed" style system notes
			}
			comments = append(comments, core.Comment{Author: n.Author.Username, Created: n.CreatedAt, Body: n.Body})
		}
		if len(items) < pageSize {
			return comments, nil
		}
	}
}

// issueItem is the GitLab issue JSON shape, shared by the list endpoints
// and the single-issue endpoint.
type issueItem struct {
	IID         int         `json:"iid"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	State       string      `json:"state"` // "opened" or "closed"
	Labels      []string    `json:"labels"`
	Author      glUserRef   `json:"author"`
	Assignee    *glUserRef  `json:"assignee"`
	Assignees   []glUserRef `json:"assignees"`
	WebURL      string      `json:"web_url"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	References  struct {
		Full string `json:"full"` // e.g. "group/project#17"
	} `json:"references"`
}

type glUserRef struct {
	Username string `json:"username"`
}

type noteItem struct {
	Author    glUserRef `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"body"`
	System    bool      `json:"system"`
}

func convertItem(trackerName string, it issueItem) core.Issue {
	key := it.References.Full
	if key == "" {
		key = fmt.Sprintf("unknown#%d", it.IID) // defensive fallback; the API always sets this
	}
	project := strings.TrimSuffix(key, fmt.Sprintf("#%d", it.IID))

	assignee := ""
	switch {
	case len(it.Assignees) > 0:
		assignee = it.Assignees[0].Username
	case it.Assignee != nil:
		assignee = it.Assignee.Username
	}

	state := core.StateClosed
	if it.State == "opened" {
		state = core.StateOpen
	}

	return core.Issue{
		Tracker:  trackerName,
		Type:     "gitlab",
		Key:      key,
		Title:    it.Title,
		State:    state,
		Status:   it.State,
		Assignee: assignee,
		Reporter: it.Author.Username,
		Labels:   it.Labels,
		Project:  project,
		Created:  it.CreatedAt,
		Updated:  it.UpdatedAt,
		URL:      it.WebURL,
		Body:     it.Description,
	}
}

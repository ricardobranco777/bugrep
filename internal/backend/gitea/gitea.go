// SPDX-License-Identifier: BSD-2-Clause

// Package gitea implements the bugrep Backend for Gitea and Forgejo (a
// Gitea fork that keeps the same /api/v1 REST shape), scoped to configured
// orgs and/or repos.
package gitea

import (
	"context"
	"fmt"
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
	backend.Register("gitea", New)
	backend.Register("forgejo", New)
}

// Backend is the Gitea/Forgejo tracker implementation. typ is "gitea" or
// "forgejo", exactly as configured, since they're otherwise identical.
type Backend struct {
	name   string
	typ    string
	client *client
	scope  scope

	meOnce  sync.Once
	meLogin string
	meErr   error
}

// New constructs a Gitea/Forgejo Backend from its configuration.
func New(_ context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	if len(tc.Orgs) == 0 && len(tc.Repos) == 0 {
		return nil, fmt.Errorf("%s tracker %q: no orgs or repos configured", tc.Type, name)
	}
	if tc.URL == "" {
		return nil, fmt.Errorf("%s tracker %q: missing url", tc.Type, name)
	}

	token, err := resolveToken(tc)
	if err != nil {
		return nil, fmt.Errorf("%s tracker %q: resolving token: %w", tc.Type, name, err)
	}

	return &Backend{
		name: name,
		typ:  tc.Type,
		client: &client{
			http:    httpx.NewClient(httpOpts),
			baseURL: strings.TrimSuffix(tc.URL, "/"),
			token:   token,
		},
		scope: scope{Orgs: tc.Orgs, Repos: tc.Repos},
	}, nil
}

// resolveToken returns "" (unauthenticated) when no credential source is
// configured, since public repos can be searched without a token.
func resolveToken(tc config.TrackerConfig) (string, error) {
	if tc.TokenEnv == "" && len(tc.TokenCmd) == 0 && tc.TokenKeyring == "" && tc.Token == "" {
		return "", nil
	}
	return config.ResolveCredential(tc.TokenEnv, tc.TokenCmd, tc.TokenKeyring, tc.Token)
}

func (b *Backend) Name() string { return b.name }
func (b *Backend) Type() string { return b.typ }

func (b *Backend) Capabilities() core.Caps {
	return core.Caps{Labels: true, Component: false, Reporter: true}
}

// Check verifies connectivity and, when a token is configured,
// authentication.
func (b *Backend) Check(ctx context.Context) error {
	if b.client.token == "" {
		return b.client.getJSON(ctx, "/version", nil, nil)
	}
	return b.client.getJSON(ctx, "/user", nil, nil)
}

// resolveMe resolves "me" to the authenticated user's login, once, and
// caches the result (or error) for the lifetime of the Backend.
func (b *Backend) resolveMe(ctx context.Context) (string, error) {
	b.meOnce.Do(func() {
		var u struct {
			Login string `json:"login"`
		}
		if err := b.client.getJSON(ctx, "/user", nil, &u); err != nil {
			b.meErr = fmt.Errorf(`resolving "me": %w`, err)
			return
		}
		if u.Login == "" {
			b.meErr = fmt.Errorf(`resolving "me": no login returned (is a token configured?)`)
			return
		}
		b.meLogin = u.Login
	})
	return b.meLogin, b.meErr
}

const pageSize = 50

// Search fans out across every configured org and repo (or just the one
// named by q.Project) concurrently, and merges the results. A target that
// fails doesn't abort the others; Search only returns an error when every
// target failed.
func (b *Backend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	targets, err := searchTargets(b.scope, q.Project)
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
			var issues []core.Issue
			var err error
			if tg.kind == "repo" {
				issues, err = b.searchRepo(ctx, tg.id, q)
			} else {
				issues, err = b.searchOrg(ctx, tg.id, q)
			}
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
		return nil, fmt.Errorf("all %d configured org(s)/repo(s) failed", len(targets))
	}
	return allIssues, nil
}

func (b *Backend) searchRepo(ctx context.Context, project string, q core.Query) ([]core.Issue, error) {
	resolved := q
	if resolved.Assignee == "me" {
		me, err := b.resolveMe(ctx)
		if err != nil {
			return nil, err
		}
		resolved.Assignee = me
	}
	if resolved.Reporter == "me" {
		me, err := b.resolveMe(ctx)
		if err != nil {
			return nil, err
		}
		resolved.Reporter = me
	}

	vals, err := buildRepoQuery(resolved)
	if err != nil {
		return nil, err
	}
	owner, repo, ok := strings.Cut(project, "/")
	if !ok {
		return nil, fmt.Errorf("invalid repo %q: want owner/repo", project)
	}

	var issues []core.Issue
	for page := 1; ; page++ {
		var items []issueItem
		if err := b.client.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/issues", owner, repo), pageParams(vals, page, pageSize), &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.PullRequest != nil {
				continue
			}
			issues = append(issues, b.convertItem(it))
		}
		if len(items) < pageSize {
			return issues, nil
		}
	}
}

func (b *Backend) searchOrg(ctx context.Context, org string, q core.Query) ([]core.Issue, error) {
	vals, skippedAssignee, skippedReporter := buildOrgQuery(q)
	if skippedAssignee {
		fmt.Fprintf(os.Stderr, "bugrep: warning: %s: org %s: --assignee %q not applied; org-wide search only supports \"me\" (scope to one repo with -p to filter by a specific user)\n", b.name, org, q.Assignee)
	}
	if skippedReporter {
		fmt.Fprintf(os.Stderr, "bugrep: warning: %s: org %s: --reporter %q not applied; org-wide search only supports \"me\" (scope to one repo with -p to filter by a specific user)\n", b.name, org, q.Reporter)
	}
	vals.Set("owner", org)

	var issues []core.Issue
	for page := 1; ; page++ {
		var items []issueItem
		if err := b.client.getJSON(ctx, "/repos/issues/search", pageParams(vals, page, pageSize), &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.PullRequest != nil {
				continue
			}
			issues = append(issues, b.convertItem(it))
		}
		if len(items) < pageSize {
			return issues, nil
		}
	}
}

// Get fetches a single issue by its "owner/repo#number" key.
func (b *Backend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	project, number, err := core.SplitProjectKey(key)
	if err != nil {
		return nil, err
	}
	owner, repo, ok := strings.Cut(project, "/")
	if !ok {
		return nil, fmt.Errorf("invalid gitea issue key %q: want owner/repo#number", key)
	}

	var item issueItem
	if err := b.client.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/issues/%s", owner, repo, number), nil, &item); err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}
	issue := b.convertItem(item)

	if withComments {
		comments, err := b.getComments(ctx, owner, repo, number)
		if err != nil {
			return nil, fmt.Errorf("getting comments for %s: %w", key, err)
		}
		issue.Comments = comments
	}
	return &issue, nil
}

func (b *Backend) getComments(ctx context.Context, owner, repo, number string) ([]core.Comment, error) {
	var comments []core.Comment
	for page := 1; ; page++ {
		vals := pageParams(nil, page, pageSize)
		var items []commentItem
		if err := b.client.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/issues/%s/comments", owner, repo, number), vals, &items); err != nil {
			return nil, err
		}
		for _, c := range items {
			comments = append(comments, core.Comment{Author: c.User.Login, Created: c.CreatedAt, Body: c.Body})
		}
		if len(items) < pageSize {
			return comments, nil
		}
	}
}

type giteaLabel struct {
	Name string `json:"name"`
}

type giteaUserRef struct {
	Login string `json:"login"`
}

type giteaRepoRef struct {
	FullName string `json:"full_name"`
}

// issueItem is shared by the repo-scoped issues endpoint, the org-wide
// search endpoint, and the single-issue endpoint; only Repository is
// missing on the single-issue and repo-scoped responses, since the caller
// already knows which repo it asked about there.
type issueItem struct {
	Number      int            `json:"number"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	State       string         `json:"state"` // "open" or "closed"
	HTMLURL     string         `json:"html_url"`
	Labels      []giteaLabel   `json:"labels"`
	User        giteaUserRef   `json:"user"`
	Assignee    *giteaUserRef  `json:"assignee"`
	Assignees   []giteaUserRef `json:"assignees"`
	Repository  *giteaRepoRef  `json:"repository"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	PullRequest *struct{}      `json:"pull_request,omitempty"`
}

type commentItem struct {
	User      giteaUserRef `json:"user"`
	Body      string       `json:"body"`
	CreatedAt time.Time    `json:"created_at"`
}

func (b *Backend) convertItem(it issueItem) core.Issue {
	project := ""
	if it.Repository != nil {
		project = it.Repository.FullName
	}
	key := it.Number
	ref := strconv.Itoa(key)
	if project != "" {
		ref = project + "#" + ref
	}

	assignee := ""
	switch {
	case len(it.Assignees) > 0:
		assignee = it.Assignees[0].Login
	case it.Assignee != nil:
		assignee = it.Assignee.Login
	}

	labels := make([]string, len(it.Labels))
	for i, l := range it.Labels {
		labels[i] = l.Name
	}

	return core.Issue{
		Tracker:  b.name,
		Type:     b.typ,
		Key:      ref,
		Title:    it.Title,
		State:    core.State(it.State),
		Status:   it.State,
		Assignee: assignee,
		Reporter: it.User.Login,
		Labels:   labels,
		Project:  project,
		Created:  it.CreatedAt,
		Updated:  it.UpdatedAt,
		URL:      it.HTMLURL,
		Body:     it.Body,
	}
}

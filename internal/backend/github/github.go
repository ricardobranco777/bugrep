// SPDX-License-Identifier: BSD-2-Clause

// Package github implements the bugrep Backend for GitHub (and GitHub
// Enterprise) issues, scoped to configured orgs and/or repos.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ricardobranco777/bugrep/internal/backend"
	"github.com/ricardobranco777/bugrep/internal/config"
	"github.com/ricardobranco777/bugrep/internal/core"
	"github.com/ricardobranco777/bugrep/internal/httpx"
)

func init() {
	backend.Register("github", New)
}

// Backend is the GitHub tracker implementation.
type Backend struct {
	name   string
	client *client
	scope  scope
}

// New constructs a GitHub Backend from its configuration. It requires the
// tracker to have at least one org or repo configured; config.Validate
// enforces this before New is ever called, but the check here is cheap
// insurance for callers that build a Config by hand (as tests do).
func New(_ context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	if len(tc.Orgs) == 0 && len(tc.Repos) == 0 {
		return nil, fmt.Errorf("github tracker %q: no orgs or repos configured", name)
	}

	token, err := resolveToken(tc)
	if err != nil {
		return nil, fmt.Errorf("github tracker %q: resolving token: %w", name, err)
	}

	baseURL := strings.TrimSuffix(tc.URL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	return &Backend{
		name: name,
		client: &client{
			http:    httpx.NewClient(httpOpts),
			baseURL: baseURL,
			token:   token,
		},
		scope: scope{Orgs: tc.Orgs, Repos: tc.Repos},
	}, nil
}

// resolveToken returns "" (unauthenticated) when no credential source is
// configured, rather than erroring, since GitHub search works against
// public repos without a token (at a lower rate limit).
func resolveToken(tc config.TrackerConfig) (string, error) {
	if tc.TokenEnv == "" && len(tc.TokenCmd) == 0 && tc.TokenKeyring == "" && tc.Token == "" {
		return "", nil
	}
	return config.ResolveCredential(tc.TokenEnv, tc.TokenCmd, tc.TokenKeyring, tc.Token)
}

func (b *Backend) Name() string { return b.name }
func (b *Backend) Type() string { return "github" }

func (b *Backend) Capabilities() core.Caps {
	return core.Caps{Labels: true, Component: false, Reporter: true}
}

// Check verifies connectivity and, when a token is configured,
// authentication.
func (b *Backend) Check(ctx context.Context) error {
	path := "/rate_limit"
	if b.client.token != "" {
		path = "/user"
	}
	req, err := b.client.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return b.client.do(req, nil)
}

const (
	searchPageSize = 100
	searchHardCap  = 1000 // GitHub's search API never returns more than this
)

// Search runs q against the GitHub search API, scoped to this instance's
// configured orgs/repos, and pages through every result up to GitHub's
// 1000-result search cap.
func (b *Backend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	queryStr, err := buildSearchQuery(q, b.scope)
	if err != nil {
		return nil, err
	}
	sortParam, orderParam := sortParams(q.Sort)

	var issues []core.Issue
	for page := 1; (page-1)*searchPageSize < searchHardCap; page++ {
		vals := url.Values{}
		vals.Set("q", queryStr)
		vals.Set("per_page", strconv.Itoa(searchPageSize))
		vals.Set("page", strconv.Itoa(page))
		if sortParam != "" {
			vals.Set("sort", sortParam)
			vals.Set("order", orderParam)
		}

		req, err := b.client.newRequest(ctx, http.MethodGet, "/search/issues", vals)
		if err != nil {
			return nil, err
		}
		var sr searchResponse
		if err := b.client.do(req, &sr); err != nil {
			return nil, fmt.Errorf("searching: %w", err)
		}

		for _, item := range sr.Items {
			if item.PullRequest != nil {
				continue // is:issue should already exclude these; skip defensively
			}
			issues = append(issues, convertItem(b.name, item))
		}

		if len(sr.Items) < searchPageSize {
			return issues, nil
		}
		if page*searchPageSize >= searchHardCap && sr.TotalCount > searchHardCap {
			fmt.Fprintf(os.Stderr, "bugrep: warning: %s: GitHub's search API caps results at %d of %d matches; refine the query to see the rest\n", b.name, searchHardCap, sr.TotalCount)
		}
	}
	return issues, nil
}

// Get fetches a single issue by its "owner/repo#number" key.
func (b *Backend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	project, number, err := core.SplitProjectKey(key)
	if err != nil {
		return nil, err
	}
	owner, repo, ok := strings.Cut(project, "/")
	if !ok {
		return nil, fmt.Errorf("invalid github issue key %q: want owner/repo#number", key)
	}

	req, err := b.client.newRequest(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/issues/%s", owner, repo, number), nil)
	if err != nil {
		return nil, err
	}
	var item issueItem
	if err := b.client.do(req, &item); err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}

	issue := convertItem(b.name, item)

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
	const pageSize = 100
	var comments []core.Comment
	for page := 1; ; page++ {
		vals := url.Values{}
		vals.Set("per_page", strconv.Itoa(pageSize))
		vals.Set("page", strconv.Itoa(page))

		req, err := b.client.newRequest(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/issues/%s/comments", owner, repo, number), vals)
		if err != nil {
			return nil, err
		}
		var items []commentItem
		if err := b.client.do(req, &items); err != nil {
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

// searchResponse is the GitHub search/issues response shape.
type searchResponse struct {
	TotalCount int         `json:"total_count"`
	Items      []issueItem `json:"items"`
}

// issueItem is shared by the search response's "items" and the single-issue
// endpoint response: both return the same issue shape, including
// repository_url.
type issueItem struct {
	Number        int         `json:"number"`
	Title         string      `json:"title"`
	State         string      `json:"state"`
	StateReason   string      `json:"state_reason"`
	HTMLURL       string      `json:"html_url"`
	Labels        []ghLabel   `json:"labels"`
	User          ghUserRef   `json:"user"`
	Assignee      *ghUserRef  `json:"assignee"`
	Assignees     []ghUserRef `json:"assignees"`
	RepositoryURL string      `json:"repository_url"`
	Body          string      `json:"body"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	PullRequest   *struct{}   `json:"pull_request,omitempty"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUserRef struct {
	Login string `json:"login"`
}

type commentItem struct {
	User      ghUserRef `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"body"`
}

func convertItem(trackerName string, item issueItem) core.Issue {
	owner, repo := parseRepositoryURL(item.RepositoryURL)
	project := owner + "/" + repo
	key := fmt.Sprintf("%s#%d", project, item.Number)

	assignee := ""
	switch {
	case len(item.Assignees) > 0:
		assignee = item.Assignees[0].Login
	case item.Assignee != nil:
		assignee = item.Assignee.Login
	}

	labels := make([]string, len(item.Labels))
	for i, l := range item.Labels {
		labels[i] = l.Name
	}

	status := item.State
	if item.State == "closed" && item.StateReason != "" {
		status = item.StateReason
	}

	return core.Issue{
		Tracker:  trackerName,
		Type:     "github",
		Key:      key,
		Title:    item.Title,
		State:    core.State(item.State),
		Status:   status,
		Assignee: assignee,
		Reporter: item.User.Login,
		Labels:   labels,
		Project:  project,
		Created:  item.CreatedAt,
		Updated:  item.UpdatedAt,
		URL:      item.HTMLURL,
		Body:     item.Body,
	}
}

// parseRepositoryURL extracts "owner", "repo" from
// "https://api.github.com/repos/owner/repo".
func parseRepositoryURL(u string) (owner, repo string) {
	parts := strings.Split(strings.TrimSuffix(u, "/"), "/")
	if len(parts) < 2 {
		return "", ""
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}

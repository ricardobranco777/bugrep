// SPDX-License-Identifier: BSD-2-Clause

// Package jira implements the bugrep Backend for Jira, supporting both
// Jira Cloud and Jira Server/Data Center (the "server" flavor covers both,
// since they share the same v2 REST API and JQL grammar).
package jira

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ricardobranco777/bugrep/internal/backend"
	"github.com/ricardobranco777/bugrep/internal/config"
	"github.com/ricardobranco777/bugrep/internal/core"
	"github.com/ricardobranco777/bugrep/internal/httpx"
)

func init() {
	backend.Register("jira", New)
}

// Backend is the Jira tracker implementation, for either flavor.
type Backend struct {
	name    string
	flavor  string // "cloud" or "server"
	apiBase string // "/rest/api/3" (cloud) or "/rest/api/2" (server)
	client  *client
}

// New constructs a Jira Backend from its configuration.
func New(_ context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	if tc.Flavor != "cloud" && tc.Flavor != "server" {
		return nil, fmt.Errorf(`jira tracker %q: flavor must be "cloud" or "server"`, name)
	}
	if tc.URL == "" {
		return nil, fmt.Errorf("jira tracker %q: missing url", name)
	}

	token, err := resolveToken(tc)
	if err != nil {
		return nil, fmt.Errorf("jira tracker %q: resolving token: %w", name, err)
	}

	authHeader, err := buildAuthHeader(tc.Flavor, tc.User, token)
	if err != nil {
		return nil, fmt.Errorf("jira tracker %q: %w", name, err)
	}

	apiBase := "/rest/api/2"
	if tc.Flavor == "cloud" {
		apiBase = "/rest/api/3"
	}

	return &Backend{
		name:    name,
		flavor:  tc.Flavor,
		apiBase: apiBase,
		client: &client{
			http:       httpx.NewClient(httpOpts),
			baseURL:    strings.TrimSuffix(tc.URL, "/"),
			authHeader: authHeader,
		},
	}, nil
}

// resolveToken returns "" (unauthenticated) when no credential source is
// configured, since some Jira instances (e.g. issues.apache.org) allow
// anonymous read access to public projects.
func resolveToken(tc config.TrackerConfig) (string, error) {
	if tc.TokenEnv == "" && len(tc.TokenCmd) == 0 && tc.TokenKeyring == "" && tc.Token == "" {
		return "", nil
	}
	return config.ResolveCredential(tc.TokenEnv, tc.TokenCmd, tc.TokenKeyring, tc.Token)
}

// buildAuthHeader computes the Authorization header value for a flavor:
//   - cloud always uses Basic auth with the account email and an API token.
//   - server uses Basic auth when a user is configured alongside the token
//     (username + password/PAT), or Bearer auth for a bare personal access
//     token (Jira 8.14+).
func buildAuthHeader(flavor, user, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	switch flavor {
	case "cloud":
		if user == "" {
			return "", fmt.Errorf(`cloud requires "user" (the account email) alongside a token`)
		}
		return basicAuth(user, token), nil
	case "server":
		if user != "" {
			return basicAuth(user, token), nil
		}
		return "Bearer " + token, nil
	default:
		return "", fmt.Errorf("unknown flavor %q", flavor)
	}
}

func basicAuth(user, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+token))
}

func (b *Backend) Name() string { return b.name }
func (b *Backend) Type() string { return "jira" }

func (b *Backend) Capabilities() core.Caps {
	return core.Caps{Labels: true, Component: true, Reporter: true}
}

// Check verifies connectivity and, when a token is configured,
// authentication.
func (b *Backend) Check(ctx context.Context) error {
	if b.client.authHeader == "" {
		// /myself requires auth on both flavors; serverInfo works
		// anonymously and still confirms the instance is reachable.
		return b.client.getJSON(ctx, b.apiBase+"/serverInfo", nil, nil)
	}
	return b.client.getJSON(ctx, b.apiBase+"/myself", nil, nil)
}

var searchFields = []string{
	"summary", "status", "assignee", "reporter", "labels",
	"project", "created", "updated", "priority", "description", "components",
}

const pageSize = 100

// Search runs q as JQL, paginating according to the instance's flavor:
// Cloud's newer /search/jql endpoint uses a forward-only nextPageToken
// cursor, while Server/Data Center's /search endpoint uses startAt/total.
func (b *Backend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	jql := buildJQL(q)
	if b.flavor == "cloud" {
		return b.searchCloud(ctx, jql)
	}
	return b.searchServer(ctx, jql)
}

func (b *Backend) searchCloud(ctx context.Context, jql string) ([]core.Issue, error) {
	var issues []core.Issue
	var token string
	for {
		reqBody := map[string]any{"jql": jql, "maxResults": pageSize, "fields": searchFields}
		if token != "" {
			reqBody["nextPageToken"] = token
		}
		var resp struct {
			Issues        []jiraIssue `json:"issues"`
			IsLast        bool        `json:"isLast"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := b.client.postJSON(ctx, b.apiBase+"/search/jql", reqBody, &resp); err != nil {
			return nil, fmt.Errorf("searching: %w", err)
		}
		for _, it := range resp.Issues {
			issues = append(issues, b.convertIssue(it))
		}
		if resp.IsLast || resp.NextPageToken == "" {
			return issues, nil
		}
		token = resp.NextPageToken
	}
}

func (b *Backend) searchServer(ctx context.Context, jql string) ([]core.Issue, error) {
	var issues []core.Issue
	startAt := 0
	for {
		reqBody := map[string]any{"jql": jql, "startAt": startAt, "maxResults": pageSize, "fields": searchFields}
		var resp struct {
			Total  int         `json:"total"`
			Issues []jiraIssue `json:"issues"`
		}
		if err := b.client.postJSON(ctx, b.apiBase+"/search", reqBody, &resp); err != nil {
			return nil, fmt.Errorf("searching: %w", err)
		}
		for _, it := range resp.Issues {
			issues = append(issues, b.convertIssue(it))
		}
		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			return issues, nil
		}
	}
}

// Get fetches a single issue by its native key, e.g. "PROJ-123".
func (b *Backend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	vals := url.Values{"fields": {strings.Join(searchFields, ",")}}
	var item jiraIssue
	if err := b.client.getJSON(ctx, b.apiBase+"/issue/"+url.PathEscape(key), vals, &item); err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}

	issue := b.convertIssue(item)

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
	var comments []core.Comment
	startAt := 0
	for {
		vals := url.Values{}
		vals.Set("startAt", strconv.Itoa(startAt))
		vals.Set("maxResults", strconv.Itoa(pageSize))

		var resp struct {
			Total    int           `json:"total"`
			Comments []jiraComment `json:"comments"`
		}
		if err := b.client.getJSON(ctx, b.apiBase+"/issue/"+url.PathEscape(key)+"/comment", vals, &resp); err != nil {
			return nil, err
		}
		for _, c := range resp.Comments {
			comments = append(comments, core.Comment{Author: c.Author.name(), Created: c.Created.Time(), Body: string(c.Body)})
		}
		startAt += len(resp.Comments)
		if len(resp.Comments) == 0 || startAt >= resp.Total {
			return comments, nil
		}
	}
}

// jiraIssue is the issue JSON shape returned by both the search and
// single-issue endpoints, on both API versions.
type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary  string     `json:"summary"`
		Status   jiraStatus `json:"status"`
		Assignee *jiraUser  `json:"assignee"`
		Reporter *jiraUser  `json:"reporter"`
		Labels   []string   `json:"labels"`
		Project  struct {
			Key string `json:"key"`
		} `json:"project"`
		Created     jiraTime    `json:"created"`
		Updated     jiraTime    `json:"updated"`
		Priority    *jiraNamed  `json:"priority"`
		Description richText    `json:"description"`
		Components  []jiraNamed `json:"components"`
	} `json:"fields"`
}

type jiraStatus struct {
	Name           string `json:"name"`
	StatusCategory struct {
		Key string `json:"key"` // "new", "indeterminate", "done"
	} `json:"statusCategory"`
}

type jiraNamed struct {
	Name string `json:"name"`
}

// jiraUser covers both Cloud (accountId/emailAddress/displayName) and
// Server (name/displayName) user shapes.
type jiraUser struct {
	DisplayName string `json:"displayName"`
	Name        string `json:"name"`      // server
	AccountID   string `json:"accountId"` // cloud
}

func (u *jiraUser) name() string {
	if u == nil {
		return ""
	}
	switch {
	case u.DisplayName != "":
		return u.DisplayName
	case u.Name != "":
		return u.Name
	default:
		return u.AccountID
	}
}

type jiraComment struct {
	Author  jiraUser `json:"author"`
	Body    richText `json:"body"`
	Created jiraTime `json:"created"`
}

func (b *Backend) convertIssue(it jiraIssue) core.Issue {
	state := core.StateOpen
	if it.Fields.Status.StatusCategory.Key == "done" {
		state = core.StateClosed
	}

	priority := ""
	if it.Fields.Priority != nil {
		priority = it.Fields.Priority.Name
	}

	components := make([]string, len(it.Fields.Components))
	for i, c := range it.Fields.Components {
		components[i] = c.Name
	}

	return core.Issue{
		Tracker:  b.name,
		Type:     "jira",
		Key:      it.Key,
		Title:    it.Fields.Summary,
		State:    state,
		Status:   it.Fields.Status.Name,
		Priority: priority,
		Assignee: it.Fields.Assignee.name(),
		Reporter: it.Fields.Reporter.name(),
		Labels:   it.Fields.Labels,
		Project:  it.Fields.Project.Key,
		Created:  it.Fields.Created.Time(),
		Updated:  it.Fields.Updated.Time(),
		URL:      b.client.baseURL + "/browse/" + it.Key,
		Body:     string(it.Fields.Description),
		Extra:    map[string]any{"components": components},
	}
}

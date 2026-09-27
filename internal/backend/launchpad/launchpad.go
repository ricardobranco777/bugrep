// SPDX-License-Identifier: BSD-2-Clause

// Package launchpad implements the bugrep Backend for Launchpad
// (launchpad.net), scoped to configured targets — a distribution, a
// distribution source package, or a project.
//
// Launchpad's API ("LAZR") is architecturally unlike the other backends: it
// has no site-wide search (every request names an explicit target), and a
// bug's status/importance/assignee live on a separate "bug task" resource,
// not the bug itself, since one bug can affect several distros/projects at
// once.
package launchpad

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ricardobranco777/bugrep/internal/backend"
	"github.com/ricardobranco777/bugrep/internal/config"
	"github.com/ricardobranco777/bugrep/internal/core"
	"github.com/ricardobranco777/bugrep/internal/httpx"
)

func init() {
	backend.Register("launchpad", New)
}

// Backend is the Launchpad tracker implementation.
type Backend struct {
	name    string
	client  *client
	targets []string
}

// New constructs a Launchpad Backend from its configuration. There's no
// supported way to authenticate in v1, so this never reads any credential
// fields; every request is anonymous.
func New(_ context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	if len(tc.Targets) == 0 {
		return nil, fmt.Errorf("launchpad tracker %q: no targets configured", name)
	}

	baseURL := strings.TrimSuffix(tc.URL, "/")
	if baseURL == "" {
		baseURL = "https://api.launchpad.net/1.0"
	}

	return &Backend{
		name:    name,
		client:  &client{http: httpx.NewClient(httpOpts), baseURL: baseURL},
		targets: tc.Targets,
	}, nil
}

func (b *Backend) Name() string { return b.name }
func (b *Backend) Type() string { return "launchpad" }

func (b *Backend) Capabilities() core.Caps {
	return core.Caps{Labels: true, Component: false, Reporter: true}
}

// Check verifies connectivity by fetching the first configured target.
func (b *Backend) Check(ctx context.Context) error {
	return b.client.get(ctx, "/"+b.targets[0], nil, nil)
}

// resolveTargets returns the targets a search should fan out to: just the
// requested project if one is given (and it's configured), or every
// configured target otherwise.
func (b *Backend) resolveTargets(project string) ([]string, error) {
	if project == "" {
		return b.targets, nil
	}
	if slices.Contains(b.targets, project) {
		return []string{project}, nil
	}
	return nil, fmt.Errorf("project %q is outside this tracker's configured targets", project)
}

// Search fans out across every configured target (or just the one named by
// q.Project) concurrently, and merges the results. A target that fails
// doesn't abort the others; Search only returns an error when every target
// failed.
func (b *Backend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	targets, err := b.resolveTargets(q.Project)
	if err != nil {
		return nil, err
	}
	vals, err := buildSearchTasksQuery(q, b.client.baseURL)
	if err != nil {
		return nil, err
	}

	type outcome struct {
		issues []core.Issue
		err    error
		target string
	}
	outcomes := make([]outcome, len(targets))
	var wg sync.WaitGroup
	for i, tg := range targets {
		wg.Add(1)
		go func(i int, tg string) {
			defer wg.Done()
			issues, err := b.searchTarget(ctx, tg, vals)
			outcomes[i] = outcome{issues: issues, err: err, target: tg}
		}(i, tg)
	}
	wg.Wait()

	var allIssues []core.Issue
	var failed int
	for _, o := range outcomes {
		if o.err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "bugrep: warning: %s: %s: %v\n", b.name, o.target, o.err)
			continue
		}
		allIssues = append(allIssues, o.issues...)
	}
	if failed == len(targets) {
		return nil, fmt.Errorf("all %d configured target(s) failed", len(targets))
	}
	return allIssues, nil
}

func (b *Backend) searchTarget(ctx context.Context, targetPath string, vals url.Values) ([]core.Issue, error) {
	var issues []core.Issue
	var resp searchResponse
	if err := b.client.get(ctx, "/"+targetPath, vals, &resp); err != nil {
		return nil, err
	}
	for _, it := range resp.Entries {
		issues = append(issues, b.convertTask(it))
	}
	for resp.NextCollectionLink != "" {
		var next searchResponse
		if err := b.client.getURL(ctx, resp.NextCollectionLink, &next); err != nil {
			return nil, err
		}
		for _, it := range next.Entries {
			issues = append(issues, b.convertTask(it))
		}
		resp = next
	}
	return issues, nil
}

// Get fetches a single bug by its numeric id. A bug can have several bug
// tasks (one per affected distro/project); Get uses the first one for
// status/importance/assignee, a known v1 simplification.
func (b *Backend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	var bug bugItem
	if err := b.client.get(ctx, "/bugs/"+url.PathEscape(key), nil, &bug); err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}

	issue := core.Issue{
		Tracker:  b.name,
		Type:     "launchpad",
		Key:      key,
		Title:    bug.Title,
		State:    core.StateOpen, // overwritten below if a task was found
		Reporter: usernameFromLink(bug.OwnerLink, b.client.baseURL),
		Labels:   bug.Tags,
		Created:  bug.DateCreated,
		Updated:  bug.DateLastUpdated,
		URL:      bug.WebLink,
		Body:     bug.Description,
	}

	if bug.BugTasksCollectionLink != "" {
		var tasksResp struct {
			Entries []bugTaskItem `json:"entries"`
		}
		if err := b.client.getURL(ctx, bug.BugTasksCollectionLink, &tasksResp); err == nil && len(tasksResp.Entries) > 0 {
			task := tasksResp.Entries[0]
			issue.State = normalizeState(task.Status)
			issue.Status = task.Status
			issue.Priority = task.Importance
			issue.Project = task.BugTargetDisplayName
			if task.AssigneeLink != nil {
				issue.Assignee = usernameFromLink(*task.AssigneeLink, b.client.baseURL)
			}
			if task.WebLink != "" {
				issue.URL = task.WebLink // more specific than the bug's own generic web_link
			}
		}
		// A failed or empty task lookup degrades gracefully: the issue is
		// still returned with whatever the Bug entity itself had.
	}

	if withComments {
		comments, err := b.getMessages(ctx, bug.MessagesCollectionLink)
		if err != nil {
			return nil, fmt.Errorf("getting comments for %s: %w", key, err)
		}
		issue.Comments = comments
	}
	return &issue, nil
}

// getMessages fetches a bug's messages, skipping the first: Launchpad
// always represents the bug's own initial report as message #0, which
// would otherwise duplicate Body.
func (b *Backend) getMessages(ctx context.Context, link string) ([]core.Comment, error) {
	var comments []core.Comment
	first := true
	for link != "" {
		var resp struct {
			Entries            []messageItem `json:"entries"`
			NextCollectionLink string        `json:"next_collection_link"`
		}
		if err := b.client.getURL(ctx, link, &resp); err != nil {
			return nil, err
		}
		for _, m := range resp.Entries {
			if first {
				first = false
				continue
			}
			comments = append(comments, core.Comment{
				Author:  usernameFromLink(m.OwnerLink, b.client.baseURL),
				Created: m.DateCreated,
				Body:    m.Content,
			})
		}
		link = resp.NextCollectionLink
	}
	return comments, nil
}

// searchResponse is a searchTasks collection page.
type searchResponse struct {
	Entries            []bugTaskItem `json:"entries"`
	NextCollectionLink string        `json:"next_collection_link"`
}

// bugTaskItem is one entry from searchTasks: a bug's status within one
// particular target. Notably, it has no single "last updated" timestamp of
// its own (only the Bug entity does); Updated is approximated as the
// latest of this task's own state-transition dates, which won't reflect a
// new comment with no status change until Get is called on that bug.
type bugTaskItem struct {
	BugLink              string     `json:"bug_link"`
	Status               string     `json:"status"`
	Importance           string     `json:"importance"`
	AssigneeLink         *string    `json:"assignee_link"`
	OwnerLink            string     `json:"owner_link"`
	BugTargetDisplayName string     `json:"bug_target_display_name"`
	DateCreated          *time.Time `json:"date_created"`
	DateConfirmed        *time.Time `json:"date_confirmed"`
	DateIncomplete       *time.Time `json:"date_incomplete"`
	DateInProgress       *time.Time `json:"date_in_progress"`
	DateClosed           *time.Time `json:"date_closed"`
	DateLeftNew          *time.Time `json:"date_left_new"`
	DateTriaged          *time.Time `json:"date_triaged"`
	DateFixCommitted     *time.Time `json:"date_fix_committed"`
	DateFixReleased      *time.Time `json:"date_fix_released"`
	DateLeftClosed       *time.Time `json:"date_left_closed"`
	DateDeferred         *time.Time `json:"date_deferred"`
	Title                string     `json:"title"`
	WebLink              string     `json:"web_link"`
}

// bugItem is the Bug entity, fetched by Get.
type bugItem struct {
	Title                  string    `json:"title"`
	Description            string    `json:"description"`
	Tags                   []string  `json:"tags"`
	OwnerLink              string    `json:"owner_link"`
	DateCreated            time.Time `json:"date_created"`
	DateLastUpdated        time.Time `json:"date_last_updated"`
	WebLink                string    `json:"web_link"`
	BugTasksCollectionLink string    `json:"bug_tasks_collection_link"`
	MessagesCollectionLink string    `json:"messages_collection_link"`
}

type messageItem struct {
	Content     string    `json:"content"`
	OwnerLink   string    `json:"owner_link"`
	DateCreated time.Time `json:"date_created"`
}

func (b *Backend) convertTask(it bugTaskItem) core.Issue {
	assignee := ""
	if it.AssigneeLink != nil {
		assignee = usernameFromLink(*it.AssigneeLink, b.client.baseURL)
	}

	created := time.Time{}
	if it.DateCreated != nil {
		created = *it.DateCreated
	}
	updated := latestOf(
		it.DateCreated, it.DateConfirmed, it.DateIncomplete, it.DateInProgress,
		it.DateClosed, it.DateLeftNew, it.DateTriaged, it.DateFixCommitted,
		it.DateFixReleased, it.DateLeftClosed, it.DateDeferred,
	)

	return core.Issue{
		Tracker:  b.name,
		Type:     "launchpad",
		Key:      lastPathSegment(it.BugLink),
		Title:    parseTaskTitle(it.Title),
		State:    normalizeState(it.Status),
		Status:   it.Status,
		Priority: it.Importance,
		Assignee: assignee,
		Reporter: usernameFromLink(it.OwnerLink, b.client.baseURL),
		Project:  it.BugTargetDisplayName,
		Created:  created,
		Updated:  updated,
		URL:      it.WebLink,
	}
}

func normalizeState(status string) core.State {
	if slices.Contains(closedStatuses, status) {
		return core.StateClosed
	}
	return core.StateOpen
}

// parseTaskTitle extracts the bug's own title from a bug task's combined
// title string, e.g. `Bug #123 in foo (Ubuntu): "Kernel panic on boot"`
// becomes `Kernel panic on boot`. Falls back to the raw string if it
// doesn't match that shape.
func parseTaskTitle(raw string) string {
	first := strings.Index(raw, `"`)
	last := strings.LastIndex(raw, `"`)
	if first < 0 || last <= first {
		return raw
	}
	return raw[first+1 : last]
}

// usernameFromLink extracts a login from a person resource link, e.g.
// "https://api.launchpad.net/1.0/~jsmith" becomes "jsmith".
func usernameFromLink(link, baseURL string) string {
	return strings.TrimPrefix(link, baseURL+"/~")
}

// lastPathSegment extracts a bug's numeric id from its bug_link, e.g.
// "https://api.launchpad.net/1.0/bugs/2167563" becomes "2167563".
func lastPathSegment(u string) string {
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[i+1:]
	}
	return u
}

// latestOf returns the latest non-nil time, or the zero time if every
// argument is nil.
func latestOf(times ...*time.Time) time.Time {
	var latest time.Time
	for _, t := range times {
		if t != nil && t.After(latest) {
			latest = *t
		}
	}
	return latest
}

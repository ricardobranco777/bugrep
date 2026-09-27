// SPDX-License-Identifier: BSD-2-Clause

package gitea

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// scope is the set of orgs and/or repos a Gitea/Forgejo tracker instance is
// configured to search. bugrep never searches a whole instance.
type scope struct {
	Orgs  []string
	Repos []string
}

func (s scope) contains(project string) bool {
	if slices.Contains(s.Repos, project) {
		return true
	}
	owner, _, ok := strings.Cut(project, "/")
	if !ok {
		return false
	}
	return slices.Contains(s.Orgs, owner)
}

// target is one thing a search fans out to.
type target struct {
	kind string // "org" or "repo"
	id   string // org/owner name, or "owner/repo"
}

// searchTargets returns the targets a search should fan out to: just the
// requested project if one is given (and it's inside sc), or every
// configured org and repo otherwise.
func searchTargets(sc scope, project string) ([]target, error) {
	if project != "" {
		if !sc.contains(project) {
			return nil, &scopeError{project: project}
		}
		return []target{{kind: "repo", id: project}}, nil
	}
	var targets []target
	for _, o := range sc.Orgs {
		targets = append(targets, target{kind: "org", id: o})
	}
	for _, r := range sc.Repos {
		targets = append(targets, target{kind: "repo", id: r})
	}
	return targets, nil
}

type scopeError struct{ project string }

func (e *scopeError) Error() string {
	return "project \"" + e.project + "\" is outside this tracker's configured scope"
}

// buildRepoQuery translates a Query into parameters for
// /repos/{owner}/{repo}/issues, which supports the full filter set
// including arbitrary assignee/reporter usernames via assigned_by/
// created_by. q.Assignee/q.Reporter must already be resolved ("me"
// replaced with an actual login) by the caller.
func buildRepoQuery(q core.Query) (url.Values, error) {
	if q.Raw != "" {
		return url.ParseQuery(q.Raw)
	}

	vals := url.Values{}
	vals.Set("type", "issues") // exclude pull requests
	vals.Set("state", issueState(q.State))
	if q.Text != "" {
		vals.Set("q", q.Text)
	}
	if len(q.Labels) > 0 {
		vals.Set("labels", strings.Join(q.Labels, ","))
	}
	if q.Assignee != "" {
		vals.Set("assigned_by", q.Assignee)
	}
	if q.Reporter != "" {
		vals.Set("created_by", q.Reporter)
	}
	if !q.UpdatedSince.IsZero() {
		vals.Set("since", q.UpdatedSince.Format("2006-01-02T15:04:05Z07:00"))
	}
	if sort := sortName(q.Sort); sort != "" {
		vals.Set("sort", sort)
	}
	return vals, nil
}

// buildOrgQuery translates a Query into parameters for the global
// /repos/issues/search endpoint (scoped with owner=<org>). That endpoint
// only supports filtering assignee/reporter to the token's own user (via
// boolean assigned=/created= flags), not an arbitrary username, so
// skippedAssignee/skippedReporter report when a literal --assignee/
// --reporter couldn't be applied and the caller should warn about it.
func buildOrgQuery(q core.Query) (vals url.Values, skippedAssignee, skippedReporter bool) {
	if q.Raw != "" {
		vals, _ = url.ParseQuery(q.Raw) // parse errors already surfaced by buildRepoQuery for repo targets
		return vals, false, false
	}

	vals = url.Values{}
	vals.Set("type", "issues")
	vals.Set("state", issueState(q.State))
	if q.Text != "" {
		vals.Set("q", q.Text)
	}
	if len(q.Labels) > 0 {
		vals.Set("labels", strings.Join(q.Labels, ","))
	}
	switch q.Assignee {
	case "":
	case "me":
		vals.Set("assigned", "true")
	default:
		skippedAssignee = true
	}
	switch q.Reporter {
	case "":
	case "me":
		vals.Set("created", "true")
	default:
		skippedReporter = true
	}
	if !q.UpdatedSince.IsZero() {
		vals.Set("since", q.UpdatedSince.Format("2006-01-02T15:04:05Z07:00"))
	}
	if sort := sortName(q.Sort); sort != "" {
		vals.Set("sort", sort)
	}
	return vals, skippedAssignee, skippedReporter
}

func issueState(s core.State) string {
	switch s {
	case core.StateOpen, "":
		return "open"
	case core.StateClosed:
		return "closed"
	default:
		return "all"
	}
}

// sortName maps a core.SortField to the sort enum /repos/{owner}/{repo}/issues
// and /repos/issues/search both accept. Gitea/Forgejo issues have no notion
// of priority, so SortPriority falls back to the API's default ordering.
func sortName(s core.SortField) string {
	switch s {
	case core.SortUpdated:
		return "recentupdate"
	case core.SortCreated:
		return "latest"
	default:
		return ""
	}
}

func pageParams(vals url.Values, page, limit int) url.Values {
	out := vals.Clone()
	if out == nil {
		out = url.Values{} // Clone() returns nil for nil input; callers may pass nil for "no filters"
	}
	out.Set("page", strconv.Itoa(page))
	out.Set("limit", strconv.Itoa(limit))
	return out
}

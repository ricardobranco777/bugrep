// SPDX-License-Identifier: BSD-2-Clause

package gitlab

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// scope is the set of groups and/or projects a GitLab tracker instance is
// configured to search. bugrep never searches all of GitLab.
type scope struct {
	Groups   []string
	Projects []string
}

// contains reports whether project ("group/project" or
// "group/subgroup/project") falls inside the scope: either it's listed
// directly in Projects, or it sits under one of the configured Groups.
func (s scope) contains(project string) bool {
	if slices.Contains(s.Projects, project) {
		return true
	}
	for _, g := range s.Groups {
		if project == g || strings.HasPrefix(project, g+"/") {
			return true
		}
	}
	return false
}

// target is one GitLab API scope to query: either a group (searched with
// include_subgroups) or a single project.
type target struct {
	kind string // "group" or "project"
	id   string // group or project path
}

// searchTargets returns the targets a search should fan out to: just the
// requested project if one is given (and it's inside sc), or every
// configured group and project otherwise.
func searchTargets(sc scope, project string) ([]target, error) {
	if project != "" {
		if !sc.contains(project) {
			return nil, fmt.Errorf("project %q is outside this tracker's configured scope", project)
		}
		return []target{{kind: "project", id: project}}, nil
	}

	var targets []target
	for _, g := range sc.Groups {
		targets = append(targets, target{kind: "group", id: g})
	}
	for _, p := range sc.Projects {
		targets = append(targets, target{kind: "project", id: p})
	}
	return targets, nil
}

// buildIssuesQuery translates a Query into the query parameters for
// GitLab's /issues endpoints. q.Assignee/q.Reporter must already be
// resolved ("me" replaced with an actual username) by the caller.
//
// When q.Raw is set, it's parsed as a literal query string and used as-is:
// GitLab's raw passthrough is query string params, not a single query
// language like JQL or GitHub's search syntax.
func buildIssuesQuery(q core.Query) (url.Values, error) {
	if q.Raw != "" {
		vals, err := url.ParseQuery(q.Raw)
		if err != nil {
			return nil, fmt.Errorf("invalid --raw query string: %w", err)
		}
		return vals, nil
	}

	vals := url.Values{}
	switch q.State {
	case core.StateOpen:
		vals.Set("state", "opened")
	case core.StateClosed:
		vals.Set("state", "closed")
	case core.StateAll, "":
		// omitted: GitLab returns issues in every state
	}
	if q.Text != "" {
		vals.Set("search", q.Text)
		vals.Set("in", "title,description")
	}
	if len(q.Labels) > 0 {
		vals.Set("labels", strings.Join(q.Labels, ","))
	}
	if q.Assignee != "" {
		vals.Set("assignee_username", q.Assignee)
	}
	if q.Reporter != "" {
		vals.Set("author_username", q.Reporter)
	}
	if !q.CreatedSince.IsZero() {
		vals.Set("created_after", q.CreatedSince.Format("2006-01-02T15:04:05Z07:00"))
	}
	if !q.UpdatedSince.IsZero() {
		vals.Set("updated_after", q.UpdatedSince.Format("2006-01-02T15:04:05Z07:00"))
	}
	switch q.Sort {
	case core.SortUpdated:
		vals.Set("order_by", "updated_at")
		vals.Set("sort", "desc")
	case core.SortCreated:
		vals.Set("order_by", "created_at")
		vals.Set("sort", "desc")
	default:
		// GitLab has no notion of issue priority; leave the API's default
		// ordering (created_at desc) in place.
	}
	return vals, nil
}

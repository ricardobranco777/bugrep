// SPDX-License-Identifier: BSD-2-Clause

package github

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// scope is the set of orgs and/or repos a GitHub tracker instance is
// configured to search. bugrep never searches all of GitHub.
type scope struct {
	Orgs  []string
	Repos []string
}

// contains reports whether project ("owner/repo") falls inside the scope,
// either because it's listed directly in Repos or because its owner is one
// of the configured Orgs.
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

// buildSearchQuery translates a Query into a GitHub search/issues "q"
// parameter. Scope qualifiers (org:/repo:) are always appended, even when
// q.Raw is set, so a raw query can't escape the configured scope.
func buildSearchQuery(q core.Query, sc scope) (string, error) {
	var parts []string

	if q.Raw != "" {
		parts = append(parts, q.Raw)
	} else {
		parts = append(parts, "is:issue")

		switch q.State {
		case core.StateOpen:
			parts = append(parts, "is:open")
		case core.StateClosed:
			parts = append(parts, "is:closed")
		case core.StateAll, "":
			// no state qualifier: match both
		}

		if q.Text != "" {
			parts = append(parts, q.Text)
		}
		for _, l := range q.Labels {
			parts = append(parts, "label:"+quoteIfNeeded(l))
		}
		if q.Assignee != "" {
			parts = append(parts, "assignee:"+ghUser(q.Assignee))
		}
		if q.Reporter != "" {
			parts = append(parts, "author:"+ghUser(q.Reporter))
		}
		if !q.CreatedSince.IsZero() {
			parts = append(parts, "created:>="+q.CreatedSince.Format("2006-01-02"))
		}
		if !q.UpdatedSince.IsZero() {
			parts = append(parts, "updated:>="+q.UpdatedSince.Format("2006-01-02"))
		}
	}

	scopeParts, err := scopeQualifiers(q.Project, sc)
	if err != nil {
		return "", err
	}
	parts = append(parts, scopeParts...)

	return strings.Join(parts, " "), nil
}

// scopeQualifiers returns the org:/repo: qualifiers to scope a search: just
// the requested project if one is given (and it's inside sc), or every
// configured org and repo otherwise.
func scopeQualifiers(project string, sc scope) ([]string, error) {
	if project != "" {
		if !sc.contains(project) {
			return nil, fmt.Errorf("project %q is outside this tracker's configured scope", project)
		}
		return []string{"repo:" + project}, nil
	}

	var parts []string
	for _, org := range sc.Orgs {
		parts = append(parts, "org:"+org)
	}
	for _, repo := range sc.Repos {
		parts = append(parts, "repo:"+repo)
	}
	return parts, nil
}

// sortParams maps a core.SortField to the GitHub search API's sort/order
// parameters. GitHub has no notion of issue priority, so SortPriority falls
// back to the API's default relevance ordering.
func sortParams(s core.SortField) (sort, order string) {
	switch s {
	case core.SortUpdated:
		return "updated", "desc"
	case core.SortCreated:
		return "created", "desc"
	default:
		return "", ""
	}
}

func ghUser(u string) string {
	if u == "me" {
		return "@me"
	}
	return u
}

func quoteIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

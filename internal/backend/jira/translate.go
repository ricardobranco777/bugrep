// SPDX-License-Identifier: BSD-2-Clause

package jira

import (
	"strings"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// buildJQL translates a Query into JQL. Cloud and Server/Data Center accept
// the same JQL grammar, so this is shared between both flavors.
//
// Unlike the GitHub/GitLab backends, q.Raw isn't scoped further: Jira has no
// per-instance "scope" concept the way a GitHub/GitLab tracker is scoped to
// orgs/groups, so a raw JQL string is used exactly as given.
func buildJQL(q core.Query) string {
	if q.Raw != "" {
		return q.Raw
	}

	var clauses []string
	if q.Project != "" {
		clauses = append(clauses, "project = "+quoteJQL(q.Project))
	}
	if q.Component != "" {
		clauses = append(clauses, "component = "+quoteJQL(q.Component))
	}
	switch q.State {
	case core.StateOpen:
		clauses = append(clauses, "statusCategory != Done")
	case core.StateClosed:
		clauses = append(clauses, "statusCategory = Done")
	case core.StateAll, "":
		// no filter: match every status
	}
	if q.Assignee != "" {
		clauses = append(clauses, "assignee = "+jqlUser(q.Assignee))
	}
	if q.Reporter != "" {
		clauses = append(clauses, "reporter = "+jqlUser(q.Reporter))
	}
	for _, l := range q.Labels {
		clauses = append(clauses, "labels = "+quoteJQL(l))
	}
	if !q.CreatedSince.IsZero() {
		clauses = append(clauses, `created >= "`+q.CreatedSince.Format("2006-01-02")+`"`)
	}
	if !q.UpdatedSince.IsZero() {
		clauses = append(clauses, `updated >= "`+q.UpdatedSince.Format("2006-01-02")+`"`)
	}
	if q.Text != "" {
		clauses = append(clauses, "text ~ "+quoteJQL(q.Text))
	}

	var b strings.Builder
	b.WriteString(strings.Join(clauses, " AND "))

	orderBy := ""
	switch q.Sort {
	case core.SortUpdated:
		orderBy = "updated DESC"
	case core.SortCreated:
		orderBy = "created DESC"
	case core.SortPriority:
		orderBy = "priority DESC"
	}
	if orderBy != "" {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString("ORDER BY " + orderBy)
	}
	return b.String()
}

// jqlUser maps "me" to JQL's currentUser() function; any other value is
// quoted as a literal.
func jqlUser(u string) string {
	if u == "me" {
		return "currentUser()"
	}
	return quoteJQL(u)
}

func quoteJQL(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

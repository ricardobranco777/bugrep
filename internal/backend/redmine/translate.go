// SPDX-License-Identifier: BSD-2-Clause

package redmine

import (
	"net/url"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// buildQuery translates a Query into /issues.json filter parameters.
//
// Redmine has two real limitations here: labels aren't a native concept
// (they'd be a per-install custom field, so --label is silently unsupported
// — see Capabilities), and assigned_to_id/author_id expect a numeric user
// id, not a login, with one exception: Redmine treats the literal string
// "me" as the current user for both fields, which covers the common case.
func buildQuery(q core.Query) (url.Values, error) {
	if q.Raw != "" {
		vals, err := url.ParseQuery(q.Raw)
		if err != nil {
			return nil, err
		}
		return vals, nil
	}

	vals := url.Values{}
	if q.Project != "" {
		vals.Set("project_id", q.Project)
	}
	switch q.State {
	case core.StateOpen, "":
		vals.Set("status_id", "open")
	case core.StateClosed:
		vals.Set("status_id", "closed")
	case core.StateAll:
		vals.Set("status_id", "*")
	}
	if q.Assignee != "" {
		vals.Set("assigned_to_id", q.Assignee)
	}
	if q.Reporter != "" {
		vals.Set("author_id", q.Reporter)
	}
	if q.Text != "" {
		vals.Set("subject", "~"+q.Text) // "~" is Redmine's substring-match operator
	}
	if !q.CreatedSince.IsZero() {
		vals.Set("created_on", ">="+q.CreatedSince.Format("2006-01-02"))
	}
	if !q.UpdatedSince.IsZero() {
		vals.Set("updated_on", ">="+q.UpdatedSince.Format("2006-01-02"))
	}
	switch q.Sort {
	case core.SortUpdated:
		vals.Set("sort", "updated_on:desc")
	case core.SortCreated:
		vals.Set("sort", "created_on:desc")
	case core.SortPriority:
		vals.Set("sort", "priority:desc")
	}
	return vals, nil
}

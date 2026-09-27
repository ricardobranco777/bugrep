// SPDX-License-Identifier: BSD-2-Clause

package bugzilla

import (
	"fmt"
	"net/url"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// buildQuery translates a Query into /rest/bug query parameters.
//
// Most filters map to a plain named REST parameter (product, component,
// assigned_to, ...). Anything that needs more than simple equality —
// "resolution is/isn't set", each required label — uses Bugzilla's numbered
// boolean-chart parameters (f1/o1/v1, f2/o2/v2, ...), which are ANDed
// together by default; that's also how the search UI itself builds queries.
//
// q.Assignee/q.Reporter must already be resolved ("me" replaced with an
// actual login) by the caller.
func buildQuery(q core.Query) (url.Values, error) {
	if q.Raw != "" {
		vals, err := url.ParseQuery(q.Raw)
		if err != nil {
			return nil, fmt.Errorf("invalid --raw query string: %w", err)
		}
		return vals, nil
	}

	vals := url.Values{}
	if q.Project != "" {
		vals.Set("product", q.Project)
	}
	if q.Component != "" {
		vals.Set("component", q.Component)
	}

	n := 0
	addClause := func(field, op, value string) {
		n++
		vals.Set(fmt.Sprintf("f%d", n), field)
		vals.Set(fmt.Sprintf("o%d", n), op)
		vals.Set(fmt.Sprintf("v%d", n), value)
	}
	// isempty/isnotempty take no value.
	addUnaryClause := func(field, op string) {
		n++
		vals.Set(fmt.Sprintf("f%d", n), field)
		vals.Set(fmt.Sprintf("o%d", n), op)
	}

	switch q.State {
	case core.StateOpen:
		addUnaryClause("resolution", "isempty")
	case core.StateClosed:
		addUnaryClause("resolution", "isnotempty")
	case core.StateAll, "":
		// no filter
	}

	if q.Assignee != "" {
		vals.Set("assigned_to", q.Assignee)
	}
	if q.Reporter != "" {
		vals.Set("creator", q.Reporter)
	}
	for _, l := range q.Labels {
		addClause("keywords", "substring", l)
	}
	if q.Text != "" {
		vals.Set("short_desc", q.Text)
		vals.Set("short_desc_type", "allwordssubstr")
	}
	if !q.CreatedSince.IsZero() {
		vals.Set("creation_time", q.CreatedSince.Format(time.RFC3339))
	}
	if !q.UpdatedSince.IsZero() {
		vals.Set("last_change_time", q.UpdatedSince.Format(time.RFC3339))
	}

	switch q.Sort {
	case core.SortUpdated:
		vals.Set("order", "delta_ts DESC")
	case core.SortCreated:
		vals.Set("order", "creation_ts DESC")
	case core.SortPriority:
		vals.Set("order", "priority DESC")
	}

	return vals, nil
}

// bugzillaFields lists the bug fields bugrep needs; passed as
// include_fields so the server doesn't send (and we don't pay to receive)
// anything else.
var bugzillaFields = []string{
	"id", "summary", "status", "resolution", "priority", "severity",
	"assigned_to", "creator", "keywords", "product", "component",
	"creation_time", "last_change_time",
}

// SPDX-License-Identifier: BSD-2-Clause

package launchpad

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// openStatuses/closedStatuses are Launchpad's own fixed, platform-wide bug
// task status vocabulary — unlike Redmine's per-install statuses, sites
// can't customize this, so the open/closed grouping can be hardcoded rather
// than discovered.
var (
	openStatuses = []string{
		"New", "Incomplete", "Confirmed", "Triaged", "In Progress", "Fix Committed",
	}
	closedStatuses = []string{
		"Fix Released", "Invalid", "Won't Fix", "Expired", "Opinion",
	}
)

// buildSearchTasksQuery translates a Query into parameters for a
// ws.op=searchTasks request. baseURL is used to build person-link
// parameters (assignee/bug_reporter): Launchpad's API needs a full
// resource link, not a bare username.
//
// "me" isn't supported for --assignee/--reporter: Launchpad has no
// anonymous notion of "the current user", and authenticating would require
// the OAuth1 dance, which v1 doesn't implement.
func buildSearchTasksQuery(q core.Query, baseURL string) (url.Values, error) {
	vals := url.Values{}
	vals.Set("ws.op", "searchTasks")
	vals.Set("omit_duplicates", "true")

	if q.Raw != "" {
		raw, err := url.ParseQuery(q.Raw)
		if err != nil {
			return nil, fmt.Errorf("invalid --raw query string: %w", err)
		}
		maps.Copy(vals, raw)
		vals.Set("ws.op", "searchTasks") // raw can't override the operation
		return vals, nil
	}

	if q.Text != "" {
		vals.Set("search_text", q.Text)
	}

	switch q.State {
	case core.StateOpen, "":
		if err := setJSONArray(vals, "status", openStatuses); err != nil {
			return nil, err
		}
	case core.StateClosed:
		if err := setJSONArray(vals, "status", closedStatuses); err != nil {
			return nil, err
		}
	case core.StateAll:
		// omitted: matches every status
	}

	if len(q.Labels) > 0 {
		if err := setJSONArray(vals, "tags", q.Labels); err != nil {
			return nil, err
		}
		vals.Set("tags_combinator", "All")
	}

	if q.Assignee != "" {
		if q.Assignee == "me" {
			return nil, fmt.Errorf(`launchpad has no anonymous "me": authenticate isn't supported, so --assignee me can't be resolved`)
		}
		vals.Set("assignee", personLink(baseURL, q.Assignee))
	}
	if q.Reporter != "" {
		if q.Reporter == "me" {
			return nil, fmt.Errorf(`launchpad has no anonymous "me": authenticate isn't supported, so --reporter me can't be resolved`)
		}
		vals.Set("bug_reporter", personLink(baseURL, q.Reporter))
	}

	if !q.CreatedSince.IsZero() {
		vals.Set("created_since", q.CreatedSince.Format(time.RFC3339))
	}
	if !q.UpdatedSince.IsZero() {
		vals.Set("modified_since", q.UpdatedSince.Format(time.RFC3339))
	}

	if orderBy := orderByName(q.Sort); orderBy != "" {
		vals.Set("order_by", orderBy)
	}

	return vals, nil
}

// setJSONArray encodes values as a JSON array in a single query parameter,
// which is how Launchpad's LAZR API accepts multi-valued parameters —
// not as repeated same-name params.
func setJSONArray(vals url.Values, key string, values []string) error {
	b, err := json.Marshal(values)
	if err != nil {
		return err
	}
	vals.Set(key, string(b))
	return nil
}

func personLink(baseURL, username string) string {
	return baseURL + "/~" + username
}

// orderByName maps a core.SortField to the field name searchTasks accepts
// for order_by. Note "datecreated" has no underscore, unlike
// "date_last_updated".
func orderByName(s core.SortField) string {
	switch s {
	case core.SortUpdated:
		return "-date_last_updated"
	case core.SortCreated:
		return "-datecreated"
	case core.SortPriority:
		return "-importance"
	default:
		return ""
	}
}

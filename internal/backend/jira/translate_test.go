// SPDX-License-Identifier: BSD-2-Clause

package jira

import (
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func TestBuildJQL(t *testing.T) {
	cases := []struct {
		name string
		q    core.Query
		want string
	}{
		{
			name: "project and open state with default sort",
			q:    core.Query{Project: "PROJ", State: core.StateOpen, Sort: core.SortUpdated},
			want: `project = "PROJ" AND statusCategory != Done ORDER BY updated DESC`,
		},
		{
			name: "closed state",
			q:    core.Query{State: core.StateClosed},
			want: `statusCategory = Done`,
		},
		{
			name: "all state omits status filter",
			q:    core.Query{State: core.StateAll},
			want: ``,
		},
		{
			name: "assignee and reporter me",
			q:    core.Query{Assignee: "me", Reporter: "me"},
			want: `assignee = currentUser() AND reporter = currentUser()`,
		},
		{
			name: "assignee and reporter literal, quoted",
			q:    core.Query{Assignee: "alice", Reporter: "bob"},
			want: `assignee = "alice" AND reporter = "bob"`,
		},
		{
			name: "labels and component",
			q:    core.Query{Component: "backend", Labels: []string{"bug", "urgent"}},
			want: `component = "backend" AND labels = "bug" AND labels = "urgent"`,
		},
		{
			name: "text search is quoted and escaped",
			q:    core.Query{Text: `has "quotes" and \backslash`},
			want: `text ~ "has \"quotes\" and \\backslash"`,
		},
		{
			name: "created and updated since",
			q: core.Query{
				CreatedSince: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedSince: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			},
			want: `created >= "2026-01-01" AND updated >= "2026-02-15"`,
		},
		{
			name: "sort priority",
			q:    core.Query{State: core.StateAll, Sort: core.SortPriority},
			want: `ORDER BY priority DESC`,
		},
		{
			name: "raw passthrough ignores everything else",
			q:    core.Query{Raw: "assignee = jsmith ORDER BY created ASC", Project: "IGNORED", State: core.StateOpen},
			want: `assignee = jsmith ORDER BY created ASC`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildJQL(c.q)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

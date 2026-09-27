// SPDX-License-Identifier: BSD-2-Clause

package gitlab

import (
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func TestSearchTargets(t *testing.T) {
	groupScope := scope{Groups: []string{"mygroup"}}
	projectScope := scope{Projects: []string{"group/project"}}
	mixedScope := scope{Groups: []string{"mygroup"}, Projects: []string{"other/project"}}

	t.Run("no project uses full scope", func(t *testing.T) {
		targets, err := searchTargets(mixedScope, "")
		if err != nil {
			t.Fatal(err)
		}
		want := []target{{kind: "group", id: "mygroup"}, {kind: "project", id: "other/project"}}
		if len(targets) != len(want) {
			t.Fatalf("got %+v, want %+v", targets, want)
		}
		for i := range want {
			if targets[i] != want[i] {
				t.Errorf("target %d: got %+v, want %+v", i, targets[i], want[i])
			}
		}
	})

	t.Run("project inside a group", func(t *testing.T) {
		targets, err := searchTargets(groupScope, "mygroup/subproject")
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 || targets[0] != (target{kind: "project", id: "mygroup/subproject"}) {
			t.Errorf("got %+v", targets)
		}
	})

	t.Run("project inside a nested subgroup", func(t *testing.T) {
		targets, err := searchTargets(groupScope, "mygroup/subgroup/project")
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 {
			t.Errorf("got %+v", targets)
		}
	})

	t.Run("project matches a configured project exactly", func(t *testing.T) {
		targets, err := searchTargets(projectScope, "group/project")
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 {
			t.Errorf("got %+v", targets)
		}
	})

	t.Run("project outside scope errors", func(t *testing.T) {
		if _, err := searchTargets(groupScope, "someoneelse/project"); err == nil {
			t.Fatal("expected error for a project outside the configured scope")
		}
	})
}

func TestBuildIssuesQuery(t *testing.T) {
	cases := []struct {
		name string
		q    core.Query
		want map[string]string
	}{
		{
			name: "open state and text",
			q:    core.Query{State: core.StateOpen, Text: "crash"},
			want: map[string]string{"state": "opened", "search": "crash", "in": "title,description"},
		},
		{
			name: "closed state",
			q:    core.Query{State: core.StateClosed},
			want: map[string]string{"state": "closed"},
		},
		{
			name: "all state omits state param",
			q:    core.Query{State: core.StateAll},
			want: map[string]string{},
		},
		{
			name: "labels joined with commas",
			q:    core.Query{Labels: []string{"bug", "urgent"}},
			want: map[string]string{"labels": "bug,urgent"},
		},
		{
			name: "resolved assignee and reporter",
			q:    core.Query{Assignee: "alice", Reporter: "bob"},
			want: map[string]string{"assignee_username": "alice", "author_username": "bob"},
		},
		{
			name: "sort updated",
			q:    core.Query{Sort: core.SortUpdated},
			want: map[string]string{"order_by": "updated_at", "sort": "desc"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildIssuesQuery(c.q)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, v := range c.want {
				if got.Get(k) != v {
					t.Errorf("%s: got %q, want %q", k, got.Get(k), v)
				}
			}
		})
	}
}

func TestBuildIssuesQueryDates(t *testing.T) {
	q := core.Query{
		CreatedSince: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedSince: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
	}
	got, err := buildIssuesQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("created_after") != "2026-01-01T00:00:00Z" {
		t.Errorf("created_after = %q", got.Get("created_after"))
	}
	if got.Get("updated_after") != "2026-02-15T00:00:00Z" {
		t.Errorf("updated_after = %q", got.Get("updated_after"))
	}
}

func TestBuildIssuesQueryRawPassthrough(t *testing.T) {
	got, err := buildIssuesQuery(core.Query{Raw: "labels=bug&state=opened"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("labels") != "bug" || got.Get("state") != "opened" {
		t.Errorf("got %v", got)
	}
}

func TestBuildIssuesQueryInvalidRaw(t *testing.T) {
	if _, err := buildIssuesQuery(core.Query{Raw: "%zz"}); err == nil {
		t.Fatal("expected error for invalid raw query string")
	}
}

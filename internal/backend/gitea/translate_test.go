// SPDX-License-Identifier: BSD-2-Clause

package gitea

import (
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func TestSearchTargets(t *testing.T) {
	orgScope := scope{Orgs: []string{"myorg"}}
	repoScope := scope{Repos: []string{"owner/repo"}}
	mixedScope := scope{Orgs: []string{"myorg"}, Repos: []string{"other/repo"}}

	t.Run("no project uses full scope", func(t *testing.T) {
		targets, err := searchTargets(mixedScope, "")
		if err != nil {
			t.Fatal(err)
		}
		want := []target{{kind: "org", id: "myorg"}, {kind: "repo", id: "other/repo"}}
		if len(targets) != len(want) {
			t.Fatalf("got %+v, want %+v", targets, want)
		}
		for i := range want {
			if targets[i] != want[i] {
				t.Errorf("target %d: got %+v, want %+v", i, targets[i], want[i])
			}
		}
	})

	t.Run("project inside an org", func(t *testing.T) {
		targets, err := searchTargets(orgScope, "myorg/somerepo")
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 || targets[0] != (target{kind: "repo", id: "myorg/somerepo"}) {
			t.Errorf("got %+v", targets)
		}
	})

	t.Run("project matches a configured repo exactly", func(t *testing.T) {
		targets, err := searchTargets(repoScope, "owner/repo")
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 {
			t.Errorf("got %+v", targets)
		}
	})

	t.Run("project outside scope errors", func(t *testing.T) {
		if _, err := searchTargets(orgScope, "someoneelse/repo"); err == nil {
			t.Fatal("expected error for a project outside the configured scope")
		}
	})
}

func TestBuildRepoQuery(t *testing.T) {
	cases := []struct {
		name string
		q    core.Query
		want map[string]string
	}{
		{
			name: "default excludes PRs and defaults to open",
			q:    core.Query{},
			want: map[string]string{"type": "issues", "state": "open"},
		},
		{
			name: "closed state",
			q:    core.Query{State: core.StateClosed},
			want: map[string]string{"state": "closed"},
		},
		{
			name: "all state",
			q:    core.Query{State: core.StateAll},
			want: map[string]string{"state": "all"},
		},
		{
			name: "text and labels",
			q:    core.Query{Text: "crash", Labels: []string{"bug", "urgent"}},
			want: map[string]string{"q": "crash", "labels": "bug,urgent"},
		},
		{
			name: "resolved assignee and reporter",
			q:    core.Query{Assignee: "alice", Reporter: "bob"},
			want: map[string]string{"assigned_by": "alice", "created_by": "bob"},
		},
		{
			name: "sort updated",
			q:    core.Query{Sort: core.SortUpdated},
			want: map[string]string{"sort": "recentupdate"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildRepoQuery(c.q)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range c.want {
				if got.Get(k) != v {
					t.Errorf("%s: got %q, want %q", k, got.Get(k), v)
				}
			}
		})
	}
}

func TestBuildRepoQuerySince(t *testing.T) {
	q := core.Query{UpdatedSince: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)}
	got, err := buildRepoQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("since") != "2026-02-15T00:00:00Z" {
		t.Errorf("since = %q", got.Get("since"))
	}
}

func TestBuildRepoQueryRawPassthrough(t *testing.T) {
	got, err := buildRepoQuery(core.Query{Raw: "state=closed&labels=bug"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("state") != "closed" || got.Get("labels") != "bug" {
		t.Errorf("got %v", got)
	}
}

func TestBuildOrgQueryMeIsSupported(t *testing.T) {
	vals, skippedA, skippedR := buildOrgQuery(core.Query{Assignee: "me", Reporter: "me"})
	if skippedA || skippedR {
		t.Fatal("expected \"me\" to be supported for org-wide search")
	}
	if vals.Get("assigned") != "true" || vals.Get("created") != "true" {
		t.Errorf("got %v", vals)
	}
}

func TestBuildOrgQueryLiteralUserIsSkipped(t *testing.T) {
	vals, skippedA, skippedR := buildOrgQuery(core.Query{Assignee: "alice", Reporter: "bob"})
	if !skippedA || !skippedR {
		t.Fatal("expected a literal username to be reported as skipped for org-wide search")
	}
	if vals.Get("assigned_by") != "" || vals.Get("assigned") != "" {
		t.Errorf("expected no assignee param set at all, got %v", vals)
	}
}

func TestIssueState(t *testing.T) {
	cases := map[core.State]string{
		core.StateOpen:   "open",
		"":               "open",
		core.StateClosed: "closed",
		core.StateAll:    "all",
	}
	for in, want := range cases {
		if got := issueState(in); got != want {
			t.Errorf("issueState(%q) = %q, want %q", in, got, want)
		}
	}
}

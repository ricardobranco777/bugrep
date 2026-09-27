// SPDX-License-Identifier: BSD-2-Clause

package github

import (
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func TestBuildSearchQuery(t *testing.T) {
	orgScope := scope{Orgs: []string{"myorg"}}
	repoScope := scope{Repos: []string{"owner/repo"}}
	mixedScope := scope{Orgs: []string{"myorg"}, Repos: []string{"other/repo"}}

	cases := []struct {
		name string
		q    core.Query
		sc   scope
		want string
	}{
		{
			name: "default open search in an org",
			q:    core.Query{State: core.StateOpen, Text: "crash on startup"},
			sc:   orgScope,
			want: "is:issue is:open crash on startup org:myorg",
		},
		{
			name: "closed state",
			q:    core.Query{State: core.StateClosed},
			sc:   repoScope,
			want: "is:issue is:closed repo:owner/repo",
		},
		{
			name: "all states omits is: qualifier",
			q:    core.Query{State: core.StateAll},
			sc:   repoScope,
			want: "is:issue repo:owner/repo",
		},
		{
			name: "labels with and without spaces",
			q:    core.Query{State: core.StateOpen, Labels: []string{"bug", "needs triage"}},
			sc:   repoScope,
			want: `is:issue is:open label:bug label:"needs triage" repo:owner/repo`,
		},
		{
			name: "assignee and reporter me",
			q:    core.Query{State: core.StateOpen, Assignee: "me", Reporter: "me"},
			sc:   repoScope,
			want: "is:issue is:open assignee:@me author:@me repo:owner/repo",
		},
		{
			name: "assignee and reporter literal user",
			q:    core.Query{State: core.StateOpen, Assignee: "alice", Reporter: "bob"},
			sc:   repoScope,
			want: "is:issue is:open assignee:alice author:bob repo:owner/repo",
		},
		{
			name: "updated and created since",
			q: core.Query{
				State:        core.StateOpen,
				CreatedSince: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedSince: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			},
			sc:   repoScope,
			want: "is:issue is:open created:>=2026-01-01 updated:>=2026-02-15 repo:owner/repo",
		},
		{
			name: "project narrows scope to a single repo",
			q:    core.Query{State: core.StateOpen, Project: "other/repo"},
			sc:   mixedScope,
			want: "is:issue is:open repo:other/repo",
		},
		{
			name: "project inside an org scope",
			q:    core.Query{State: core.StateOpen, Project: "myorg/some-repo"},
			sc:   orgScope,
			want: "is:issue is:open repo:myorg/some-repo",
		},
		{
			name: "mixed org and repo scope with no project",
			q:    core.Query{State: core.StateOpen},
			sc:   mixedScope,
			want: "is:issue is:open org:myorg repo:other/repo",
		},
		{
			name: "raw passthrough still gets scope qualifiers",
			q:    core.Query{Raw: "author:someone is:open"},
			sc:   repoScope,
			want: "author:someone is:open repo:owner/repo",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildSearchQuery(c.q, c.sc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestBuildSearchQueryProjectOutsideScope(t *testing.T) {
	_, err := buildSearchQuery(core.Query{Project: "someoneelse/repo"}, scope{Orgs: []string{"myorg"}})
	if err == nil {
		t.Fatal("expected error for a project outside the configured scope")
	}
}

func TestSortParams(t *testing.T) {
	cases := []struct {
		in        core.SortField
		wantSort  string
		wantOrder string
	}{
		{core.SortUpdated, "updated", "desc"},
		{core.SortCreated, "created", "desc"},
		{core.SortPriority, "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		sort, order := sortParams(c.in)
		if sort != c.wantSort || order != c.wantOrder {
			t.Errorf("sortParams(%q) = (%q, %q), want (%q, %q)", c.in, sort, order, c.wantSort, c.wantOrder)
		}
	}
}

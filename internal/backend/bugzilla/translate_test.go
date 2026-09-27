// SPDX-License-Identifier: BSD-2-Clause

package bugzilla

import (
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func TestBuildQuery(t *testing.T) {
	cases := []struct {
		name string
		q    core.Query
		want map[string]string
	}{
		{
			name: "product and component",
			q:    core.Query{Project: "Fedora", Component: "kernel"},
			want: map[string]string{"product": "Fedora", "component": "kernel"},
		},
		{
			name: "open state uses isempty boolean chart",
			q:    core.Query{State: core.StateOpen},
			want: map[string]string{"f1": "resolution", "o1": "isempty"},
		},
		{
			name: "closed state uses isnotempty boolean chart",
			q:    core.Query{State: core.StateClosed},
			want: map[string]string{"f1": "resolution", "o1": "isnotempty"},
		},
		{
			name: "all state has no resolution filter",
			q:    core.Query{State: core.StateAll},
			want: map[string]string{"f1": ""},
		},
		{
			name: "resolved assignee and reporter",
			q:    core.Query{Assignee: "alice@example.com", Reporter: "bob@example.com"},
			want: map[string]string{"assigned_to": "alice@example.com", "creator": "bob@example.com"},
		},
		{
			name: "text search",
			q:    core.Query{Text: "kernel panic"},
			want: map[string]string{"short_desc": "kernel panic", "short_desc_type": "allwordssubstr"},
		},
		{
			name: "sort updated",
			q:    core.Query{Sort: core.SortUpdated},
			want: map[string]string{"order": "delta_ts DESC"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildQuery(c.q)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, v := range c.want {
				if v == "" {
					if got.Get(k) != "" {
						t.Errorf("%s: expected empty, got %q", k, got.Get(k))
					}
					continue
				}
				if got.Get(k) != v {
					t.Errorf("%s: got %q, want %q", k, got.Get(k), v)
				}
			}
		})
	}
}

func TestBuildQueryLabelsUseBooleanChartClauses(t *testing.T) {
	got, err := buildQuery(core.Query{Labels: []string{"regression", "security"}})
	if err != nil {
		t.Fatal(err)
	}
	// Two AND'd clauses: f1/o1/v1 and f2/o2/v2, both "keywords substring".
	if got.Get("f1") != "keywords" || got.Get("o1") != "substring" || got.Get("v1") != "regression" {
		t.Errorf("clause 1: f=%q o=%q v=%q", got.Get("f1"), got.Get("o1"), got.Get("v1"))
	}
	if got.Get("f2") != "keywords" || got.Get("o2") != "substring" || got.Get("v2") != "security" {
		t.Errorf("clause 2: f=%q o=%q v=%q", got.Get("f2"), got.Get("o2"), got.Get("v2"))
	}
}

func TestBuildQueryStateAndLabelsShareClauseNumbering(t *testing.T) {
	got, err := buildQuery(core.Query{State: core.StateOpen, Labels: []string{"regression"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("f1") != "resolution" || got.Get("f2") != "keywords" {
		t.Errorf("expected resolution as clause 1 and keywords as clause 2, got f1=%q f2=%q", got.Get("f1"), got.Get("f2"))
	}
}

func TestBuildQueryDates(t *testing.T) {
	q := core.Query{
		CreatedSince: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedSince: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
	}
	got, err := buildQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("creation_time") != "2026-01-01T00:00:00Z" {
		t.Errorf("creation_time = %q", got.Get("creation_time"))
	}
	if got.Get("last_change_time") != "2026-02-15T00:00:00Z" {
		t.Errorf("last_change_time = %q", got.Get("last_change_time"))
	}
}

func TestBuildQueryRawPassthrough(t *testing.T) {
	got, err := buildQuery(core.Query{Raw: "quicksearch=ALL+kernel+panic"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("quicksearch") != "ALL kernel panic" {
		t.Errorf("got %v", got)
	}
}

func TestBuildQueryInvalidRaw(t *testing.T) {
	if _, err := buildQuery(core.Query{Raw: "%zz"}); err == nil {
		t.Fatal("expected error for invalid raw query string")
	}
}

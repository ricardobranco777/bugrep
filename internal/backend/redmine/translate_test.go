// SPDX-License-Identifier: BSD-2-Clause

package redmine

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
			name: "project id",
			q:    core.Query{Project: "myproject"},
			want: map[string]string{"project_id": "myproject", "status_id": "open"},
		},
		{
			name: "default state is open",
			q:    core.Query{},
			want: map[string]string{"status_id": "open"},
		},
		{
			name: "closed state",
			q:    core.Query{State: core.StateClosed},
			want: map[string]string{"status_id": "closed"},
		},
		{
			name: "all state uses wildcard",
			q:    core.Query{State: core.StateAll},
			want: map[string]string{"status_id": "*"},
		},
		{
			name: "assignee and reporter me pass through literally",
			q:    core.Query{Assignee: "me", Reporter: "me"},
			want: map[string]string{"assigned_to_id": "me", "author_id": "me"},
		},
		{
			name: "text search uses substring operator",
			q:    core.Query{Text: "crash"},
			want: map[string]string{"subject": "~crash"},
		},
		{
			name: "sort updated",
			q:    core.Query{Sort: core.SortUpdated},
			want: map[string]string{"sort": "updated_on:desc"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildQuery(c.q)
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

func TestBuildQueryDates(t *testing.T) {
	q := core.Query{
		CreatedSince: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedSince: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
	}
	got, err := buildQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("created_on") != ">=2026-01-01" {
		t.Errorf("created_on = %q", got.Get("created_on"))
	}
	if got.Get("updated_on") != ">=2026-02-15" {
		t.Errorf("updated_on = %q", got.Get("updated_on"))
	}
}

func TestBuildQueryRawPassthrough(t *testing.T) {
	got, err := buildQuery(core.Query{Raw: "status_id=7&project_id=42"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("status_id") != "7" || got.Get("project_id") != "42" {
		t.Errorf("got %v", got)
	}
}

func TestBuildQueryInvalidRaw(t *testing.T) {
	if _, err := buildQuery(core.Query{Raw: "%zz"}); err == nil {
		t.Fatal("expected error for invalid raw query string")
	}
}

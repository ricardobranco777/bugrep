// SPDX-License-Identifier: BSD-2-Clause

package launchpad

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

const testBaseURL = "https://api.launchpad.net/1.0"

func decodeJSONArray(t *testing.T, s string) []string {
	t.Helper()
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("expected a JSON array in %q: %v", s, err)
	}
	return out
}

func TestBuildSearchTasksQueryDefaultOpen(t *testing.T) {
	vals, err := buildSearchTasksQuery(core.Query{}, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("ws.op") != "searchTasks" {
		t.Errorf("ws.op = %q", vals.Get("ws.op"))
	}
	got := decodeJSONArray(t, vals.Get("status"))
	if len(got) != len(openStatuses) {
		t.Errorf("expected the open status list, got %v", got)
	}
}

func TestBuildSearchTasksQueryClosed(t *testing.T) {
	vals, err := buildSearchTasksQuery(core.Query{State: core.StateClosed}, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSONArray(t, vals.Get("status"))
	if len(got) != len(closedStatuses) {
		t.Errorf("expected the closed status list, got %v", got)
	}
}

func TestBuildSearchTasksQueryAllOmitsStatus(t *testing.T) {
	vals, err := buildSearchTasksQuery(core.Query{State: core.StateAll}, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("status") != "" {
		t.Errorf("expected no status filter for state:all, got %q", vals.Get("status"))
	}
}

func TestBuildSearchTasksQueryLabels(t *testing.T) {
	vals, err := buildSearchTasksQuery(core.Query{Labels: []string{"regression", "amdgpu"}}, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSONArray(t, vals.Get("tags"))
	if len(got) != 2 || got[0] != "regression" || got[1] != "amdgpu" {
		t.Errorf("got %v", got)
	}
	if vals.Get("tags_combinator") != "All" {
		t.Errorf("expected AND semantics via tags_combinator=All, got %q", vals.Get("tags_combinator"))
	}
}

func TestBuildSearchTasksQueryAssigneeReporter(t *testing.T) {
	vals, err := buildSearchTasksQuery(core.Query{Assignee: "jsmith", Reporter: "asmith"}, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("assignee") != testBaseURL+"/~jsmith" {
		t.Errorf("assignee = %q", vals.Get("assignee"))
	}
	if vals.Get("bug_reporter") != testBaseURL+"/~asmith" {
		t.Errorf("bug_reporter = %q", vals.Get("bug_reporter"))
	}
}

func TestBuildSearchTasksQueryMeUnsupported(t *testing.T) {
	if _, err := buildSearchTasksQuery(core.Query{Assignee: "me"}, testBaseURL); err == nil {
		t.Fatal(`expected an error for --assignee me`)
	}
	if _, err := buildSearchTasksQuery(core.Query{Reporter: "me"}, testBaseURL); err == nil {
		t.Fatal(`expected an error for --reporter me`)
	}
}

func TestBuildSearchTasksQueryDates(t *testing.T) {
	q := core.Query{
		CreatedSince: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedSince: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
	}
	vals, err := buildSearchTasksQuery(q, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("created_since") != "2026-01-01T00:00:00Z" {
		t.Errorf("created_since = %q", vals.Get("created_since"))
	}
	if vals.Get("modified_since") != "2026-02-15T00:00:00Z" {
		t.Errorf("modified_since = %q", vals.Get("modified_since"))
	}
}

func TestBuildSearchTasksQuerySort(t *testing.T) {
	cases := map[core.SortField]string{
		core.SortUpdated:  "-date_last_updated",
		core.SortCreated:  "-datecreated",
		core.SortPriority: "-importance",
	}
	for sort, want := range cases {
		vals, err := buildSearchTasksQuery(core.Query{Sort: sort}, testBaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if got := vals.Get("order_by"); got != want {
			t.Errorf("sort %q: order_by = %q, want %q", sort, got, want)
		}
	}
}

func TestBuildSearchTasksQueryRawForcesOp(t *testing.T) {
	vals, err := buildSearchTasksQuery(core.Query{Raw: "search_text=kernel"}, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("ws.op") != "searchTasks" {
		t.Errorf("raw passthrough must not be able to change the operation, got %q", vals.Get("ws.op"))
	}
	if vals.Get("search_text") != "kernel" {
		t.Errorf("got %v", vals)
	}
}

func TestParseTaskTitle(t *testing.T) {
	raw := `Bug #2167563 in mutter (Ubuntu): "Ubuntu 26.10 / gnome-shell crashed with SIGSEGV"`
	want := `Ubuntu 26.10 / gnome-shell crashed with SIGSEGV`
	if got := parseTaskTitle(raw); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseTaskTitleWithEmbeddedQuotes(t *testing.T) {
	raw := `Bug #1 in foo: "crash in "quoted" function"`
	want := `crash in "quoted" function`
	if got := parseTaskTitle(raw); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseTaskTitleFallback(t *testing.T) {
	raw := "no quotes here"
	if got := parseTaskTitle(raw); got != raw {
		t.Errorf("got %q, want the raw string unchanged", got)
	}
}

func TestUsernameFromLink(t *testing.T) {
	if got := usernameFromLink(testBaseURL+"/~jsmith", testBaseURL); got != "jsmith" {
		t.Errorf("got %q", got)
	}
	if got := usernameFromLink("", testBaseURL); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestLastPathSegment(t *testing.T) {
	if got := lastPathSegment(testBaseURL + "/bugs/2167563"); got != "2167563" {
		t.Errorf("got %q", got)
	}
}

func TestLatestOf(t *testing.T) {
	a := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	got := latestOf(&a, nil, &c, nil)
	if !got.Equal(c) {
		t.Errorf("got %v, want %v", got, c)
	}
	if got := latestOf(nil, nil); !got.IsZero() {
		t.Errorf("expected zero time when every argument is nil, got %v", got)
	}
}

func TestNormalizeState(t *testing.T) {
	if normalizeState("New") != core.StateOpen {
		t.Error("expected New to be open")
	}
	if normalizeState("Fix Released") != core.StateClosed {
		t.Error("expected Fix Released to be closed")
	}
	if normalizeState("Won't Fix") != core.StateClosed {
		t.Error("expected Won't Fix to be closed")
	}
}

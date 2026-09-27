// SPDX-License-Identifier: BSD-2-Clause

package redmine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func newTestBackend(t *testing.T, handler http.HandlerFunc) *Backend {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Backend{
		name:   "rm-test",
		client: &client{http: srv.Client(), baseURL: srv.URL, apiKey: "test-key"},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func rawIssue(id int, subject string, statusID int, statusName string) map[string]any {
	return map[string]any{
		"id": id, "subject": subject, "description": "some description",
		"status":      map[string]any{"id": statusID, "name": statusName},
		"priority":    map[string]any{"id": 4, "name": "High"},
		"author":      map[string]any{"id": 1, "name": "Alice"},
		"assigned_to": map[string]any{"id": 2, "name": "Bob"},
		"project":     map[string]any{"id": 10, "name": "myproject"},
		"created_on":  "2026-01-01T00:00:00Z",
		"updated_on":  "2026-02-01T00:00:00Z",
	}
}

func TestSearchOpenStateKnownFromFilter(t *testing.T) {
	// No /issue_statuses.json call should happen: the status_id=open filter
	// already tells us every result is open.
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/issue_statuses.json" {
			t.Fatal("should not need /issue_statuses.json when the filter already pins down state")
		}
		if r.URL.Query().Get("status_id") != "open" {
			t.Errorf("expected status_id=open, got %q", r.URL.Query().Get("status_id"))
		}
		writeJSON(t, w, map[string]any{
			"issues":      []any{rawIssue(1, "Something broke", 1, "New")},
			"total_count": 1,
		})
	})

	issues, err := b.Search(t.Context(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].State != core.StateOpen {
		t.Fatalf("unexpected result: %+v", issues)
	}
	got := issues[0]
	if got.Key != "1" || got.Ref() != "rm-test#1" {
		t.Errorf("unexpected key/ref: %+v", got)
	}
	if got.Assignee != "Bob" || got.Reporter != "Alice" {
		t.Errorf("unexpected assignee/reporter: %+v", got)
	}
	if got.URL != b.client.baseURL+"/issues/1" {
		t.Errorf("unexpected URL: %s", got.URL)
	}
}

func TestSearchAllStateUsesClosedStatusLookup(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/issue_statuses.json":
			writeJSON(t, w, map[string]any{
				"issue_statuses": []any{
					map[string]any{"id": 1, "name": "New", "is_closed": false},
					map[string]any{"id": 5, "name": "Closed", "is_closed": true},
				},
			})
		case "/issues.json":
			writeJSON(t, w, map[string]any{
				"issues": []any{
					rawIssue(1, "Open one", 1, "New"),
					rawIssue(2, "Closed one", 5, "Closed"),
				},
				"total_count": 2,
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	issues, err := b.Search(t.Context(), core.Query{State: core.StateAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[0].State != core.StateOpen || issues[1].State != core.StateClosed {
		t.Errorf("unexpected states: %v / %v", issues[0].State, issues[1].State)
	}
}

func TestSearchClosedStatusLookupFailsGracefully(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/issue_statuses.json":
			w.WriteHeader(http.StatusForbidden)
		case "/issues.json":
			writeJSON(t, w, map[string]any{"issues": []any{rawIssue(1, "x", 5, "Closed")}, "total_count": 1})
		}
	})
	issues, err := b.Search(t.Context(), core.Query{State: core.StateAll})
	if err != nil {
		t.Fatalf("expected search to succeed despite the failed status lookup, got %v", err)
	}
	if issues[0].State != core.StateOpen {
		t.Errorf("expected fallback to open when the status lookup fails, got %q", issues[0].State)
	}
}

func TestSearchPagination(t *testing.T) {
	var requests int
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		offset := r.URL.Query().Get("offset")
		var issues []any
		if offset == "0" {
			for i := range pageSize {
				issues = append(issues, rawIssue(i+1, "x", 1, "New"))
			}
		} else {
			issues = append(issues, rawIssue(1000, "last", 1, "New"))
		}
		writeJSON(t, w, map[string]any{"issues": issues, "total_count": pageSize + 1})
	})

	issues, err := b.Search(t.Context(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != pageSize+1 {
		t.Fatalf("expected %d issues, got %d", pageSize+1, len(issues))
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
}

func TestGetWithComments(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/issue_statuses.json" {
			writeJSON(t, w, map[string]any{"issue_statuses": []any{map[string]any{"id": 1, "name": "New", "is_closed": false}}})
			return
		}
		if r.URL.Path != "/issues/1.json" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("include") != "journals" {
			t.Errorf("expected include=journals, got %q", r.URL.Query().Get("include"))
		}
		issue := rawIssue(1, "Something broke", 1, "New")
		issue["journals"] = []any{
			map[string]any{"user": map[string]any{"id": 2, "name": "Bob"}, "notes": "confirmed", "created_on": "2026-01-02T00:00:00Z"},
			map[string]any{"user": map[string]any{"id": 3, "name": "System"}, "notes": "", "created_on": "2026-01-03T00:00:00Z"},
		}
		writeJSON(t, w, map[string]any{"issue": issue})
	})

	issue, err := b.Get(t.Context(), "1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Author != "Bob" || issue.Comments[0].Body != "confirmed" {
		t.Errorf("expected the empty-notes journal filtered out, got %+v", issue.Comments)
	}
}

func TestGetWithoutComments(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/issue_statuses.json" {
			writeJSON(t, w, map[string]any{"issue_statuses": []any{map[string]any{"id": 1, "name": "New", "is_closed": false}}})
			return
		}
		if r.URL.Query().Get("include") != "" {
			t.Errorf("expected no include param, got %q", r.URL.Query().Get("include"))
		}
		writeJSON(t, w, map[string]any{"issue": rawIssue(1, "Something broke", 1, "New")})
	})
	issue, err := b.Get(t.Context(), "1", false)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Comments != nil {
		t.Errorf("expected no comments fetched, got %+v", issue.Comments)
	}
}

func TestUnassignedIssueHasEmptyAssignee(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		issue := rawIssue(1, "Unassigned", 1, "New")
		delete(issue, "assigned_to")
		writeJSON(t, w, map[string]any{"issues": []any{issue}, "total_count": 1})
	})
	issues, err := b.Search(t.Context(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if issues[0].Assignee != "" {
		t.Errorf("expected empty assignee, got %q", issues[0].Assignee)
	}
}

func TestErrorMessageSurfaced(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		writeJSON(t, w, map[string]any{"errors": []string{"Project does not exist"}})
	})
	_, err := b.Search(t.Context(), core.Query{Project: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "Project does not exist") {
		t.Fatalf("expected redmine error message surfaced, got %v", err)
	}
}

func TestCheckAuthenticated(t *testing.T) {
	var gotPath, gotHeader string
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Redmine-API-Key")
		writeJSON(t, w, map[string]any{"user": map[string]any{"id": 1, "login": "alice"}})
	})
	if err := b.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/users/current.json" {
		t.Errorf("expected /users/current.json, got %s", gotPath)
	}
	if gotHeader != "test-key" {
		t.Errorf("expected api key header, got %q", gotHeader)
	}
}

func TestCheckUnauthenticated(t *testing.T) {
	var gotPath string
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, map[string]any{"issue_statuses": []any{}})
	})
	b.client.apiKey = ""
	if err := b.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/issue_statuses.json" {
		t.Errorf("expected /issue_statuses.json, got %s", gotPath)
	}
}

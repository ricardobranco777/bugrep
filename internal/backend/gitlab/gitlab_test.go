// SPDX-License-Identifier: BSD-2-Clause

package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func newTestBackend(t *testing.T, sc scope, handler http.HandlerFunc) *Backend {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Backend{
		name:   "gl-test",
		client: &client{http: srv.Client(), baseURL: srv.URL, token: "test-token"},
		scope:  sc,
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func sampleIssue(iid int, refFull string) issueItem {
	it := issueItem{
		IID: iid, Title: "Something broke", State: "opened",
		WebURL:    "https://gitlab.com/" + refFull,
		Author:    glUserRef{Username: "alice"},
		Assignees: []glUserRef{{Username: "bob"}},
		Labels:    []string{"bug"},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	it.References.Full = refFull
	return it
}

func TestSearchSingleProject(t *testing.T) {
	b := newTestBackend(t, scope{Projects: []string{"group/project"}}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/group/project/issues" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		// The project path must reach the server as a single escaped
		// segment (%2F), not as literal sub-path components: GitLab uses
		// that to tell a project's :id apart from a nested route.
		if !strings.Contains(r.URL.EscapedPath(), "group%2Fproject") {
			t.Errorf("expected an escaped slash in the project id, got %s", r.URL.EscapedPath())
		}
		if r.URL.Query().Get("state") != "opened" {
			t.Errorf("expected state=opened, got %q", r.URL.Query().Get("state"))
		}
		writeJSON(t, w, []issueItem{sampleIssue(17, "group/project#17")})
	})

	issues, err := b.Search(context.Background(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	got := issues[0]
	if got.Key != "group/project#17" || got.Ref() != "gl-test#group/project#17" {
		t.Errorf("unexpected key/ref: %+v", got)
	}
	if got.Project != "group/project" {
		t.Errorf("unexpected project: %q", got.Project)
	}
	if got.Assignee != "bob" || got.Reporter != "alice" {
		t.Errorf("unexpected assignee/reporter: %+v", got)
	}
	if got.State != core.StateOpen {
		t.Errorf("expected normalized open state, got %q", got.State)
	}
}

func TestSearchFansOutAcrossTargets(t *testing.T) {
	var mu sync.Mutex
	var pathsHit []string
	b := newTestBackend(t, scope{Groups: []string{"mygroup"}, Projects: []string{"other/project"}}, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pathsHit = append(pathsHit, r.URL.Path)
		mu.Unlock()

		switch r.URL.Path {
		case "/api/v4/groups/mygroup/issues":
			writeJSON(t, w, []issueItem{sampleIssue(1, "mygroup/proj-a#1")})
		case "/api/v4/projects/other/project/issues":
			writeJSON(t, w, []issueItem{sampleIssue(2, "other/project#2")})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	issues, err := b.Search(context.Background(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues from 2 targets, got %d", len(issues))
	}
	sort.Strings(pathsHit)
	want := []string{"/api/v4/groups/mygroup/issues", "/api/v4/projects/other/project/issues"}
	for i := range want {
		if pathsHit[i] != want[i] {
			t.Errorf("paths hit = %v, want %v", pathsHit, want)
		}
	}
}

func TestSearchPartialFailureKeepsGoodResults(t *testing.T) {
	b := newTestBackend(t, scope{Groups: []string{"good"}, Projects: []string{"bad/project"}}, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "bad") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(t, w, []issueItem{sampleIssue(1, "good/proj#1")})
	})

	issues, err := b.Search(context.Background(), core.Query{})
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue from the surviving target, got %d", len(issues))
	}
}

func TestSearchAllTargetsFail(t *testing.T) {
	b := newTestBackend(t, scope{Groups: []string{"mygroup"}}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := b.Search(context.Background(), core.Query{}); err == nil {
		t.Fatal("expected error when every target fails")
	}
}

func TestSearchPagination(t *testing.T) {
	b := newTestBackend(t, scope{Projects: []string{"group/project"}}, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var items []issueItem
		if page == "1" {
			for i := range pageSize {
				items = append(items, sampleIssue(i+1, "group/project#1"))
			}
		} else {
			items = append(items, sampleIssue(101, "group/project#101"))
		}
		writeJSON(t, w, items)
	})

	issues, err := b.Search(context.Background(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != pageSize+1 {
		t.Fatalf("expected %d issues, got %d", pageSize+1, len(issues))
	}
}

func TestSearchProjectOutsideScope(t *testing.T) {
	b := newTestBackend(t, scope{Groups: []string{"mygroup"}}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request should be made for an out-of-scope project")
	})
	if _, err := b.Search(context.Background(), core.Query{Project: "other/project"}); err == nil {
		t.Fatal("expected error for a project outside the configured scope")
	}
}

func TestSearchResolvesMe(t *testing.T) {
	b := newTestBackend(t, scope{Projects: []string{"group/project"}}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/user":
			writeJSON(t, w, map[string]string{"username": "alice"})
		case "/api/v4/projects/group/project/issues":
			if got := r.URL.Query().Get("assignee_username"); got != "alice" {
				t.Errorf(`expected assignee_username=alice (resolved from "me"), got %q`, got)
			}
			writeJSON(t, w, []issueItem{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	if _, err := b.Search(context.Background(), core.Query{Assignee: "me"}); err != nil {
		t.Fatal(err)
	}
}

func TestGetWithComments(t *testing.T) {
	b := newTestBackend(t, scope{Projects: []string{"group/project"}}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/group/project/issues/17":
			writeJSON(t, w, sampleIssue(17, "group/project#17"))
		case "/api/v4/projects/group/project/issues/17/notes":
			writeJSON(t, w, []noteItem{
				{Author: glUserRef{Username: "bob"}, Body: "confirmed", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				{Author: glUserRef{Username: "system"}, Body: "changed status", System: true},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	issue, err := b.Get(context.Background(), "group/project#17", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Author != "bob" {
		t.Errorf("expected system note filtered out, got %+v", issue.Comments)
	}
}

func TestGetInvalidKey(t *testing.T) {
	b := newTestBackend(t, scope{Projects: []string{"group/project"}}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request should be made for an invalid key")
	})
	if _, err := b.Get(context.Background(), "not-a-valid-key", false); err == nil {
		t.Fatal("expected error for a key without project#number")
	}
}

func TestCheckAuthenticated(t *testing.T) {
	var gotPath, gotHeader string
	b := newTestBackend(t, scope{Projects: []string{"group/project"}}, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("PRIVATE-TOKEN")
		writeJSON(t, w, map[string]string{"username": "alice"})
	})
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v4/user" {
		t.Errorf("expected /api/v4/user, got %s", gotPath)
	}
	if gotHeader != "test-token" {
		t.Errorf("expected PRIVATE-TOKEN header, got %q", gotHeader)
	}
}

func TestCheckUnauthenticated(t *testing.T) {
	var gotPath string
	b := newTestBackend(t, scope{Projects: []string{"group/project"}}, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, []map[string]string{})
	})
	b.client.token = ""
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v4/projects" {
		t.Errorf("expected /api/v4/projects, got %s", gotPath)
	}
}

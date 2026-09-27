// SPDX-License-Identifier: BSD-2-Clause

package gitea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func newTestBackend(t *testing.T, sc scope, handler http.HandlerFunc) *Backend {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Backend{
		name:   "gt-test",
		typ:    "forgejo",
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

func sampleItem(number int, repoFullName string) issueItem {
	return issueItem{
		Number:     number,
		Title:      "Something broke",
		State:      "open",
		HTMLURL:    "https://example.org/" + repoFullName + "/issues/1",
		User:       giteaUserRef{Login: "alice"},
		Assignees:  []giteaUserRef{{Login: "bob"}},
		Labels:     []giteaLabel{{Name: "bug"}},
		Repository: &giteaRepoRef{FullName: repoFullName},
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestSearchRepoTarget(t *testing.T) {
	b := newTestBackend(t, scope{Repos: []string{"owner/repo"}}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/owner/repo/issues" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("type") != "issues" {
			t.Errorf("expected type=issues, got %q", r.URL.Query().Get("type"))
		}
		writeJSON(t, w, []issueItem{
			sampleItem(1, "owner/repo"),
			{Number: 2, Repository: &giteaRepoRef{FullName: "owner/repo"}, PullRequest: &struct{}{}},
		})
	})

	issues, err := b.Search(context.Background(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (PR filtered out), got %d: %+v", len(issues), issues)
	}
	got := issues[0]
	if got.Key != "owner/repo#1" || got.Ref() != "gt-test#owner/repo#1" {
		t.Errorf("unexpected key/ref: %+v", got)
	}
	if got.Type != "forgejo" {
		t.Errorf("expected Type to reflect the configured tracker type, got %q", got.Type)
	}
	if got.Assignee != "bob" || got.Reporter != "alice" {
		t.Errorf("unexpected assignee/reporter: %+v", got)
	}
}

func TestSearchOrgTarget(t *testing.T) {
	b := newTestBackend(t, scope{Orgs: []string{"myorg"}}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/issues/search" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("owner") != "myorg" {
			t.Errorf("expected owner=myorg, got %q", r.URL.Query().Get("owner"))
		}
		writeJSON(t, w, []issueItem{sampleItem(1, "myorg/somerepo")})
	})

	issues, err := b.Search(context.Background(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}

func TestSearchOrgTargetSkipsLiteralAssignee(t *testing.T) {
	b := newTestBackend(t, scope{Orgs: []string{"myorg"}}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("assigned_by") != "" || r.URL.Query().Get("assigned") != "" {
			t.Errorf("expected no assignee filter applied to an org-wide search, got %v", r.URL.Query())
		}
		writeJSON(t, w, []issueItem{})
	})
	// Should succeed (with a warning printed, not an error) rather than fail outright.
	if _, err := b.Search(context.Background(), core.Query{Assignee: "alice"}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchPagination(t *testing.T) {
	var requests int
	b := newTestBackend(t, scope{Repos: []string{"owner/repo"}}, func(w http.ResponseWriter, r *http.Request) {
		requests++
		page := r.URL.Query().Get("page")
		var items []issueItem
		if page == "1" {
			for i := range pageSize {
				items = append(items, sampleItem(i+1, "owner/repo"))
			}
		} else {
			items = append(items, sampleItem(1000, "owner/repo"))
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
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
}

func TestSearchProjectOutsideScope(t *testing.T) {
	b := newTestBackend(t, scope{Orgs: []string{"myorg"}}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request should be made for an out-of-scope project")
	})
	if _, err := b.Search(context.Background(), core.Query{Project: "other/repo"}); err == nil {
		t.Fatal("expected error for a project outside the configured scope")
	}
}

func TestGetWithComments(t *testing.T) {
	b := newTestBackend(t, scope{Repos: []string{"owner/repo"}}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/owner/repo/issues/1":
			writeJSON(t, w, sampleItem(1, "owner/repo"))
		case "/api/v1/repos/owner/repo/issues/1/comments":
			writeJSON(t, w, []commentItem{{User: giteaUserRef{Login: "bob"}, Body: "confirmed", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	issue, err := b.Get(context.Background(), "owner/repo#1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Author != "bob" {
		t.Errorf("unexpected comments: %+v", issue.Comments)
	}
}

func TestGetInvalidKey(t *testing.T) {
	b := newTestBackend(t, scope{Repos: []string{"owner/repo"}}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request should be made for an invalid key")
	})
	if _, err := b.Get(context.Background(), "not-a-valid-key", false); err == nil {
		t.Fatal("expected error for a key without owner/repo#number")
	}
}

func TestCheckAuthenticated(t *testing.T) {
	var gotPath, gotHeader string
	b := newTestBackend(t, scope{Repos: []string{"owner/repo"}}, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("Authorization")
		writeJSON(t, w, map[string]string{"login": "alice"})
	})
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/user" {
		t.Errorf("expected /api/v1/user, got %s", gotPath)
	}
	if gotHeader != "token test-token" {
		t.Errorf("expected token auth header, got %q", gotHeader)
	}
}

func TestCheckUnauthenticated(t *testing.T) {
	var gotPath string
	b := newTestBackend(t, scope{Repos: []string{"owner/repo"}}, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, map[string]string{"version": "1.22.0"})
	})
	b.client.token = ""
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/version" {
		t.Errorf("expected /api/v1/version, got %s", gotPath)
	}
}

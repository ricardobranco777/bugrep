// SPDX-License-Identifier: BSD-2-Clause

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// newTestBackend builds a Backend pointed at an httptest.Server, bypassing
// New/config so tests don't need a real token or the registry.
func newTestBackend(t *testing.T, handler http.HandlerFunc) (*Backend, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Backend{
		name:   "gh-test",
		client: &client{http: srv.Client(), baseURL: srv.URL, token: "test-token"},
		scope:  scope{Orgs: []string{"myorg"}},
	}, srv
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSinglePage(t *testing.T) {
	b, _ := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/issues" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); !strings.Contains(got, "org:myorg") {
			t.Errorf("query missing scope: %q", got)
		}
		writeJSON(t, w, searchResponse{
			TotalCount: 2,
			Items: []issueItem{
				{
					Number: 42, Title: "Something broke", State: "open",
					HTMLURL:       "https://github.com/owner/repo/issues/42",
					RepositoryURL: "https://api.github.com/repos/owner/repo",
					User:          ghUserRef{Login: "alice"},
					Assignees:     []ghUserRef{{Login: "bob"}},
					Labels:        []ghLabel{{Name: "bug"}},
					CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
					UpdatedAt:     time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					Number: 43, Title: "A pull request", State: "open",
					RepositoryURL: "https://api.github.com/repos/owner/repo",
					PullRequest:   &struct{}{},
				},
			},
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
	if got.Key != "owner/repo#42" || got.Ref() != "gh-test#owner/repo#42" {
		t.Errorf("unexpected key/ref: %+v", got)
	}
	if got.Assignee != "bob" || got.Reporter != "alice" {
		t.Errorf("unexpected assignee/reporter: %+v", got)
	}
	if got.Project != "owner/repo" {
		t.Errorf("unexpected project: %q", got.Project)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "bug" {
		t.Errorf("unexpected labels: %v", got.Labels)
	}
}

func TestSearchPagination(t *testing.T) {
	var requests []string
	b, _ := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query().Get("page"))
		page := r.URL.Query().Get("page")

		items := make([]issueItem, 0)
		switch page {
		case "1":
			for i := range searchPageSize {
				items = append(items, issueItem{
					Number: i + 1, State: "open",
					RepositoryURL: "https://api.github.com/repos/owner/repo",
				})
			}
		case "2":
			items = append(items, issueItem{
				Number: 101, State: "open",
				RepositoryURL: "https://api.github.com/repos/owner/repo",
			})
		}
		writeJSON(t, w, searchResponse{TotalCount: searchPageSize + 1, Items: items})
	})

	issues, err := b.Search(context.Background(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != searchPageSize+1 {
		t.Fatalf("expected %d issues, got %d", searchPageSize+1, len(issues))
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests (one per page), got %d: %v", len(requests), requests)
	}
}

func TestSearchUnauthorized(t *testing.T) {
	b, _ := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := b.Search(context.Background(), core.Query{State: core.StateOpen}); err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestGetWithComments(t *testing.T) {
	b, _ := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/issues/42":
			writeJSON(t, w, issueItem{
				Number: 42, Title: "Something broke", State: "closed", StateReason: "completed",
				HTMLURL:       "https://github.com/owner/repo/issues/42",
				RepositoryURL: "https://api.github.com/repos/owner/repo",
				User:          ghUserRef{Login: "alice"},
				Body:          "It's broken.",
			})
		case "/repos/owner/repo/issues/42/comments":
			if r.URL.Query().Get("page") == "1" {
				writeJSON(t, w, []commentItem{{User: ghUserRef{Login: "bob"}, Body: "confirmed", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}})
			} else {
				writeJSON(t, w, []commentItem{})
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	issue, err := b.Get(context.Background(), "owner/repo#42", true)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Status != "completed" {
		t.Errorf("expected status_reason to surface as Status, got %q", issue.Status)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Author != "bob" {
		t.Errorf("unexpected comments: %+v", issue.Comments)
	}
}

func TestGetInvalidKey(t *testing.T) {
	b, _ := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request should be made for an invalid key")
	})
	if _, err := b.Get(context.Background(), "not-a-valid-key", false); err == nil {
		t.Fatal("expected error for a key without owner/repo#number")
	}
}

func TestCheckAuthenticated(t *testing.T) {
	var gotPath string
	b, _ := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing/incorrect Authorization header: %q", r.Header.Get("Authorization"))
		}
		writeJSON(t, w, map[string]string{"login": "alice"})
	})
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/user" {
		t.Errorf("expected /user, got %s", gotPath)
	}
}

func TestCheckUnauthenticatedUsesRateLimit(t *testing.T) {
	var gotPath string
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, map[string]string{})
	})
	b.client.token = ""
	_ = srv
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rate_limit" {
		t.Errorf("expected /rate_limit, got %s", gotPath)
	}
}

func TestRateLimitError(t *testing.T) {
	b, _ := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix()))
		w.WriteHeader(http.StatusForbidden)
	})
	err := b.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("expected rate limit error, got %v", err)
	}
}

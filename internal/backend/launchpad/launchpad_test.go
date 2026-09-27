// SPDX-License-Identifier: BSD-2-Clause

package launchpad

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// newTestServer starts an httptest.Server whose handler is provided as a
// closure that can reference srv.URL (needed to build person/bug links and
// next_collection_link values that point back at the same server).
func newTestServer(t *testing.T, handler func(srv *httptest.Server) http.HandlerFunc) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(srv)(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestBackend(srv *httptest.Server, targets []string) *Backend {
	return &Backend{
		name:    "lp-test",
		client:  &client{http: srv.Client(), baseURL: srv.URL},
		targets: targets,
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func sampleTask(baseURL string, bugID int, status string) map[string]any {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirmed := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	id := strconv.Itoa(bugID)
	return map[string]any{
		"bug_link":                baseURL + "/bugs/" + id,
		"status":                  status,
		"importance":              "High",
		"assignee_link":           baseURL + "/~bob",
		"owner_link":              baseURL + "/~alice",
		"bug_target_display_name": "linux (Ubuntu)",
		"date_created":            created.Format(time.RFC3339),
		"date_confirmed":          confirmed.Format(time.RFC3339),
		"title":                   `Bug #` + id + ` in linux (Ubuntu): "Kernel panic on boot"`,
		"web_link":                baseURL + "/ubuntu/+source/linux/+bug/" + id,
	}
}

func TestSearchSingleTarget(t *testing.T) {
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ubuntu/+source/linux" {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			if r.URL.Query().Get("ws.op") != "searchTasks" {
				t.Errorf("expected ws.op=searchTasks, got %q", r.URL.Query().Get("ws.op"))
			}
			writeJSON(t, w, map[string]any{"entries": []any{sampleTask(srv.URL, 2167563, "New")}})
		}
	})
	b := newTestBackend(srv, []string{"ubuntu/+source/linux"})

	issues, err := b.Search(context.Background(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	got := issues[0]
	if got.Key != "2167563" || got.Ref() != "lp-test#2167563" {
		t.Errorf("unexpected key/ref: %+v", got)
	}
	if got.Title != "Kernel panic on boot" {
		t.Errorf("unexpected title: %q", got.Title)
	}
	if got.Assignee != "bob" || got.Reporter != "alice" {
		t.Errorf("unexpected assignee/reporter: %+v", got)
	}
	if got.State != core.StateOpen {
		t.Errorf("expected open state for status=New, got %q", got.State)
	}
	if got.Updated.IsZero() {
		t.Error("expected Updated to be approximated from the task's transition dates")
	}
}

func TestSearchClosedStatus(t *testing.T) {
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, map[string]any{"entries": []any{sampleTask(srv.URL, 1, "Fix Released")}})
		}
	})
	b := newTestBackend(srv, []string{"ubuntu/+source/linux"})

	issues, err := b.Search(context.Background(), core.Query{State: core.StateClosed})
	if err != nil {
		t.Fatal(err)
	}
	if issues[0].State != core.StateClosed {
		t.Errorf("expected closed state for Fix Released, got %q", issues[0].State)
	}
}

func TestSearchPagination(t *testing.T) {
	var requests int
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			requests++
			switch r.URL.Path {
			case "/ubuntu/+source/linux":
				writeJSON(t, w, map[string]any{
					"entries":              []any{sampleTask(srv.URL, 1, "New")},
					"next_collection_link": srv.URL + "/next-page",
				})
			case "/next-page":
				writeJSON(t, w, map[string]any{"entries": []any{sampleTask(srv.URL, 2, "New")}})
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		}
	})
	b := newTestBackend(srv, []string{"ubuntu/+source/linux"})

	issues, err := b.Search(context.Background(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues across 2 pages, got %d", len(issues))
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
}

func TestSearchFansOutAcrossTargets(t *testing.T) {
	var mu sync.Mutex
	var pathsHit []string
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			pathsHit = append(pathsHit, r.URL.Path)
			mu.Unlock()
			writeJSON(t, w, map[string]any{"entries": []any{sampleTask(srv.URL, 1, "New")}})
		}
	})
	b := newTestBackend(srv, []string{"ubuntu/+source/linux", "debian"})

	issues, err := b.Search(context.Background(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues from 2 targets, got %d", len(issues))
	}
	if len(pathsHit) != 2 {
		t.Fatalf("expected 2 requests, got %d: %v", len(pathsHit), pathsHit)
	}
}

func TestSearchPartialFailureKeepsGoodResults(t *testing.T) {
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "bad") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeJSON(t, w, map[string]any{"entries": []any{sampleTask(srv.URL, 1, "New")}})
		}
	})
	b := newTestBackend(srv, []string{"good", "bad"})

	issues, err := b.Search(context.Background(), core.Query{})
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue from the surviving target, got %d", len(issues))
	}
}

func TestSearchAllTargetsFail(t *testing.T) {
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	b := newTestBackend(srv, []string{"ubuntu"})
	if _, err := b.Search(context.Background(), core.Query{}); err == nil {
		t.Fatal("expected error when every target fails")
	}
}

func TestSearchProjectOutsideScope(t *testing.T) {
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("no HTTP request should be made for an out-of-scope project")
		}
	})
	b := newTestBackend(srv, []string{"ubuntu"})

	if _, err := b.Search(context.Background(), core.Query{Project: "debian"}); err == nil {
		t.Fatal("expected error for a target outside the configured scope")
	}
}

func TestGetWithTaskAndComments(t *testing.T) {
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			base := srv.URL
			switch r.URL.Path {
			case "/bugs/2167563":
				writeJSON(t, w, map[string]any{
					"title": "Kernel panic on boot", "description": "It panics.",
					"tags": []string{"regression"}, "owner_link": base + "/~alice",
					"date_created": "2026-01-01T00:00:00Z", "date_last_updated": "2026-02-01T00:00:00Z",
					"web_link":                  base + "/bugs/2167563",
					"bug_tasks_collection_link": base + "/bugs/2167563/bug_tasks",
					"messages_collection_link":  base + "/bugs/2167563/messages",
				})
			case "/bugs/2167563/bug_tasks":
				writeJSON(t, w, map[string]any{"entries": []any{sampleTask(base, 2167563, "Confirmed")}})
			case "/bugs/2167563/messages":
				writeJSON(t, w, map[string]any{
					"entries": []any{
						map[string]any{"content": "It panics.", "owner_link": base + "/~alice", "date_created": "2026-01-01T00:00:00Z"},
						map[string]any{"content": "confirmed", "owner_link": base + "/~bob", "date_created": "2026-01-02T00:00:00Z"},
					},
				})
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		}
	})
	b := newTestBackend(srv, []string{"ubuntu"})

	issue, err := b.Get(context.Background(), "2167563", true)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Status != "Confirmed" || issue.Priority != "High" {
		t.Errorf("unexpected task-derived fields: %+v", issue)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Body != "confirmed" {
		t.Errorf("expected message #0 (the bug's own report) skipped, got %+v", issue.Comments)
	}
}

func TestGetTaskLookupFailureDegradesGracefully(t *testing.T) {
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/bugs/1":
				writeJSON(t, w, map[string]any{
					"title": "Something", "bug_tasks_collection_link": srv.URL + "/bugs/1/bug_tasks",
				})
			case "/bugs/1/bug_tasks":
				w.WriteHeader(http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		}
	})
	b := newTestBackend(srv, []string{"ubuntu"})

	issue, err := b.Get(context.Background(), "1", false)
	if err != nil {
		t.Fatalf("expected Get to degrade gracefully, got error: %v", err)
	}
	if issue.Title != "Something" {
		t.Errorf("expected the bug's own fields still populated, got %+v", issue)
	}
	if issue.State != core.StateOpen {
		t.Errorf("expected the documented open fallback, got %q", issue.State)
	}
}

func TestCheck(t *testing.T) {
	var gotPath string
	srv := newTestServer(t, func(srv *httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			writeJSON(t, w, map[string]string{})
		}
	})
	b := newTestBackend(srv, []string{"ubuntu/+source/linux"})

	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/ubuntu/+source/linux" {
		t.Errorf("expected the first configured target, got %s", gotPath)
	}
}

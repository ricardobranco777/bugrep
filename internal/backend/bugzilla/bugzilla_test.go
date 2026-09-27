// SPDX-License-Identifier: BSD-2-Clause

package bugzilla

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func newTestBackend(t *testing.T, handler http.HandlerFunc) *Backend {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Backend{
		name:   "bz-test",
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

func rawBug(id int, summary string) map[string]any {
	return map[string]any{
		"id": id, "summary": summary, "status": "NEW", "resolution": "",
		"priority": "high", "severity": "normal",
		"assigned_to": "bob@example.com", "creator": "alice@example.com",
		"keywords": []string{"regression"}, "product": "Fedora", "component": "kernel",
		"creation_time": "2026-01-01T00:00:00Z", "last_change_time": "2026-02-01T00:00:00Z",
	}
}

func TestSearchSinglePage(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/bug" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-BUGZILLA-API-KEY") != "test-key" {
			t.Errorf("missing api key header")
		}
		writeJSON(t, w, map[string]any{"bugs": []any{rawBug(2234567, "Something broke")}})
	})

	issues, err := b.Search(t.Context(), core.Query{State: core.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	got := issues[0]
	if got.Key != "2234567" || got.Ref() != "bz-test#2234567" {
		t.Errorf("unexpected key/ref: %+v", got)
	}
	if got.State != core.StateOpen {
		t.Errorf("expected open state for empty resolution, got %q", got.State)
	}
	if got.Assignee != "bob@example.com" || got.Reporter != "alice@example.com" {
		t.Errorf("unexpected assignee/reporter: %+v", got)
	}
	if got.URL != b.client.baseURL+"/show_bug.cgi?id=2234567" {
		t.Errorf("unexpected URL: %s", got.URL)
	}
}

func TestSearchComponentAsArray(t *testing.T) {
	// Red Hat's Bugzilla fork (bugzilla.redhat.com) returns "component" as
	// a JSON array (multi-component support), unlike stock Bugzilla, which
	// returns a plain string.
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		bug := rawBug(1, "Kernel oops")
		bug["component"] = []string{"kernel", "GFS-kernel"}
		writeJSON(t, w, map[string]any{"bugs": []any{bug}})
	})
	issues, err := b.Search(t.Context(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	components, _ := issues[0].Extra["components"].([]string)
	if len(components) != 2 || components[0] != "kernel" || components[1] != "GFS-kernel" {
		t.Errorf("expected both components preserved, got %v", components)
	}
}

func TestSearchComponentAsPlainString(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"bugs": []any{rawBug(1, "Something broke")}})
	})
	issues, err := b.Search(t.Context(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	components, _ := issues[0].Extra["components"].([]string)
	if len(components) != 1 || components[0] != "kernel" {
		t.Errorf("expected the plain string component wrapped in a slice, got %v", components)
	}
}

func TestSearchClosedResolution(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		bug := rawBug(1, "Fixed thing")
		bug["resolution"] = "FIXED"
		bug["status"] = "CLOSED"
		writeJSON(t, w, map[string]any{"bugs": []any{bug}})
	})
	issues, err := b.Search(t.Context(), core.Query{State: core.StateClosed})
	if err != nil {
		t.Fatal(err)
	}
	if issues[0].State != core.StateClosed {
		t.Errorf("expected closed state, got %q", issues[0].State)
	}
	if issues[0].Status != "CLOSED (FIXED)" {
		t.Errorf("expected status to include resolution, got %q", issues[0].Status)
	}
}

func TestSearchPagination(t *testing.T) {
	var requests int
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		offset := r.URL.Query().Get("offset")
		var bugs []any
		if offset == "0" {
			for i := range pageSize {
				bugs = append(bugs, rawBug(i+1, "bug"))
			}
		} else {
			bugs = append(bugs, rawBug(1000, "last bug"))
		}
		writeJSON(t, w, map[string]any{"bugs": bugs})
	})

	issues, err := b.Search(t.Context(), core.Query{})
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

func TestSearchResolvesMe(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/whoami":
			writeJSON(t, w, map[string]string{"name": "alice@example.com"})
		case "/rest/bug":
			if got := r.URL.Query().Get("assigned_to"); got != "alice@example.com" {
				t.Errorf(`expected assigned_to=alice@example.com (resolved from "me"), got %q`, got)
			}
			writeJSON(t, w, map[string]any{"bugs": []any{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	if _, err := b.Search(t.Context(), core.Query{Assignee: "me"}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchResolvesMeFromConfiguredUserWithoutWhoami(t *testing.T) {
	// When `user` is set in config, "me" resolves without ever calling
	// /rest/whoami — required on instances (bugzilla.suse.com) that don't
	// expose it at all.
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/whoami" {
			t.Fatal("should not call /rest/whoami when \"user\" is configured")
		}
		if got := r.URL.Query().Get("assigned_to"); got != "bob@example.com" {
			t.Errorf(`expected assigned_to=bob@example.com from the configured user, got %q`, got)
		}
		writeJSON(t, w, map[string]any{"bugs": []any{}})
	})
	b.login = "bob@example.com"

	if _, err := b.Search(t.Context(), core.Query{Assignee: "me"}); err != nil {
		t.Fatal(err)
	}
}

func TestGetWithComments(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/bug/2234567":
			writeJSON(t, w, map[string]any{"bugs": []any{rawBug(2234567, "Something broke")}})
		case "/rest/bug/2234567/comment":
			writeJSON(t, w, map[string]any{
				"bugs": map[string]any{
					"2234567": map[string]any{
						"comments": []any{
							map[string]any{"creator": "bob@example.com", "creation_time": "2026-01-02T00:00:00Z", "text": "confirmed"},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	issue, err := b.Get(t.Context(), "2234567", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Author != "bob@example.com" || issue.Comments[0].Body != "confirmed" {
		t.Errorf("unexpected comments: %+v", issue.Comments)
	}
	if !issue.Comments[0].Created.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("unexpected comment time: %v", issue.Comments[0].Created)
	}
}

func TestGetNotFound(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"bugs": []any{}, "faults": []any{map[string]any{"id": 999, "faultString": "not found"}}})
	})
	if _, err := b.Get(t.Context(), "999", false); err == nil {
		t.Fatal("expected error for empty bugs array")
	}
}

func TestErrorEmbeddedIn200Response(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		// Bugzilla often reports errors as HTTP 200 with an "error" body.
		writeJSON(t, w, map[string]any{"error": true, "message": "Invalid Bugzilla_api_key.", "code": 306})
	})
	_, err := b.Search(t.Context(), core.Query{})
	if err == nil || !strings.Contains(err.Error(), "Invalid Bugzilla_api_key") {
		t.Fatalf("expected embedded error message surfaced, got %v", err)
	}
}

func TestErrorNon2xxStatus(t *testing.T) {
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(t, w, map[string]any{"error": true, "message": "Bad request.", "code": 32000})
	})
	_, err := b.Search(t.Context(), core.Query{})
	if err == nil || !strings.Contains(err.Error(), "Bad request.") {
		t.Fatalf("expected error message surfaced, got %v", err)
	}
}

func TestCheckAuthenticated(t *testing.T) {
	var gotPath string
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, map[string]string{"name": "alice@example.com"})
	})
	if err := b.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rest/whoami" {
		t.Errorf("expected /rest/whoami, got %s", gotPath)
	}
}

func TestCheckAuthenticatedFallsBackWhenWhoamiIsMissing(t *testing.T) {
	// Some real installs (bugzilla.suse.com) don't expose /rest/whoami at
	// all, even with a valid key: it 404s regardless. Check should fall
	// back to a plain connectivity check rather than reporting a false
	// authentication failure.
	var gotPaths []string
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		if r.URL.Path == "/rest/whoami" {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(t, w, map[string]any{"error": true, "message": "A REST API resource was not found for 'GET /whoami'.", "code": 32614})
			return
		}
		writeJSON(t, w, map[string]string{"version": "5.0.6"})
	})
	if err := b.Check(t.Context()); err != nil {
		t.Fatalf("expected Check to fall back instead of failing, got %v", err)
	}
	if len(gotPaths) != 2 || gotPaths[0] != "/rest/whoami" || gotPaths[1] != "/rest/version" {
		t.Errorf("expected whoami then a version fallback, got %v", gotPaths)
	}
}

func TestCheckAuthenticatedRealAuthFailureIsNotSwallowed(t *testing.T) {
	// A genuine auth failure (401, not a missing endpoint) must still be
	// reported, not silently papered over by the 404 fallback.
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if err := b.Check(t.Context()); err == nil {
		t.Fatal("expected a real 401 to still be reported as a failure")
	}
}

func TestCheckUnauthenticatedUsesVersion(t *testing.T) {
	var gotPath string
	b := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, map[string]string{"version": "5.0.6"})
	})
	b.client.apiKey = ""
	if err := b.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rest/version" {
		t.Errorf("expected /rest/version, got %s", gotPath)
	}
}

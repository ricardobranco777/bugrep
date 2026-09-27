// SPDX-License-Identifier: BSD-2-Clause

package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func newTestBackend(t *testing.T, flavor string, handler http.HandlerFunc) *Backend {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	apiBase := "/rest/api/2"
	if flavor == "cloud" {
		apiBase = "/rest/api/3"
	}
	return &Backend{
		name:    "jira-test",
		flavor:  flavor,
		apiBase: apiBase,
		client:  &client{http: srv.Client(), baseURL: srv.URL, authHeader: "Bearer test-token"},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func rawIssue(key, summary string) map[string]any {
	return map[string]any{
		"key": key,
		"fields": map[string]any{
			"summary":     summary,
			"status":      map[string]any{"name": "Open", "statusCategory": map[string]any{"key": "new"}},
			"assignee":    map[string]any{"displayName": "Bob"},
			"reporter":    map[string]any{"displayName": "Alice"},
			"labels":      []string{"bug"},
			"project":     map[string]any{"key": "PROJ"},
			"created":     "2026-01-01T00:00:00.000+0000",
			"updated":     "2026-02-01T00:00:00.000+0000",
			"priority":    map[string]any{"name": "High"},
			"description": "Plain text body",
		},
	}
}

func TestSearchCloudPagination(t *testing.T) {
	var requests int
	b := newTestBackend(t, "cloud", func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		if _, hasToken := body["nextPageToken"]; !hasToken {
			writeJSON(t, w, map[string]any{
				"issues":        []any{rawIssue("PROJ-1", "First")},
				"isLast":        false,
				"nextPageToken": "page2",
			})
		} else {
			writeJSON(t, w, map[string]any{
				"issues": []any{rawIssue("PROJ-2", "Second")},
				"isLast": true,
			})
		}
	})

	issues, err := b.Search(t.Context(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests (paginated via nextPageToken), got %d", requests)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[0].Ref() != "jira-test#PROJ-1" {
		t.Errorf("unexpected ref: %s", issues[0].Ref())
	}
	if issues[0].URL != b.client.baseURL+"/browse/PROJ-1" {
		t.Errorf("unexpected URL: %s", issues[0].URL)
	}
}

func TestSearchServerPagination(t *testing.T) {
	var requests int
	b := newTestBackend(t, "server", func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/rest/api/2/search" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		startAt := int(body["startAt"].(float64))

		if startAt == 0 {
			writeJSON(t, w, map[string]any{
				"total":  2,
				"issues": []any{rawIssue("PROJ-1", "First")},
			})
		} else {
			writeJSON(t, w, map[string]any{
				"total":  2,
				"issues": []any{rawIssue("PROJ-2", "Second")},
			})
		}
	})

	issues, err := b.Search(t.Context(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests (paginated via startAt/total), got %d", requests)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestSearchConvertsFields(t *testing.T) {
	b := newTestBackend(t, "server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"total":  1,
			"issues": []any{rawIssue("PROJ-1", "Something broke")},
		})
	})
	issues, err := b.Search(t.Context(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	got := issues[0]
	if got.Title != "Something broke" || got.Assignee != "Bob" || got.Reporter != "Alice" {
		t.Errorf("unexpected conversion: %+v", got)
	}
	if got.State != core.StateOpen {
		t.Errorf("expected open state (statusCategory=new), got %q", got.State)
	}
	if got.Priority != "High" {
		t.Errorf("unexpected priority: %q", got.Priority)
	}
}

func TestSearchDoneStatusCategoryIsClosed(t *testing.T) {
	b := newTestBackend(t, "server", func(w http.ResponseWriter, r *http.Request) {
		issue := rawIssue("PROJ-1", "Fixed")
		issue["fields"].(map[string]any)["status"] = map[string]any{"name": "Closed", "statusCategory": map[string]any{"key": "done"}}
		writeJSON(t, w, map[string]any{"total": 1, "issues": []any{issue}})
	})
	issues, err := b.Search(t.Context(), core.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if issues[0].State != core.StateClosed {
		t.Errorf("expected closed state, got %q", issues[0].State)
	}
}

func TestGetWithADFDescription(t *testing.T) {
	b := newTestBackend(t, "cloud", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/PROJ-1":
			issue := rawIssue("PROJ-1", "Something broke")
			issue["fields"].(map[string]any)["description"] = map[string]any{
				"type": "doc",
				"content": []any{
					map[string]any{"type": "paragraph", "content": []any{
						map[string]any{"type": "text", "text": "It's broken."},
					}},
				},
			}
			writeJSON(t, w, issue)
		case "/rest/api/3/issue/PROJ-1/comment":
			writeJSON(t, w, map[string]any{
				"total": 1,
				"comments": []any{
					map[string]any{"author": map[string]any{"displayName": "Bob"}, "body": "Confirmed.", "created": "2026-01-02T00:00:00.000+0000"},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	issue, err := b.Get(t.Context(), "PROJ-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Body != "It's broken." {
		t.Errorf("expected ADF description converted to text, got %q", issue.Body)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Author != "Bob" || issue.Comments[0].Body != "Confirmed." {
		t.Errorf("unexpected comments: %+v", issue.Comments)
	}
}

func TestGetInvalidAuth(t *testing.T) {
	b := newTestBackend(t, "server", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := b.Get(t.Context(), "PROJ-1", false); err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestJiraErrorMessageSurfaced(t *testing.T) {
	b := newTestBackend(t, "server", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(t, w, map[string]any{"errorMessages": []string{"The value 'BOGUS' does not exist for the field 'project'."}})
	})
	_, err := b.Search(t.Context(), core.Query{Project: "BOGUS"})
	if err == nil || !strings.Contains(err.Error(), "does not exist for the field 'project'") {
		t.Fatalf("expected jira error message surfaced, got %v", err)
	}
}

func TestCheckAuthenticated(t *testing.T) {
	var gotPath string
	b := newTestBackend(t, "server", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing/incorrect Authorization header: %q", r.Header.Get("Authorization"))
		}
		writeJSON(t, w, map[string]string{"name": "alice"})
	})
	if err := b.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rest/api/2/myself" {
		t.Errorf("expected /rest/api/2/myself, got %s", gotPath)
	}
}

func TestCheckUnauthenticatedUsesServerInfo(t *testing.T) {
	var gotPath string
	b := newTestBackend(t, "server", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, map[string]string{})
	})
	b.client.authHeader = ""
	if err := b.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rest/api/2/serverInfo" {
		t.Errorf("expected /rest/api/2/serverInfo, got %s", gotPath)
	}
}

func TestBuildAuthHeader(t *testing.T) {
	cases := []struct {
		name    string
		flavor  string
		user    string
		token   string
		want    string
		wantErr bool
	}{
		{name: "no token means unauthenticated", flavor: "cloud", token: "", want: ""},
		{name: "cloud requires user", flavor: "cloud", token: "tok", wantErr: true},
		{name: "cloud basic auth", flavor: "cloud", user: "me@example.com", token: "tok", want: basicAuth("me@example.com", "tok")},
		{name: "server bearer PAT without user", flavor: "server", token: "tok", want: "Bearer tok"},
		{name: "server basic auth with user", flavor: "server", user: "alice", token: "tok", want: basicAuth("alice", "tok")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildAuthHeader(c.flavor, c.user, c.token)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

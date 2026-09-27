// SPDX-License-Identifier: BSD-2-Clause

// Package core defines the types shared by every bugrep backend: the
// normalized Issue and Query shapes, the Backend interface each tracker
// implements, and small helpers used across the codebase.
package core

import (
	"context"
	"time"
)

// State is a normalized issue state, independent of any tracker's native
// status names.
type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
	StateAll    State = "all"
)

// SortField selects which timestamp or field results are ordered by.
type SortField string

const (
	SortUpdated  SortField = "updated"
	SortCreated  SortField = "created"
	SortPriority SortField = "priority"
)

// Query is a backend-agnostic search request. Each backend's translate.go
// turns a Query into that tracker's native search parameters.
type Query struct {
	Text      string
	State     State
	Assignee  string // "me" is resolved per tracker before Search is called
	Reporter  string
	Labels    []string
	Project   string
	Component string

	UpdatedSince time.Time
	CreatedSince time.Time

	Sort SortField
	Raw  string // native query passthrough; only valid with a single tracker
}

// Comment is a single comment on an issue, populated by Get when
// withComments is true.
type Comment struct {
	Author  string
	Created time.Time
	Body    string
}

// Issue is the normalized representation of a bug or issue, common to all
// backends. Fields that a backend doesn't support are left zero-valued.
type Issue struct {
	Tracker string // configured instance name, e.g. "rhbz"
	Type    string // backend type, e.g. "bugzilla"
	Key     string // tracker-native key, e.g. "2234567", "PROJ-123", "owner/repo#42"

	Title    string
	State    State  // normalized: open/closed
	Status   string // native status, e.g. "NEW", "In Progress"
	Priority string
	Severity string

	Assignee string
	Reporter string
	Labels   []string
	Project  string

	Created time.Time
	Updated time.Time

	URL string

	Body     string    // only populated by Get
	Comments []Comment // only populated by Get(withComments=true)

	// Extra carries backend-specific fields through to JSON output without
	// forcing every backend into the same normalized shape.
	Extra map[string]any `json:",omitempty"`
}

// Ref returns the "<tracker>#<key>" reference string for this issue, e.g.
// "boo#1282853" or "gh#owner/repo#42". The separator is "#" to match the
// "prefix#number" convention already familiar from Bugzilla/GitHub/GitLab
// shorthand (e.g. "boo#123", "gh#123"); ParseRef only splits on the first
// "#", so a key that itself contains "#" (as GitHub/GitLab/Gitea/Forgejo
// keys do, e.g. "owner/repo#42") still parses unambiguously.
func (i Issue) Ref() string {
	return i.Tracker + "#" + i.Key
}

// Caps describes which Query filters a backend can apply natively. The
// search engine uses this to warn the user when a filter will be ignored,
// rather than silently dropping it.
type Caps struct {
	Labels    bool
	Component bool
	Reporter  bool
}

// Backend is implemented once per tracker type (Bugzilla, Jira, Redmine,
// GitHub, GitLab). Each configured instance in the config file gets its own
// Backend value.
type Backend interface {
	// Name is the configured instance name, e.g. "rhbz" or "jira-work".
	Name() string
	// Type is the backend type, e.g. "bugzilla".
	Type() string

	Search(ctx context.Context, q Query) ([]Issue, error)
	Get(ctx context.Context, key string, withComments bool) (*Issue, error)

	// Check verifies connectivity and authentication, for `trackers test`.
	Check(ctx context.Context) error

	Capabilities() Caps
}

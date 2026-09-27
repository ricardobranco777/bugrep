// SPDX-License-Identifier: BSD-2-Clause

package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// fakeBackend is a minimal core.Backend for testing the search engine
// without hitting any real tracker.
type fakeBackend struct {
	name   string
	issues []core.Issue
	err    error
	delay  time.Duration
}

func (f *fakeBackend) Name() string { return f.name }
func (f *fakeBackend) Type() string { return "fake" }
func (f *fakeBackend) Search(ctx context.Context, q core.Query) ([]core.Issue, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.issues, nil
}
func (f *fakeBackend) Get(ctx context.Context, key string, withComments bool) (*core.Issue, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeBackend) Check(ctx context.Context) error { return nil }
func (f *fakeBackend) Capabilities() core.Caps         { return core.Caps{} }

func issueAt(tracker, key string, updated time.Time) core.Issue {
	return core.Issue{Tracker: tracker, Key: key, Updated: updated}
}

func TestRunMergesAndSorts(t *testing.T) {
	now := time.Now()
	b1 := &fakeBackend{name: "a", issues: []core.Issue{
		issueAt("a", "1", now.Add(-1*time.Hour)),
	}}
	b2 := &fakeBackend{name: "b", issues: []core.Issue{
		issueAt("b", "2", now),
		issueAt("b", "3", now.Add(-2*time.Hour)),
	}}

	res := Run(context.Background(), []core.Backend{b1, b2}, core.Query{Sort: core.SortUpdated}, time.Second)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Issues) != 3 {
		t.Fatalf("expected 3 issues, got %d", len(res.Issues))
	}
	// Most recently updated first.
	want := []string{"b#2", "a#1", "b#3"}
	for i, w := range want {
		if got := res.Issues[i].Ref(); got != w {
			t.Errorf("issue %d: got %s, want %s", i, got, w)
		}
	}
}

func TestRunPartialFailureKeepsOtherResults(t *testing.T) {
	good := &fakeBackend{name: "good", issues: []core.Issue{issueAt("good", "1", time.Now())}}
	bad := &fakeBackend{name: "bad", err: errors.New("boom")}

	res := Run(context.Background(), []core.Backend{good, bad}, core.Query{}, time.Second)
	if len(res.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(res.Issues))
	}
	if len(res.Errors) != 1 || res.Errors[0].Tracker != "bad" {
		t.Fatalf("expected 1 error from 'bad', got %v", res.Errors)
	}
}

func TestRunTimeoutPerBackend(t *testing.T) {
	slow := &fakeBackend{name: "slow", delay: 200 * time.Millisecond}
	res := Run(context.Background(), []core.Backend{slow}, core.Query{}, 20*time.Millisecond)
	if len(res.Errors) != 1 {
		t.Fatalf("expected timeout error, got issues=%v errors=%v", res.Issues, res.Errors)
	}
}

func TestSortByPriority(t *testing.T) {
	issues := []core.Issue{
		{Key: "1", Priority: "P2"},
		{Key: "2", Priority: "P1"},
	}
	sortIssues(issues, core.SortPriority)
	if issues[0].Key != "2" {
		t.Errorf("expected P1 issue first, got %+v", issues)
	}
}

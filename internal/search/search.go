// SPDX-License-Identifier: BSD-2-Clause

// Package search runs a Query against several backends concurrently and
// merges the results into one sorted list.
package search

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// TrackerError records a single tracker's failure. Search returns these
// alongside any successful results, so one failing tracker doesn't hide the
// others' results.
type TrackerError struct {
	Tracker string
	Err     error
}

func (e TrackerError) Error() string {
	return fmt.Sprintf("%s: %v", e.Tracker, e.Err)
}

// Result is the outcome of a federated search: the merged, sorted issues
// plus any per-tracker errors.
type Result struct {
	Issues []core.Issue
	Errors []TrackerError
}

// Run queries every backend in backends concurrently, each bounded by
// timeout, and merges the results sorted by q.Sort. A failing backend is
// recorded in Result.Errors rather than aborting the whole search.
func Run(ctx context.Context, backends []core.Backend, q core.Query, timeout time.Duration) Result {
	type outcome struct {
		issues []core.Issue
		err    *TrackerError
	}

	outcomes := make([]outcome, len(backends))
	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func(i int, b core.Backend) {
			defer wg.Done()
			bctx := ctx
			var cancel context.CancelFunc
			if timeout > 0 {
				bctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			issues, err := b.Search(bctx, q)
			if err != nil {
				outcomes[i] = outcome{err: &TrackerError{Tracker: b.Name(), Err: err}}
				return
			}
			outcomes[i] = outcome{issues: issues}
		}(i, b)
	}
	wg.Wait()

	var res Result
	for _, o := range outcomes {
		if o.err != nil {
			res.Errors = append(res.Errors, *o.err)
			continue
		}
		res.Issues = append(res.Issues, o.issues...)
	}

	sortIssues(res.Issues, q.Sort)
	return res
}

func sortIssues(issues []core.Issue, field core.SortField) {
	switch field {
	case core.SortCreated:
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Created.After(issues[j].Created) })
	case core.SortPriority:
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Priority < issues[j].Priority })
	case core.SortUpdated, "":
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Updated.After(issues[j].Updated) })
	}
}

// SPDX-License-Identifier: BSD-2-Clause

package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ricardobranco777/bugrep/internal/backend"
	"github.com/ricardobranco777/bugrep/internal/config"
	"github.com/ricardobranco777/bugrep/internal/core"
	"github.com/ricardobranco777/bugrep/internal/httpx"
	"github.com/ricardobranco777/bugrep/internal/output"
	"github.com/ricardobranco777/bugrep/internal/search"
)

type searchFlags struct {
	trackers     []string
	types        []string
	state        string
	assignee     string
	reporter     string
	labels       []string
	project      string
	component    string
	since        string
	createdSince string
	sortBy       string
	raw          string
}

func newSearchCmd() *cobra.Command {
	var f searchFlags

	cmd := &cobra.Command{
		Use:   "search [text...]",
		Short: "Search issues across configured trackers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, strings.Join(args, " "), f)
		},
	}

	fs := cmd.Flags()
	fs.StringSliceVarP(&f.trackers, "tracker", "t", nil, "limit to these tracker instances (repeatable; default: default_trackers, or all)")
	fs.StringSliceVar(&f.types, "type", nil, "limit to these backend types, e.g. bugzilla,jira")
	fs.StringVarP(&f.state, "state", "s", "open", "issue state: open, closed, all")
	fs.StringVarP(&f.assignee, "assignee", "a", "", `assignee username, or "me"`)
	fs.StringVarP(&f.reporter, "reporter", "r", "", `reporter username, or "me"`)
	fs.StringSliceVarP(&f.labels, "label", "l", nil, "label/keyword/tag (repeatable)")
	fs.StringVarP(&f.project, "project", "p", "", "Jira project, Bugzilla product, Redmine project, or a repo within a GitHub/GitLab tracker's scope")
	fs.StringVar(&f.component, "component", "", "Bugzilla component / Jira component")
	fs.StringVar(&f.since, "since", "", "updated since: YYYY-MM-DD or a duration like 2w, 3d, 12h")
	fs.StringVar(&f.createdSince, "created-since", "", "created since: YYYY-MM-DD or a duration like 2w, 3d, 12h")
	fs.StringVar(&f.sortBy, "sort", "updated", "sort by: updated, created, priority")
	fs.StringVar(&f.raw, "raw", "", "native query passthrough; requires exactly one --tracker")

	return cmd
}

func runSearch(cmd *cobra.Command, text string, f searchFlags) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	names, err := selectTrackers(cfg, f.trackers, f.types)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("no trackers configured (see `bugrep config init`)")
	}

	if f.raw != "" && len(names) != 1 {
		return fmt.Errorf("--raw requires exactly one --tracker, got %d", len(names))
	}

	q, err := buildQuery(text, f)
	if err != nil {
		return err
	}

	format, outOpts, err := outputOptions()
	if err != nil {
		return err
	}

	httpOpts := httpx.DefaultOptions()
	httpOpts.Timeout = g.timeout
	httpOpts.Debug = g.debug

	ctx := context.Background()
	var backends []core.Backend
	var buildErrs []search.TrackerError
	for _, name := range names {
		tc := cfg.Trackers[name]
		b, err := backend.New(ctx, name, tc, httpOpts)
		if err != nil {
			buildErrs = append(buildErrs, search.TrackerError{Tracker: name, Err: err})
			continue
		}
		backends = append(backends, b)
	}

	result := search.Run(ctx, backends, q, g.timeout)
	result.Errors = append(buildErrs, result.Errors...)

	if err := output.Render(cmd.OutOrStdout(), result.Issues, format, outOpts); err != nil {
		return err
	}

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			fmt.Fprintln(os.Stderr, "bugrep: warning:", e)
		}
		if len(result.Errors) >= len(names) {
			return fmt.Errorf("all %d tracker(s) failed", len(names))
		}
		return withExitCode(fmt.Errorf("%d of %d tracker(s) failed", len(result.Errors), len(names)), 2)
	}
	return nil
}

// selectTrackers resolves the --tracker/--type flags (or config defaults)
// into a sorted list of configured instance names.
func selectTrackers(cfg *config.Config, trackers, types []string) ([]string, error) {
	var names []string
	switch {
	case len(trackers) > 0:
		for _, name := range trackers {
			if _, ok := cfg.Trackers[name]; !ok {
				return nil, fmt.Errorf("unknown tracker %q (see `bugrep trackers list`)", name)
			}
			names = append(names, name)
		}
	case len(cfg.DefaultTrackers) > 0:
		names = append(names, cfg.DefaultTrackers...)
	default:
		for name := range cfg.Trackers {
			names = append(names, name)
		}
		sort.Strings(names)
	}

	if len(types) == 0 {
		return names, nil
	}
	wantType := make(map[string]bool, len(types))
	for _, t := range types {
		wantType[t] = true
	}
	var filtered []string
	for _, name := range names {
		if wantType[cfg.Trackers[name].Type] {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}

func buildQuery(text string, f searchFlags) (core.Query, error) {
	state, err := parseState(f.state)
	if err != nil {
		return core.Query{}, err
	}

	sortField, err := parseSortField(f.sortBy)
	if err != nil {
		return core.Query{}, err
	}

	updatedSince, err := core.ParseSince(f.since)
	if err != nil {
		return core.Query{}, fmt.Errorf("--since: %w", err)
	}
	createdSince, err := core.ParseSince(f.createdSince)
	if err != nil {
		return core.Query{}, fmt.Errorf("--created-since: %w", err)
	}

	return core.Query{
		Text:         text,
		State:        state,
		Assignee:     f.assignee,
		Reporter:     f.reporter,
		Labels:       f.labels,
		Project:      f.project,
		Component:    f.component,
		UpdatedSince: updatedSince,
		CreatedSince: createdSince,
		Sort:         sortField,
		Raw:          f.raw,
	}, nil
}

func parseState(s string) (core.State, error) {
	switch core.State(s) {
	case core.StateOpen, core.StateClosed, core.StateAll:
		return core.State(s), nil
	default:
		return "", fmt.Errorf("invalid --state %q (want open, closed or all)", s)
	}
}

func parseSortField(s string) (core.SortField, error) {
	switch core.SortField(s) {
	case core.SortUpdated, core.SortCreated, core.SortPriority:
		return core.SortField(s), nil
	default:
		return "", fmt.Errorf("invalid --sort %q (want updated, created or priority)", s)
	}
}

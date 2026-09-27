// SPDX-License-Identifier: BSD-2-Clause

package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ricardobranco777/bugrep/internal/backend"
	"github.com/ricardobranco777/bugrep/internal/core"
	"github.com/ricardobranco777/bugrep/internal/httpx"
	"github.com/ricardobranco777/bugrep/internal/output"
)

func newShowCmd() *cobra.Command {
	var withComments bool

	cmd := &cobra.Command{
		Use:   "show <ref>",
		Short: "Show a single issue's details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			issue, err := fetchIssue(args[0], withComments)
			if err != nil {
				return err
			}

			format, outOpts, err := outputOptions()
			if err != nil {
				return err
			}
			if format != output.FormatTable {
				return output.Render(cmd.OutOrStdout(), []core.Issue{*issue}, format, outOpts)
			}
			printIssueDetail(cmd, issue)
			return nil
		},
	}
	cmd.Flags().BoolVar(&withComments, "comments", false, "also fetch and print comments")
	return cmd
}

func fetchIssue(refStr string, withComments bool) (*core.Issue, error) {
	ref, err := core.ParseRef(refStr)
	if err != nil {
		return nil, err
	}

	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	tc, ok := cfg.Trackers[ref.Tracker]
	if !ok {
		return nil, fmt.Errorf("unknown tracker %q (see `bugrep trackers list`)", ref.Tracker)
	}

	httpOpts := httpx.DefaultOptions()
	httpOpts.Timeout = g.timeout
	httpOpts.Debug = g.debug

	ctx := context.Background()
	b, err := backend.New(ctx, ref.Tracker, tc, httpOpts)
	if err != nil {
		return nil, err
	}
	return b.Get(ctx, ref.Key, withComments)
}

func printIssueDetail(cmd *cobra.Command, issue *core.Issue) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s  %s\n", issue.Ref(), issue.Title)
	fmt.Fprintf(out, "State:    %s (%s)\n", issue.State, issue.Status)
	if issue.Priority != "" {
		fmt.Fprintf(out, "Priority: %s\n", issue.Priority)
	}
	if issue.Severity != "" {
		fmt.Fprintf(out, "Severity: %s\n", issue.Severity)
	}
	fmt.Fprintf(out, "Assignee: %s\n", issue.Assignee)
	fmt.Fprintf(out, "Reporter: %s\n", issue.Reporter)
	if issue.Project != "" {
		fmt.Fprintf(out, "Project:  %s\n", issue.Project)
	}
	if len(issue.Labels) > 0 {
		fmt.Fprintf(out, "Labels:   %v\n", issue.Labels)
	}
	fmt.Fprintf(out, "Created:  %s\n", issue.Created.Format("2006-01-02 15:04"))
	fmt.Fprintf(out, "Updated:  %s\n", issue.Updated.Format("2006-01-02 15:04"))
	fmt.Fprintf(out, "URL:      %s\n", issue.URL)
	if issue.Body != "" {
		fmt.Fprintf(out, "\n%s\n", issue.Body)
	}
	for _, c := range issue.Comments {
		fmt.Fprintf(out, "\n--- %s (%s) ---\n%s\n", c.Author, c.Created.Format("2006-01-02 15:04"), c.Body)
	}
}

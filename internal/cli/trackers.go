// SPDX-License-Identifier: BSD-2-Clause

package cli

import (
	"context"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ricardobranco777/bugrep/internal/backend"
	"github.com/ricardobranco777/bugrep/internal/httpx"
)

func newTrackersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trackers",
		Short: "Manage configured tracker instances",
	}
	cmd.AddCommand(newTrackersListCmd(), newTrackersTestCmd())
	return cmd
}

func newTrackersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured tracker instances",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			names := make([]string, 0, len(cfg.Trackers))
			for name := range cfg.Trackers {
				names = append(names, name)
			}
			sort.Strings(names)

			isDefault := make(map[string]bool, len(cfg.DefaultTrackers))
			for _, n := range cfg.DefaultTrackers {
				isDefault[n] = true
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tTYPE\tURL\tDEFAULT")
			for _, name := range names {
				tc := cfg.Trackers[name]
				fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", name, tc.Type, tc.URL, isDefault[name])
			}
			return tw.Flush()
		},
	}
}

func newTrackersTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [name...]",
		Short: "Check connectivity and authentication for one or more trackers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			names := args
			if len(names) == 0 {
				for name := range cfg.Trackers {
					names = append(names, name)
				}
				sort.Strings(names)
			}

			httpOpts := httpx.DefaultOptions()
			httpOpts.Timeout = g.timeout
			httpOpts.Debug = g.debug

			ctx := context.Background()
			failed := 0
			for _, name := range names {
				tc, ok := cfg.Trackers[name]
				if !ok {
					fmt.Fprintf(cmd.OutOrStdout(), "%-20s UNKNOWN (not configured)\n", name)
					failed++
					continue
				}
				b, err := backend.New(ctx, name, tc, httpOpts)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%-20s FAIL: %v\n", name, err)
					failed++
					continue
				}
				if err := b.Check(ctx); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%-20s FAIL: %v\n", name, err)
					failed++
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s OK\n", name)
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d tracker(s) failed", failed, len(names))
			}
			return nil
		},
	}
}

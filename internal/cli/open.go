// SPDX-License-Identifier: BSD-2-Clause

package cli

import (
	"fmt"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <ref>",
		Short: "Open an issue in the default browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			issue, err := fetchIssue(args[0], false)
			if err != nil {
				return err
			}
			if issue.URL == "" {
				return fmt.Errorf("%s has no URL", issue.Ref())
			}
			return browser.OpenURL(issue.URL)
		},
	}
}

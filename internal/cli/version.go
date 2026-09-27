// SPDX-License-Identifier: BSD-2-Clause

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set via -ldflags "-X github.com/ricardobranco777/bugrep/internal/cli.Version=..."
// at release build time; it defaults to "dev" for local builds.
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the bugrep version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "bugrep", Version)
			return nil
		},
	}
}

// SPDX-License-Identifier: BSD-2-Clause

package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is set via -ldflags "-X github.com/ricardobranco777/bugrep/internal/cli.Version=..."
// at release build time. Otherwise the module version from the build info is
// used (set by `go install`), falling back to "dev" for local builds.
var Version = "dev"

func version() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the bugrep version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "bugrep", version())
			return nil
		},
	}
}

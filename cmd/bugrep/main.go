// SPDX-License-Identifier: BSD-2-Clause

// Command bugrep searches Bugzilla, Jira, Redmine, GitHub and GitLab issues
// through one CLI.
package main

import (
	"os"

	"github.com/ricardobranco777/bugrep/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}

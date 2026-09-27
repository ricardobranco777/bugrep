// SPDX-License-Identifier: BSD-2-Clause

package output

import (
	"io"
	"text/tabwriter"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func renderTable(w io.Writer, issues []core.Issue, cols []string, _ Options) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	for i, c := range cols {
		if i > 0 {
			tw.Write([]byte("\t"))
		}
		tw.Write([]byte(header(c)))
	}
	tw.Write([]byte("\n"))

	for _, issue := range issues {
		for i, c := range cols {
			if i > 0 {
				tw.Write([]byte("\t"))
			}
			tw.Write([]byte(column(issue, c)))
		}
		tw.Write([]byte("\n"))
	}

	return tw.Flush()
}

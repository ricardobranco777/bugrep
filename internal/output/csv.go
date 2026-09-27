// SPDX-License-Identifier: BSD-2-Clause

package output

import (
	"encoding/csv"
	"io"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func renderCSV(w io.Writer, issues []core.Issue, cols []string) error {
	cw := csv.NewWriter(w)

	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = header(c)
	}
	if err := cw.Write(headers); err != nil {
		return err
	}

	for _, issue := range issues {
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = column(issue, c)
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
}

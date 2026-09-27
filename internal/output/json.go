// SPDX-License-Identifier: BSD-2-Clause

package output

import (
	"encoding/json"
	"io"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// renderJSON writes issues as a single JSON array, or as JSON Lines (one
// object per line) when lines is true.
func renderJSON(w io.Writer, issues []core.Issue, lines bool) error {
	if lines {
		enc := json.NewEncoder(w)
		for _, issue := range issues {
			if err := enc.Encode(issue); err != nil {
				return err
			}
		}
		return nil
	}

	if issues == nil {
		issues = []core.Issue{} // encode as [] rather than JSON null
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(issues)
}

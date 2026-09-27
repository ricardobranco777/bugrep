// SPDX-License-Identifier: BSD-2-Clause

package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// renderMarkdown writes a GitHub-flavored Markdown table. The "ref" column,
// if present, is rendered as a link to the issue's URL.
func renderMarkdown(w io.Writer, issues []core.Issue, cols []string) error {
	var b strings.Builder

	b.WriteString("|")
	for _, c := range cols {
		fmt.Fprintf(&b, " %s |", header(c))
	}
	b.WriteString("\n|")
	for range cols {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")

	for _, issue := range issues {
		b.WriteString("|")
		for _, c := range cols {
			val := escapeMarkdown(column(issue, c))
			if c == "ref" && issue.URL != "" {
				val = fmt.Sprintf("[%s](%s)", val, issue.URL)
			}
			fmt.Fprintf(&b, " %s |", val)
		}
		b.WriteString("\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

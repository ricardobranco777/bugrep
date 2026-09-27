// SPDX-License-Identifier: BSD-2-Clause

// Package output renders a slice of core.Issue as a table, JSON, JSON
// Lines, CSV or Markdown.
package output

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

// Format is an output format name, as accepted by -o/--output.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatCSV   Format = "csv"
	FormatMD    Format = "md"
)

// ParseFormat validates a -o/--output value.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatJSON, FormatJSONL, FormatCSV, FormatMD:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unknown output format %q (want table, json, jsonl, csv or md)", s)
	}
}

// DefaultColumns is the column set used when --columns isn't given.
var DefaultColumns = []string{"ref", "state", "priority", "updated", "assignee", "title"}

// AllColumns lists every column name accepted by --columns, in the order
// column() understands them.
var AllColumns = []string{
	"ref", "tracker", "type", "key", "title", "state", "status",
	"priority", "severity", "assignee", "reporter", "labels", "project",
	"created", "updated", "url",
}

// Options controls rendering that isn't determined by the format alone.
type Options struct {
	Columns []string // column names; DefaultColumns if empty
	NoColor bool     // disable table color even on a TTY
}

// Render writes issues to w in the given format.
func Render(w io.Writer, issues []core.Issue, format Format, opts Options) error {
	cols := opts.Columns
	if len(cols) == 0 {
		cols = DefaultColumns
	}
	for _, c := range cols {
		if !validColumn(c) {
			return fmt.Errorf("unknown column %q (want one of: %s)", c, strings.Join(AllColumns, ", "))
		}
	}

	switch format {
	case FormatTable:
		return renderTable(w, issues, cols, opts)
	case FormatJSON:
		return renderJSON(w, issues, false)
	case FormatJSONL:
		return renderJSON(w, issues, true)
	case FormatCSV:
		return renderCSV(w, issues, cols)
	case FormatMD:
		return renderMarkdown(w, issues, cols)
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
}

func validColumn(name string) bool {
	return slices.Contains(AllColumns, name)
}

// column returns the display value of column name for issue i.
func column(i core.Issue, name string) string {
	switch name {
	case "ref":
		return i.Ref()
	case "tracker":
		return i.Tracker
	case "type":
		return i.Type
	case "key":
		return i.Key
	case "title":
		return i.Title
	case "state":
		return string(i.State)
	case "status":
		return i.Status
	case "priority":
		return i.Priority
	case "severity":
		return i.Severity
	case "assignee":
		return i.Assignee
	case "reporter":
		return i.Reporter
	case "labels":
		return strings.Join(i.Labels, ",")
	case "project":
		return i.Project
	case "created":
		return formatTime(i.Created)
	case "updated":
		return formatTime(i.Updated)
	case "url":
		return i.URL
	default:
		return ""
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func header(name string) string {
	return strings.ToUpper(name)
}

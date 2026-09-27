// SPDX-License-Identifier: BSD-2-Clause

package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ricardobranco777/bugrep/internal/core"
)

func sampleIssues() []core.Issue {
	return []core.Issue{
		{
			Tracker: "gh", Type: "github", Key: "owner/repo#42",
			Title: "Something broke", State: core.StateOpen, Status: "open",
			Priority: "P1", Assignee: "alice", Labels: []string{"bug", "urgent"},
			Updated: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			URL:     "https://github.com/owner/repo/issues/42",
		},
		{
			Tracker: "rhbz", Type: "bugzilla", Key: "2234567",
			Title: "Crash | pipe in title", State: core.StateClosed, Status: "CLOSED",
			Priority: "high", Assignee: "bob",
			Updated: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
			URL:     "https://bugzilla.redhat.com/show_bug.cgi?id=2234567",
		},
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleIssues(), FormatTable, Options{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"REF", "gh#owner/repo#42", "rhbz#2234567", "Something broke"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleIssues(), FormatJSON, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"owner/repo#42"`) {
		t.Errorf("json output missing issue key:\n%s", buf.String())
	}
}

func TestRenderJSONEmptyIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, nil, FormatJSON, Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("expected empty JSON array, got %q", buf.String())
	}
}

func TestRenderCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleIssues(), FormatCSV, Options{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 { // header + 2 issues
		t.Errorf("expected 3 CSV lines, got %d:\n%s", len(lines), buf.String())
	}
}

func TestRenderMarkdownEscapesPipes(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleIssues(), FormatMD, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `Crash \| pipe in title`) {
		t.Errorf("expected escaped pipe in markdown output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "[gh#owner/repo#42](https://github.com/owner/repo/issues/42)") {
		t.Errorf("expected ref column linked to URL:\n%s", buf.String())
	}
}

func TestRenderUnknownColumn(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, sampleIssues(), FormatTable, Options{Columns: []string{"bogus"}})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
}

func TestParseFormat(t *testing.T) {
	if _, err := ParseFormat("bogus"); err == nil {
		t.Fatal("expected error for unknown format")
	}
	for _, f := range []string{"table", "json", "jsonl", "csv", "md"} {
		if _, err := ParseFormat(f); err != nil {
			t.Errorf("ParseFormat(%q): %v", f, err)
		}
	}
}

// SPDX-License-Identifier: BSD-2-Clause

package jira

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRichTextPlainString(t *testing.T) {
	var r richText
	if err := json.Unmarshal([]byte(`"hello world"`), &r); err != nil {
		t.Fatal(err)
	}
	if r != "hello world" {
		t.Errorf("got %q", r)
	}
}

func TestRichTextNull(t *testing.T) {
	var r richText
	if err := json.Unmarshal([]byte(`null`), &r); err != nil {
		t.Fatal(err)
	}
	if r != "" {
		t.Errorf("got %q, want empty", r)
	}
}

func TestRichTextADF(t *testing.T) {
	doc := `{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [{"type": "text", "text": "First paragraph."}]},
			{"type": "paragraph", "content": [{"type": "text", "text": "Second "}, {"type": "text", "text": "paragraph."}]}
		]
	}`
	var r richText
	if err := json.Unmarshal([]byte(doc), &r); err != nil {
		t.Fatal(err)
	}
	want := "First paragraph.\nSecond paragraph."
	if string(r) != want {
		t.Errorf("got %q, want %q", r, want)
	}
}

func TestJiraTimeUnmarshal(t *testing.T) {
	var jt jiraTime
	if err := json.Unmarshal([]byte(`"2026-01-15T10:23:45.000+0000"`), &jt); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 15, 10, 23, 45, 0, time.UTC)
	if !jt.Time().Equal(want) {
		t.Errorf("got %v, want %v", jt.Time(), want)
	}
}

func TestJiraTimeUnmarshalWithOffset(t *testing.T) {
	var jt jiraTime
	if err := json.Unmarshal([]byte(`"2026-01-15T10:23:45.000-0500"`), &jt); err != nil {
		t.Fatal(err)
	}
	if jt.Time().UTC().Hour() != 15 {
		t.Errorf("got hour %d, want 15 (10:23 -0500 = 15:23 UTC)", jt.Time().UTC().Hour())
	}
}

func TestJiraTimeUnmarshalEmpty(t *testing.T) {
	var jt jiraTime
	if err := json.Unmarshal([]byte(`""`), &jt); err != nil {
		t.Fatal(err)
	}
	if !jt.Time().IsZero() {
		t.Errorf("expected zero time, got %v", jt.Time())
	}
}

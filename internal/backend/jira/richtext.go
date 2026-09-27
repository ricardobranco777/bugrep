// SPDX-License-Identifier: BSD-2-Clause

package jira

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// richText holds a plain-text rendering of a Jira text field. Jira Server
// (API v2) sends these fields as plain strings; Jira Cloud (API v3) sends
// them as Atlassian Document Format (ADF), a JSON document tree. UnmarshalJSON
// accepts either shape and always produces plain text, since bugrep only
// ever displays issue bodies and comments as text.
type richText string

func (r *richText) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*r = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*r = richText(s)
		return nil
	}

	var doc adfNode
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	*r = richText(adfToText(doc))
	return nil
}

// adfNode is the small subset of Atlassian Document Format bugrep
// understands: enough to extract readable plain text, not to reproduce
// formatting, links, or embedded media.
type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
}

func adfToText(n adfNode) string {
	var b strings.Builder
	var walk func(adfNode)
	walk = func(n adfNode) {
		if n.Type == "text" {
			b.WriteString(n.Text)
		}
		for _, c := range n.Content {
			walk(c)
		}
		switch n.Type {
		case "paragraph", "heading", "codeBlock", "listItem":
			b.WriteString("\n")
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// jiraTimeLayout is the timestamp format Jira uses in its JSON responses,
// e.g. "2026-01-15T10:23:45.000+0000" — note the zone offset has no colon,
// so it isn't RFC3339 and won't unmarshal into time.Time directly.
const jiraTimeLayout = "2006-01-02T15:04:05.000-0700"

// jiraTime unmarshals Jira's timestamp format into a time.Time.
type jiraTime time.Time

func (t *jiraTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*t = jiraTime(time.Time{})
		return nil
	}
	parsed, err := time.Parse(jiraTimeLayout, s)
	if err != nil {
		return err
	}
	*t = jiraTime(parsed)
	return nil
}

func (t jiraTime) Time() time.Time { return time.Time(t) }

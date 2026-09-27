// SPDX-License-Identifier: BSD-2-Clause

package core

import "testing"

func TestParseRef(t *testing.T) {
	cases := []struct {
		in          string
		wantTracker string
		wantKey     string
		wantErr     bool
	}{
		{"boo#2234567", "boo", "2234567", false},
		{"jsc#PROJ-123", "jsc", "PROJ-123", false},
		{"gh#owner/repo#42", "gh", "owner/repo#42", false},
		{"gl#group/proj#17", "gl", "group/proj#17", false},
		{"noHash", "", "", true},
		{"#nokey", "", "", true},
		{"notracker#", "", "", true},
		{"", "", "", true},
	}
	for _, c := range cases {
		ref, err := ParseRef(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseRef(%q): expected error, got %+v", c.in, ref)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRef(%q): unexpected error: %v", c.in, err)
			continue
		}
		if ref.Tracker != c.wantTracker || ref.Key != c.wantKey {
			t.Errorf("ParseRef(%q) = %+v, want {%q %q}", c.in, ref, c.wantTracker, c.wantKey)
		}
		if ref.String() != c.in {
			t.Errorf("Ref.String() = %q, want %q", ref.String(), c.in)
		}
	}
}

// SPDX-License-Identifier: BSD-2-Clause

package core

import (
	"testing"
	"time"
)

func TestParseSinceEmpty(t *testing.T) {
	got, err := ParseSince("")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Errorf("expected zero time for empty input, got %v", got)
	}
}

func TestParseSinceDate(t *testing.T) {
	got, err := ParseSince("2026-01-15")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseSinceDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"3d", 3 * 24 * time.Hour},
		{"2w", 2 * 7 * 24 * time.Hour},
		{"12h", 12 * time.Hour},
		{"1.5d", 36 * time.Hour},
	}
	for _, c := range cases {
		before := time.Now().Add(-c.want)
		got, err := ParseSince(c.in)
		if err != nil {
			t.Errorf("ParseSince(%q): %v", c.in, err)
			continue
		}
		diff := got.Sub(before)
		if diff < -time.Second || diff > time.Second {
			t.Errorf("ParseSince(%q) = %v, want approximately %v (diff %v)", c.in, got, before, diff)
		}
	}
}

func TestParseSinceInvalid(t *testing.T) {
	if _, err := ParseSince("not-a-date"); err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestParseSinceDurationIsUTC(t *testing.T) {
	// Some backends (Launchpad) reject a timestamp with a non-UTC offset
	// outright, so a duration-based "since" must resolve to UTC regardless
	// of the host's local time zone.
	got, err := ParseSince("3d")
	if err != nil {
		t.Fatal(err)
	}
	if got.Location() != time.UTC {
		t.Errorf("expected a UTC location, got %v", got.Location())
	}
}

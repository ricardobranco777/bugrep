// SPDX-License-Identifier: BSD-2-Clause

package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSince parses a --since/--created-since value: either an absolute
// date (YYYY-MM-DD) or a duration ago, given as a number followed by a unit
// (h, d, w for hours/days/weeks — the units time.ParseDuration doesn't
// support natively — or any unit time.ParseDuration accepts).
func ParseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}

	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	if d, err := parseDuration(s); err == nil {
		// UTC, not local time: some backends (Launchpad) reject a
		// non-UTC offset outright, and it keeps every backend's request
		// consistent regardless of bugrep's own time zone.
		return time.Now().UTC().Add(-d), nil
	}

	return time.Time{}, fmt.Errorf("invalid date/duration %q: want YYYY-MM-DD or a duration like 2w, 3d, 12h", s)
}

// parseDuration extends time.ParseDuration with day ("d") and week ("w")
// units, since those are the common way to phrase "since" filters.
func parseDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	unit := s[len(s)-1]
	var mult time.Duration
	switch unit {
	case 'd':
		mult = 24 * time.Hour
	case 'w':
		mult = 7 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("unrecognized duration %q", s)
	}

	n, err := strconv.ParseFloat(strings.TrimSuffix(s, string(unit)), 64)
	if err != nil {
		return 0, fmt.Errorf("unrecognized duration %q", s)
	}
	return time.Duration(n * float64(mult)), nil
}

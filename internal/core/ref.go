// SPDX-License-Identifier: BSD-2-Clause

package core

import (
	"fmt"
	"strings"
)

// Ref identifies a single issue: the configured tracker instance and the
// tracker-native key, e.g. "boo#1282853" or "gh#owner/repo#42".
type Ref struct {
	Tracker string
	Key     string
}

// String returns the "<tracker>#<key>" form.
func (r Ref) String() string {
	return r.Tracker + "#" + r.Key
}

// ParseRef parses a "<tracker>#<key>" reference. The key itself may contain
// further "#"s (GitHub/GitLab/Gitea/Forgejo keys do, e.g. "owner/repo#42"),
// so only the first "#" is significant — the rest belongs to the key
// unchanged, and backends that need to split it further (SplitProjectKey)
// do so from the end.
func ParseRef(s string) (Ref, error) {
	tracker, key, ok := strings.Cut(s, "#")
	if !ok || tracker == "" || key == "" {
		return Ref{}, fmt.Errorf("invalid issue reference %q: expected <tracker>#<key>", s)
	}
	return Ref{Tracker: tracker, Key: key}, nil
}

// SplitProjectKey splits a "<project>#<number>" style key — the format used
// by both GitHub ("owner/repo#42") and GitLab ("group/project#17") issue
// keys — into the project path and the numeric id, at the last '#'.
func SplitProjectKey(key string) (project, number string, err error) {
	project, number, ok := strings.CutLast(key, "#")
	if !ok {
		return "", "", fmt.Errorf("invalid issue key %q: want <project>#<number>", key)
	}
	return project, number, nil
}

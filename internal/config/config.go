// SPDX-License-Identifier: BSD-2-Clause

// Package config loads and validates bugrep's TOML configuration file, and
// resolves tracker credentials from the environment, a command, or an OS
// keyring.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/zalando/go-keyring"
)

// keyringGet is a package variable (rather than a direct call to
// keyring.Get) so tests can substitute a fake instead of touching a real OS
// keyring.
var keyringGet = keyring.Get

// TrackerConfig is one [trackers.<name>] section. Not every field applies to
// every backend type; unused fields are simply left zero-valued.
type TrackerConfig struct {
	Type string `toml:"type"`
	URL  string `toml:"url"`

	// Jira only: "cloud" or "server" (Data Center counts as "server").
	Flavor string `toml:"flavor"`

	// Credential resolution, checked in order: *_env, *_cmd, *_keyring, then
	// the literal fields below. See ResolveCredential.
	APIKeyEnv     string   `toml:"api_key_env"`
	APIKeyCmd     []string `toml:"api_key_cmd"`
	APIKeyKeyring string   `toml:"api_key_keyring"`
	APIKey        string   `toml:"api_key"`

	TokenEnv     string   `toml:"token_env"`
	TokenCmd     []string `toml:"token_cmd"`
	TokenKeyring string   `toml:"token_keyring"`
	Token        string   `toml:"token"`

	User string `toml:"user"` // Jira Cloud email / Jira Server username

	// GitHub/Gitea/Forgejo scope: at least one of these is required.
	Orgs  []string `toml:"orgs"`
	Repos []string `toml:"repos"`

	// GitLab scope: at least one of these is required.
	Groups   []string `toml:"groups"`
	Projects []string `toml:"projects"`

	// Launchpad scope: at least one is required. Each entry is a path
	// fragment identifying a distribution ("ubuntu"), a distribution source
	// package ("ubuntu/+source/linux"), or a project ("inkscape").
	Targets []string `toml:"targets"`

	// Per-instance default filters, e.g. {product = "Fedora"}.
	Defaults map[string]string `toml:"defaults"`
}

// Defaults holds config-wide defaults applied before per-instance and
// command-line overrides.
type Defaults struct {
	State string `toml:"state"`
}

// Config is the parsed and validated contents of config.toml.
type Config struct {
	DefaultTrackers []string                 `toml:"default_trackers"`
	Defaults        Defaults                 `toml:"defaults"`
	Trackers        map[string]TrackerConfig `toml:"trackers"`
}

// DefaultPath returns the config file location: $BUGREP_CONFIG if set,
// otherwise $XDG_CONFIG_HOME/bugrep/config.toml (with the usual
// $HOME/.config fallback).
func DefaultPath() (string, error) {
	if p := os.Getenv("BUGREP_CONFIG"); p != "" {
		return p, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determining config path: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "bugrep", "config.toml"), nil
}

// Load reads and validates the config file at path. Unknown keys in the file
// are treated as errors, so typos are caught early instead of silently
// ignored.
func Load(path string) (*Config, error) {
	var cfg Config
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("loading config %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("loading config %s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("loading config %s: %w", path, err)
	}
	if err := warnIfPermissive(path); err != nil {
		fmt.Fprintln(os.Stderr, "bugrep: warning:", err)
	}
	return &cfg, nil
}

// Validate checks cross-field invariants that TOML decoding alone can't
// enforce: required scope for GitHub/GitLab instances, a required flavor for
// Jira instances, and that default_trackers refers to configured instances.
func (c *Config) Validate() error {
	names := make([]string, 0, len(c.Trackers))
	for name := range c.Trackers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tc := c.Trackers[name]
		if tc.Type == "" {
			return fmt.Errorf("tracker %q: missing type", name)
		}
		switch tc.Type {
		case "github", "gitea", "forgejo":
			if len(tc.Orgs) == 0 && len(tc.Repos) == 0 {
				return fmt.Errorf("tracker %q: %s instances must set orgs and/or repos; bugrep never searches a whole instance", name, tc.Type)
			}
		case "gitlab":
			if len(tc.Groups) == 0 && len(tc.Projects) == 0 {
				return fmt.Errorf("tracker %q: gitlab instances must set groups and/or projects; bugrep never searches all of GitLab", name)
			}
		case "jira":
			if tc.Flavor != "cloud" && tc.Flavor != "server" {
				return fmt.Errorf("tracker %q: jira instances must set flavor = \"cloud\" or \"server\"", name)
			}
		case "launchpad":
			if len(tc.Targets) == 0 {
				return fmt.Errorf("tracker %q: launchpad instances must set targets; there is no site-wide search", name)
			}
		case "bugzilla", "redmine":
			// no required scope
		default:
			return fmt.Errorf("tracker %q: unknown type %q", name, tc.Type)
		}
		if tc.URL == "" {
			return fmt.Errorf("tracker %q: missing url", name)
		}
	}

	for _, name := range c.DefaultTrackers {
		if _, ok := c.Trackers[name]; !ok {
			return fmt.Errorf("default_trackers refers to undefined tracker %q", name)
		}
	}
	return nil
}

// warnIfPermissive returns a non-nil error (meant to be printed as a
// warning, not fatal) when the config file is readable by users other than
// its owner. Config files often carry literal credentials.
func warnIfPermissive(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil //nolint:nilerr // best-effort check
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by group/other (mode %04o); consider chmod 600", path, info.Mode().Perm())
	}
	return nil
}

// StarterTemplate is written by `bugrep config init` as a starting point;
// every tracker in it is commented out.
const StarterTemplate = `# bugrep configuration — see https://github.com/ricardobranco777/bugrep
#
# Uncomment and fill in the trackers you want to search, then run
# ` + "`bugrep trackers test`" + ` to check connectivity and authentication.

# default_trackers = ["rhbz", "gh"]

[defaults]
state = "open"

# [trackers.rhbz]
# type = "bugzilla"
# url = "https://bugzilla.redhat.com"
# api_key_env = "RHBZ_API_KEY"

# [trackers.jira-work]
# type = "jira"
# url = "https://example.atlassian.net"
# flavor = "cloud"                      # cloud | server (Data Center = server)
# user = "me@example.com"
# token_env = "JIRA_TOKEN"

# [trackers.redmine]
# type = "redmine"
# url = "https://redmine.example.org"
# api_key_env = "REDMINE_API_KEY"

# [trackers.gh]
# type = "github"
# url = "https://api.github.com"        # or your GitHub Enterprise URL
# token_env = "GITHUB_TOKEN"
# orgs = ["myorg"]
# repos = ["otherowner/repo"]

# [trackers.gl]
# type = "gitlab"
# url = "https://gitlab.com"
# token_env = "GITLAB_TOKEN"
# groups = ["mygroup"]
# projects = ["other/project"]
`

// WriteStarter writes StarterTemplate to path, creating parent directories
// as needed. It refuses to overwrite an existing file unless force is true.
func WriteStarter(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(StarterTemplate), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// ResolveCredential resolves a credential following the standard bugrep
// order: environment variable, then command, then OS keyring, then a
// literal value. It returns an empty string with no error if none of the
// sources are configured.
func ResolveCredential(envVar string, cmd []string, keyringItem string, literal string) (string, error) {
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("environment variable %s is not set", envVar)
	}
	if len(cmd) > 0 {
		out, err := exec.Command(cmd[0], cmd[1:]...).Output() //nolint:gosec // user-configured command
		if err != nil {
			return "", fmt.Errorf("running credential command %q: %w", strings.Join(cmd, " "), err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	if keyringItem != "" {
		service, account := splitKeyringItem(keyringItem)
		v, err := keyringGet(service, account)
		if err != nil {
			return "", fmt.Errorf("reading keyring item %q (service %q, account %q): %w", keyringItem, service, account, err)
		}
		return v, nil
	}
	return literal, nil
}

// splitKeyringItem splits a "*_keyring" config value into the OS keyring's
// service and account: "bugrep/redmine" means service "bugrep", account
// "redmine". A value with no "/" is treated as the account under a default
// "bugrep" service.
func splitKeyringItem(item string) (service, account string) {
	if s, a, ok := strings.Cut(item, "/"); ok {
		return s, a
	}
	return "bugrep", item
}

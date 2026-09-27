// SPDX-License-Identifier: BSD-2-Clause

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func writeTemp(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	path := writeTemp(t, `
default_trackers = ["gh"]

[trackers.gh]
type = "github"
url = "https://api.github.com"
orgs = ["myorg"]

[trackers.rhbz]
type = "bugzilla"
url = "https://bugzilla.redhat.com"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Trackers) != 2 {
		t.Errorf("expected 2 trackers, got %d", len(cfg.Trackers))
	}
}

func TestLoadUnknownKey(t *testing.T) {
	path := writeTemp(t, `
[trackers.gh]
type = "github"
url = "https://api.github.com"
orgs = ["myorg"]
bogus = "x"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestValidateGithubRequiresScope(t *testing.T) {
	cfg := &Config{Trackers: map[string]TrackerConfig{
		"gh": {Type: "github", URL: "https://api.github.com"},
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for github tracker without orgs/repos")
	}
}

func TestValidateGitlabRequiresScope(t *testing.T) {
	cfg := &Config{Trackers: map[string]TrackerConfig{
		"gl": {Type: "gitlab", URL: "https://gitlab.com"},
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for gitlab tracker without groups/projects")
	}
}

func TestValidateJiraRequiresFlavor(t *testing.T) {
	cfg := &Config{Trackers: map[string]TrackerConfig{
		"jw": {Type: "jira", URL: "https://example.atlassian.net"},
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for jira tracker without flavor")
	}
}

func TestValidateUnknownDefaultTracker(t *testing.T) {
	cfg := &Config{
		DefaultTrackers: []string{"nope"},
		Trackers:        map[string]TrackerConfig{},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for default_trackers referring to undefined tracker")
	}
}

func TestValidateUnknownType(t *testing.T) {
	cfg := &Config{Trackers: map[string]TrackerConfig{
		"x": {Type: "launchpad", URL: "https://launchpad.net"},
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for unknown tracker type")
	}
}

func TestResolveCredentialEnv(t *testing.T) {
	t.Setenv("BUGREP_TEST_TOKEN", "secret")
	v, err := ResolveCredential("BUGREP_TEST_TOKEN", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if v != "secret" {
		t.Errorf("got %q, want %q", v, "secret")
	}
}

func TestResolveCredentialEnvMissing(t *testing.T) {
	_ = os.Unsetenv("BUGREP_TEST_TOKEN_UNSET")
	if _, err := ResolveCredential("BUGREP_TEST_TOKEN_UNSET", nil, "", ""); err == nil {
		t.Fatal("expected error for unset environment variable")
	}
}

func TestResolveCredentialCmd(t *testing.T) {
	v, err := ResolveCredential("", []string{"echo", "hello"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Errorf("got %q, want %q", v, "hello")
	}
}

func TestResolveCredentialKeyring(t *testing.T) {
	t.Cleanup(func() { keyringGet = keyring.Get })
	var gotService, gotAccount string
	keyringGet = func(service, account string) (string, error) {
		gotService, gotAccount = service, account
		return "secret-value", nil
	}

	v, err := ResolveCredential("", nil, "bugrep/redmine", "")
	if err != nil {
		t.Fatal(err)
	}
	if v != "secret-value" {
		t.Errorf("got %q, want %q", v, "secret-value")
	}
	if gotService != "bugrep" || gotAccount != "redmine" {
		t.Errorf("got service=%q account=%q, want service=%q account=%q", gotService, gotAccount, "bugrep", "redmine")
	}
}

func TestResolveCredentialKeyringNoSlashDefaultsService(t *testing.T) {
	t.Cleanup(func() { keyringGet = keyring.Get })
	var gotService, gotAccount string
	keyringGet = func(service, account string) (string, error) {
		gotService, gotAccount = service, account
		return "x", nil
	}

	if _, err := ResolveCredential("", nil, "redmine", ""); err != nil {
		t.Fatal(err)
	}
	if gotService != "bugrep" || gotAccount != "redmine" {
		t.Errorf("got service=%q account=%q, want service=%q account=%q", gotService, gotAccount, "bugrep", "redmine")
	}
}

func TestResolveCredentialKeyringError(t *testing.T) {
	t.Cleanup(func() { keyringGet = keyring.Get })
	keyringGet = func(service, account string) (string, error) {
		return "", keyring.ErrNotFound
	}
	if _, err := ResolveCredential("", nil, "bugrep/redmine", ""); err == nil {
		t.Fatal("expected error when the keyring item isn't found")
	}
}

func TestResolveCredentialLiteral(t *testing.T) {
	v, err := ResolveCredential("", nil, "", "literal-value")
	if err != nil {
		t.Fatal(err)
	}
	if v != "literal-value" {
		t.Errorf("got %q, want %q", v, "literal-value")
	}
}

# bugrep

A CLI that searches Bugzilla, Jira, Redmine, GitHub, GitLab, Gitea/Forgejo
and Launchpad issues through one interface. It searches every configured
tracker at once by default, merging and sorting the results into one list.

## Install

```sh
go install github.com/ricardobranco777/bugrep/cmd/bugrep@latest
```

Or build from a clone:

```sh
make build          # ./bugrep
make install        # into $(go env GOPATH)/bin
```

## Quick start

```sh
bugrep config init          # write a starter config to $XDG_CONFIG_HOME/bugrep/config.toml
$EDITOR ~/.config/bugrep/config.toml
bugrep trackers list
bugrep trackers test
bugrep search "kernel panic" -s open
bugrep show rhbz#2234567 --comments
bugrep open gh#owner/repo#42
```

Run `bugrep <command> --help` for the full flag reference.

## Configuration

The config file is TOML, at `$XDG_CONFIG_HOME/bugrep/config.toml` by default
(override with `--config` or `$BUGREP_CONFIG`). Every tracker instance gets
its own `[trackers.<name>]` section, and you can configure several instances
of the same backend type (e.g. two separate Bugzilla sites).

```toml
default_trackers = ["rhbz", "gh"]   # used when -t isn't given; otherwise every configured tracker is searched

[defaults]
state = "open"

[trackers.rhbz]
type = "bugzilla"
url = "https://bugzilla.redhat.com"
api_key_env = "RHBZ_API_KEY"        # optional: public bugs work anonymously

[trackers.jira-work]
type = "jira"
url = "https://example.atlassian.net"
flavor = "cloud"                    # "cloud" or "server" (Data Center = "server")
user = "me@example.com"
token_env = "JIRA_TOKEN"

[trackers.redmine]
type = "redmine"
url = "https://redmine.example.org"
api_key_env = "REDMINE_API_KEY"

[trackers.gh]
type = "github"
url = "https://api.github.com"      # or a GitHub Enterprise URL
token_env = "GITHUB_TOKEN"
orgs = ["myorg"]                    # at least one of orgs/repos is required
repos = ["otherowner/repo"]

[trackers.gl]
type = "gitlab"
url = "https://gitlab.com"
token_env = "GITLAB_TOKEN"
groups = ["mygroup"]                # at least one of groups/projects is required
projects = ["other/project"]

[trackers.codeberg]
type = "forgejo"                    # or "gitea"; both share one implementation
url = "https://codeberg.org"
token_env = "CODEBERG_TOKEN"
orgs = ["someorg"]

[trackers.lp-ubuntu]
type = "launchpad"
url = "https://api.launchpad.net/1.0"
targets = ["ubuntu/+source/linux"]  # a distro, a distro source package, or a project; at least one is required
```

bugrep never searches an entire GitHub/GitLab/Gitea/Forgejo instance or all
of Launchpad — those backends require an explicit scope, as shown above.
Bugzilla, Jira and Redmine have no such requirement, since they're normally
one self-contained instance already.

### Credentials

For each `*_env`/`*_cmd`/`*_keyring` pair, bugrep tries them in that order,
then falls back to a literal value (e.g. `token = "..."`, discouraged —
bugrep warns if the config file is readable by anyone but you):

| Field | Meaning |
|---|---|
| `token_env` / `api_key_env` | read from this environment variable |
| `token_cmd` / `api_key_cmd` | run this command (e.g. `["pass", "show", "jira/token"]`) and use its stdout, trimmed |
| `token_keyring` / `api_key_keyring` | read from the OS keyring; `"bugrep/redmine"` means service `bugrep`, account `redmine` |
| `token` / `api_key` | a literal value in the file |

GitHub, GitLab, Gitea/Forgejo, Bugzilla and Redmine all work anonymously
against public data if no credential is configured (at a lower rate limit).
Launchpad has no supported authentication in this version — it's always
anonymous. Jira always requires credentials.

## Commands

| Command | Does |
|---|---|
| `bugrep search [text...]` | federated search; see `--help` for all filters (`-t`, `-s`, `-a`/`-r`, `-l`, `-p`, `--since`, `--sort`, `--raw`, ...) |
| `bugrep show <ref>` | one issue's details; `--comments` also fetches comments |
| `bugrep open <ref>` | open an issue in `$BROWSER` |
| `bugrep trackers list` | list configured instances |
| `bugrep trackers test [name...]` | check connectivity and authentication |
| `bugrep config init` | write a starter config file |
| `bugrep completion <shell>` | generate a shell completion script (bash/zsh/fish/powershell) |

An issue reference (`<ref>` above) is `<tracker>#<key>`, e.g. `rhbz#2234567`,
`jira-work#PROJ-123`, or `gh#owner/repo#42` — the tracker name is whatever
you called its `[trackers.<name>]` section, so naming a Bugzilla instance
`boo` (for bugzilla.opensuse.org) gives refs like `boo#1282853`.

Output defaults to a table; `-o json`, `-o jsonl`, `-o csv` and `-o md` are
also available, and `--columns` picks which fields to show.

## Develop

```sh
make check       # fmt-check + vet + golangci-lint + race tests — what CI runs
make test        # go test ./...
make test-race   # go test -race ./...
make lint        # golangci-lint run ./...
make fmt         # gofmt -w .
make cover       # coverage report
```

See `GNUmakefile` for the full target list.

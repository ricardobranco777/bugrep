// SPDX-License-Identifier: BSD-2-Clause

// Package cli wires up bugrep's cobra commands: flag parsing, config
// loading, and dispatch into the search engine and output renderers.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ricardobranco777/bugrep/internal/config"
	"github.com/ricardobranco777/bugrep/internal/output"

	// Backend implementations register themselves via init(); import each
	// for its side effect as it's added.
	_ "github.com/ricardobranco777/bugrep/internal/backend/bugzilla"
	_ "github.com/ricardobranco777/bugrep/internal/backend/gitea"
	_ "github.com/ricardobranco777/bugrep/internal/backend/github"
	_ "github.com/ricardobranco777/bugrep/internal/backend/gitlab"
	_ "github.com/ricardobranco777/bugrep/internal/backend/jira"
	_ "github.com/ricardobranco777/bugrep/internal/backend/launchpad"
	_ "github.com/ricardobranco777/bugrep/internal/backend/redmine"
)

// globals holds the persistent (root-level) flag values, shared by every
// subcommand.
type globals struct {
	configPath string
	debug      bool
	timeout    time.Duration
	outputFmt  string
	columns    []string
}

var g globals

// NewRootCmd builds the bugrep command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "bugrep",
		Short:         "Search Bugzilla, Jira, Redmine, GitHub and GitLab issues from one CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&g.configPath, "config", "", "config file (default $XDG_CONFIG_HOME/bugrep/config.toml)")
	root.PersistentFlags().BoolVar(&g.debug, "debug", false, "log HTTP requests to stderr (credentials redacted)")
	root.PersistentFlags().DurationVar(&g.timeout, "timeout", 20*time.Second, "per-tracker request timeout")
	root.PersistentFlags().StringVarP(&g.outputFmt, "output", "o", string(output.FormatTable), "output format: table, json, jsonl, csv, md")
	root.PersistentFlags().StringSliceVar(&g.columns, "columns", nil, "columns to show (default: "+joinDefaultColumns()+")")

	root.AddCommand(
		newSearchCmd(),
		newShowCmd(),
		newOpenCmd(),
		newTrackersCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)
	return root
}

// Execute runs the root command and returns the process exit code.
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "bugrep:", err)
		if ec, ok := errors.AsType[*exitCodeErr](err); ok {
			return ec.code
		}
		return 1
	}
	return 0
}

func joinDefaultColumns() string {
	var s strings.Builder
	for i, c := range output.DefaultColumns {
		if i > 0 {
			s.WriteString(",")
		}
		s.WriteString(c)
	}
	return s.String()
}

// loadConfig resolves the config path (flag, then default) and loads it.
func loadConfig() (*config.Config, error) {
	path := g.configPath
	if path == "" {
		var err error
		path, err = config.DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	return config.Load(path)
}

// outputOptions builds the output.Options and format from the persistent
// flags, validating the format.
func outputOptions() (output.Format, output.Options, error) {
	format, err := output.ParseFormat(g.outputFmt)
	if err != nil {
		return "", output.Options{}, err
	}
	return format, output.Options{Columns: g.columns}, nil
}

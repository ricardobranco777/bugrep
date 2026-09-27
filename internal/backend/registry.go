// SPDX-License-Identifier: BSD-2-Clause

// Package backend holds the registry that maps a tracker type name (e.g.
// "github") to a constructor for that backend. Each backend package (see
// internal/backend/github, .../gitlab, etc.) registers itself from an
// init() function; the cli package imports them for side effects.
package backend

import (
	"context"
	"fmt"
	"sort"

	"github.com/ricardobranco777/bugrep/internal/config"
	"github.com/ricardobranco777/bugrep/internal/core"
	"github.com/ricardobranco777/bugrep/internal/httpx"
)

// NewFunc constructs a Backend for one configured instance.
type NewFunc func(ctx context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error)

var registry = map[string]NewFunc{}

// Register adds a constructor for tracker type name. It panics on a
// duplicate registration, since that can only be a programming error.
func Register(name string, fn NewFunc) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("backend: duplicate registration for type %q", name))
	}
	registry[name] = fn
}

// New builds the Backend for a configured instance, by its type.
func New(ctx context.Context, name string, tc config.TrackerConfig, httpOpts httpx.Options) (core.Backend, error) {
	fn, ok := registry[tc.Type]
	if !ok {
		return nil, fmt.Errorf("tracker %q: no backend implementation for type %q (%s)", name, tc.Type, availableTypes())
	}
	return fn(ctx, name, tc, httpOpts)
}

func availableTypes() string {
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	sort.Strings(types)
	if len(types) == 0 {
		return "no backends are registered yet"
	}
	return "available: " + fmt.Sprint(types)
}

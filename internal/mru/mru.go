// SPDX-License-Identifier: MIT

// Package mru remembers which tmux windows you most recently switched to.
//
// tmux exposes no per-window "last used" timestamp. #{window_activity} is
// output-driven, so a window running top or a build is permanently "most
// recent" whether or not you have looked at it, and #{session_last_attached}
// only moves when a client attaches — which is exactly what kaku-tab avoids
// doing when it retargets an existing tab. Neither answers "where was I?", so
// kaku-tab records its own picks.
//
// The list lives in a tmux server option rather than a state file. Window ids
// (@42) are only meaningful for the life of one tmux server, which is precisely
// how long a server option survives: nothing to migrate, nothing to garbage
// collect, and `tmux show-option -gqv @kaku-tab-mru` shows the whole thing when
// the order looks wrong.
package mru

import (
	"strings"

	"github.com/dsaad68/kaku-tab/internal/tmux"
)

// Option is the tmux server option holding the list, most recent first.
const Option = "@kaku-tab-mru"

// Cap bounds the stored list. Past a few dozen entries the tail is noise, and
// this keeps the option from growing without limit on a long-lived server.
const Cap = 64

// Store is where the list is kept. Production uses Tmux; tests use their own.
type Store interface {
	Get() string
	Set(string) error
}

// Tmux stores the list in a tmux server option.
type Tmux struct{}

func (Tmux) Get() string { return tmux.Option(Option, "") }

func (Tmux) Set(v string) error { return tmux.SetOption(Option, v) }

// List returns recorded window ids, most recent first.
func List(s Store) []string {
	return split(s.Get())
}

// Record pushes a window id to the front, removing any earlier occurrence so an
// id appears exactly once.
func Record(s Store, id string) error {
	if id == "" {
		return nil
	}
	out := []string{id}
	for _, prev := range split(s.Get()) {
		if prev != id && len(out) < Cap {
			out = append(out, prev)
		}
	}
	return s.Set(strings.Join(out, ","))
}

// Ranks maps window id to position, 0 being the most recent. Ids absent from
// the list are absent from the map; callers order those by their usual rule.
//
// current — the window the picker was invoked from — is demoted one place when
// it heads the list. It always does head it, because switching here is what
// recorded it. Leaving it at 0 would put "where you already are" under the
// cursor and make Enter a no-op, which is the one thing an MRU order exists to
// avoid; alt-tab has the same rule for the same reason.
func Ranks(list []string, current string) map[string]int {
	order := append([]string(nil), list...)
	if current != "" && len(order) > 1 && order[0] == current {
		order[0], order[1] = order[1], order[0]
	}
	m := make(map[string]int, len(order))
	for i, id := range order {
		if _, dup := m[id]; !dup {
			m[id] = i
		}
	}
	return m
}

func split(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

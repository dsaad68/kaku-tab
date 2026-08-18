// SPDX-License-Identifier: MIT

package action

import (
	"strconv"
	"strings"

	"github.com/dsaad68/kaku-tab/internal/kaku"
	"github.com/dsaad68/kaku-tab/internal/model"
	"github.com/dsaad68/kaku-tab/internal/tmux"
)

// DefaultTitleFormat is the tab title template used when @kaku-tab-title-format
// is unset. Placeholders:
//
//	%s  session name (a satellite keeps its own name)
//	%g  base session name, with any satellite suffix stripped
//	%w  window name
//	%i  window index
//
// The default is %g alone. %w is deliberately absent: with tmux
// automatic-rename on, every window is named after its foreground process, so
// including it yields a tab bar reading "api · zsh, web · zsh, db · zsh" —
// repeated on every tab and identifying nothing.
const DefaultTitleFormat = "%g"

// lastTitles avoids redundant CLI calls; these run from tmux hooks that fire
// often (every window switch), and each set-tab-title is a process spawn.
var lastTitles = map[string]string{}

// SyncTitles retitles each terminal tab after the tmux window its client is
// actually showing.
//
// Once one tmux window owns one tab, titling by session name alone makes every
// satellite tab of a session read identically.
//
// Returns what it set, keyed by tab id, so callers can report a dry run.
func SyncTitles(format, suffix string, dry bool) (map[string]string, error) {
	if format == "" {
		format = DefaultTitleFormat
	}
	if suffix == "" {
		suffix = model.DefaultSatelliteSuffix
	}
	if !kaku.Available() {
		return nil, nil
	}

	clients, err := tmux.Clients()
	if err != nil {
		return nil, err
	}
	rows, err := tmux.Run("list-clients", "-F",
		"#{client_tty}"+tmux.FS+"#{window_name}"+tmux.FS+"#{window_index}")
	if err != nil {
		return nil, err
	}

	meta := map[string][2]string{} // tty -> {window name, index}
	for _, line := range strings.Split(rows, "\n") {
		f := strings.Split(line, tmux.FS)
		if len(f) >= 3 {
			meta[f[0]] = [2]string{f[1], f[2]}
		}
	}

	out := map[string]string{}
	for _, c := range clients {
		p, ok := kaku.ByTTY(c.TTY)
		if !ok {
			continue // client is not inside this terminal
		}
		tab := strconv.Itoa(p.TabID)
		m := meta[c.TTY]

		r := strings.NewReplacer(
			"%s", c.Session,
			"%g", model.BaseSession(c.Session, suffix),
			"%w", squeeze(m[0]),
			"%i", m[1],
		)
		title := strings.TrimSpace(r.Replace(format))
		out[tab] = title

		if dry || lastTitles[tab] == title {
			continue
		}
		if err := kaku.SetTabTitle(tab, title); err == nil {
			lastTitles[tab] = title
		}
	}
	return out, nil
}

// squeeze trims a window name and collapses runs of spaces. Nerd-font window
// namers pad icons generously, and a tab is narrow.
func squeeze(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

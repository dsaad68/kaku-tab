// SPDX-License-Identifier: MIT

// Package resolve joins the terminal's pane list to tmux's client and window
// lists, producing one row per tmux window annotated with the tab it is
// visible in.
//
// The join key is the tty: `kaku cli list` (or `wezterm cli list`) reports
// tty_name per pane and `tmux list-clients` reports client_tty; for a tmux
// client running inside a terminal pane they are the same string.
//
// It deliberately does NOT use $WEZTERM_PANE. That variable is captured into
// the tmux session environment when the session is created and then goes
// stale — a long-lived session routinely advertises a pane id belonging to a
// tab that no longer holds it, so anything keyed on it targets the wrong tab.
package resolve

import (
	"sort"
	"strconv"

	"github.com/dsaad68/kaku-tab/internal/model"
)

// Source supplies the live inputs. Tests inject fixtures.
//
// Panes returns every pane at once, keyed by tmux window id — one bulk query
// rather than one per window, which matters for pane mode and search.
type Source interface {
	KakuPanes() ([]model.KakuPane, error)
	Clients() ([]model.Client, error)
	Windows() ([]model.RawWindow, error)
	Panes() (map[string][]model.Pane, error)
}

// Options tune which windows are listed.
type Options struct {
	Suffix      string // satellite suffix; defaults to model.DefaultSatelliteSuffix
	Scope       string // "all" | "session" | "group"
	SelfSession string // reference session for the session/group scopes
	WithPanes   bool   // populate Window.Panes_
	// Ignore lists session names to omit entirely, e.g. a throwaway popup
	// session bound to a key.
	Ignore []string
}

type placement struct {
	tab, gui, clientSession string
}

// Resolve performs the join. Rows come back ordered by session then window
// index, which is the order the tree renderer relies on for grouping.
func Resolve(src Source, opt Options) ([]model.Window, error) {
	if opt.Suffix == "" {
		opt.Suffix = model.DefaultSatelliteSuffix
	}

	panes, err := src.KakuPanes()
	if err != nil {
		return nil, err
	}
	clients, err := src.Clients()
	if err != nil {
		return nil, err
	}
	raw, err := src.Windows()
	if err != nil {
		return nil, err
	}
	var panesByWindow map[string][]model.Pane
	if opt.WithPanes {
		if panesByWindow, err = src.Panes(); err != nil {
			return nil, err
		}
	}

	// tty -> Kaku tab / GUI window
	byTTY := make(map[string]model.KakuPane, len(panes))
	for _, p := range panes {
		if p.TTYName != "" {
			byTTY[p.TTYName] = p
		}
	}

	// A window is Visible when some client's session currently shows it;
	// a session is attached when any client sits on it.
	visible := map[string]placement{}
	bySession := map[string]placement{}
	for _, c := range clients {
		kp, ok := byTTY[c.TTY]
		if !ok {
			continue // client is not inside the terminal (ssh, elsewhere)
		}
		pl := placement{
			tab:           strconv.Itoa(kp.TabID),
			gui:           strconv.Itoa(kp.WindowID),
			clientSession: c.Session,
		}
		if c.WindowID != "" {
			visible[c.WindowID] = pl
		}
		// Register under the BASE name: a satellite client still means "this
		// session has a tab". Without this, a session whose only client
		// sits on a satellite reports Detached, and its hidden windows would
		// wrongly spawn new tabs instead of retargeting the existing one.
		bySession[model.BaseSession(c.Session, opt.Suffix)] = pl
	}

	ignore := make(map[string]bool, len(opt.Ignore))
	for _, s := range opt.Ignore {
		ignore[s] = true
	}

	out := make([]model.Window, 0, len(raw))
	for _, rw := range raw {
		// Satellites share their base session's windows, so listing both would
		// duplicate every row once per group member.
		if model.IsSatellite(rw.Session, opt.Suffix) || ignore[rw.Session] {
			continue
		}
		if !inScope(rw, opt) {
			continue
		}

		w := model.Window{RawWindow: rw, Status: model.Detached}
		if pl, ok := visible[rw.ID]; ok {
			w.Status, w.TabID, w.GUIWin, w.ClientSession = model.Visible, pl.tab, pl.gui, pl.clientSession
		} else if pl, ok := bySession[rw.Session]; ok {
			w.Status, w.TabID, w.GUIWin, w.ClientSession = model.AttachedHidden, pl.tab, pl.gui, pl.clientSession
		}

		if opt.WithPanes {
			w.Panes_ = panesByWindow[rw.ID]
		}
		out = append(out, w)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Session != out[j].Session {
			return out[i].Session < out[j].Session
		}
		return numeric(out[i].Index) < numeric(out[j].Index)
	})
	return out, nil
}

func inScope(rw model.RawWindow, opt Options) bool {
	switch opt.Scope {
	case "session":
		return rw.Session == opt.SelfSession
	case "group":
		if rw.Grouped && rw.Group != "" {
			return rw.Group == model.BaseSession(opt.SelfSession, opt.Suffix)
		}
		return rw.Session == opt.SelfSession
	default:
		return true
	}
}

func numeric(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1 << 30
	}
	return n
}

// Find returns the window with the given tmux window id.
func Find(ws []model.Window, id string) (model.Window, bool) {
	for _, w := range ws {
		if w.ID == id {
			return w, true
		}
	}
	return model.Window{}, false
}

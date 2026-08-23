// SPDX-License-Identifier: MIT

// Package model holds the data types shared across kaku-tab and the rules for
// naming satellite sessions.
//
// A satellite is a tmux grouped session (api~kaku2) that shares its base
// session's window list but keeps its own current-window. tmux stores
// current-window per session, not per client, so without satellites two Kaku
// tabs attached to one session always display the same window — which is what
// makes "one tmux window = one Kaku tab" impossible to arrange by hand.
package model

import (
	"strings"
	"unicode"

	"github.com/dsaad68/kaku-tab/internal/agent"
)

// DefaultSatelliteSuffix separates a base session name from its satellite index.
const DefaultSatelliteSuffix = "~kaku"

// Status describes where a tmux window is currently displayed, if anywhere.
type Status int

const (
	// Detached: no tmux client anywhere is attached to this window's session.
	Detached Status = iota
	// AttachedHidden: the session has a Kaku tab, but that tab currently shows
	// a different window of the same session.
	AttachedHidden
	// Visible: a Kaku tab is displaying exactly this window.
	Visible
)

func (s Status) String() string {
	switch s {
	case Visible:
		return "VISIBLE"
	case AttachedHidden:
		return "ATTACHED_HIDDEN"
	default:
		return "DETACHED"
	}
}

// KakuPane is one pane as reported by `kaku cli list --format json`.
type KakuPane struct {
	PaneID   int    `json:"pane_id"`
	TabID    int    `json:"tab_id"`
	WindowID int    `json:"window_id"` // Kaku GUI window, not a tmux window
	TTYName  string `json:"tty_name"`
	TabTitle string `json:"tab_title"`
}

// Client is one tmux client. WindowID is the window its session currently
// shows, which every client of that session reports identically.
type Client struct {
	TTY      string
	Session  string
	WindowID string
}

// RawWindow is one row of `tmux list-windows -a`.
type RawWindow struct {
	Session  string
	ID       string // @42 — stable; indices shift under renumber-windows
	Index    string
	Name     string
	Panes    int
	Path     string
	Activity bool
	Zoomed   bool
	Grouped  bool
	Group    string
}

// Pane is one row of `tmux list-panes`.
type Pane struct {
	ID     string // %17
	Index  string
	Cmd    string
	Path   string
	Active bool

	// Agent is the AI coding agent running in this pane, published by its own
	// hooks into the pane option. Zero value means none. This is why the pane
	// is knowable at all: pane_current_command reports "node" for Claude Code.
	Agent agent.Record
}

// Window is a resolved tmux window: everything needed to draw a row and to act
// on it.
type Window struct {
	RawWindow

	Status Status
	TabID  string // Kaku tab showing it, or "" when Detached
	GUIWin string // Kaku GUI window holding that tab

	// ClientSession is the session actually attached inside TabID, which may be
	// a satellite rather than Session. Retargeting must select-window against
	// this, not the base: current-window is per-session, so aiming at the base
	// would focus the right tab while leaving it on the wrong window.
	ClientSession string

	// Agent is the most actionable agent record among this window's panes, so a
	// window row can show "something in here wants you" without pane mode.
	Agent agent.Record

	Panes_ []Pane // populated only in pane mode
}

// IsSatellite reports whether a session name was created by kaku-tab, i.e. it
// carries the suffix followed by an index.
func IsSatellite(session, suffix string) bool {
	if suffix == "" {
		return false
	}
	i := strings.Index(session, suffix)
	if i < 0 {
		return false
	}
	rest := session[i+len(suffix):]
	return rest != "" && unicode.IsDigit(rune(rest[0]))
}

// BaseSession strips a satellite suffix, returning the shared base name.
// Non-satellite names are returned unchanged.
func BaseSession(session, suffix string) string {
	if !IsSatellite(session, suffix) {
		return session
	}
	return session[:strings.Index(session, suffix)]
}

// NextSatellite returns the first unused satellite name for a base session.
// exists reports whether a session name is already taken.
func NextSatellite(base, suffix string, exists func(string) bool) string {
	for n := 2; n <= 64; n++ {
		cand := base + suffix + itoa(n)
		if !exists(cand) {
			return cand
		}
	}
	return base + suffix + "x"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

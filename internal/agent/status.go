// SPDX-License-Identifier: MIT

package agent

import "strconv"

// Counts is the tally the tmux status segment renders.
type Counts struct {
	Waiting int // Perm + Ask: blocked on you right now
	Done    int
	Failed  int
	Working int
}

// Open is how many agent sessions are running at all.
func (c Counts) Open() int { return c.Waiting + c.Done + c.Failed + c.Working }

// Attention is how many of them want something from you. Working is the only
// state that does not.
func (c Counts) Attention() int { return c.Waiting + c.Done + c.Failed }

// Tally buckets a set of records.
func Tally(rs []Record) Counts {
	var c Counts
	for _, r := range rs {
		switch r.State {
		case Perm, Ask:
			c.Waiting++
		case Done:
			c.Done++
		case Err:
			c.Failed++
		case Busy:
			c.Working++
		}
	}
	return c
}

// Default status-bar icons. Nerd font, matching the register the rest of a
// themed tmux status bar is already in.
const (
	IconAgents = "\U000f06a9" // nf-md-robot: how many agents are open
	IconNotify = "\U000f009a" // nf-md-bell: how many of them want you
)

// Theme is the catppuccin status-module shape, resolved from live tmux options
// by the caller. Kept as plain strings so the rendering below stays a pure
// function with no tmux in it.
type Theme struct {
	Sep      string // rounded separator opening the icon block
	IconFG   string
	TextFG   string
	TextBG   string
	AgentBG  string // icon block for the "agents open" pill
	NotifyBG string // ... for the "wants you" pill, when non-zero
	IdleBG   string // ... and when zero
	AgentIco string
	NotifIco string
}

// pill renders one status module: a rounded, coloured icon block followed by
// its value on the shared module background.
//
// Built here rather than delegated to a catppuccin module because a real module
// always paints its icon and separators — including around an empty value,
// which is what this segment is most of the time.
//
// The spaces are load-bearing and asymmetric on purpose. One pads the icon
// inside its coloured block; the next belongs to the value's block; the trailing
// one is catppuccin's right separator, the one-cell gap between neighbouring
// pills. Drop it and these fuse into each other and into the module beside them.
func (t Theme) pill(bg, icon, value string) string {
	return "#[fg=" + bg + "]" + t.Sep +
		"#[fg=" + t.IconFG + ",bg=" + bg + "] " + icon + " " +
		"#[fg=" + t.TextFG + ",bg=" + t.TextBG + "] " + value +
		"#[fg=" + t.TextBG + "] "
}

// Segment renders the status-right counter: how many agents are open, and how
// many of them are waiting on you.
//
// Empty when nothing is running, so the status bar carries no icon, no zero and
// no stray separator the rest of the time.
func (t Theme) Segment(c Counts) string {
	if c.Open() == 0 {
		return ""
	}
	// The second pill is drawn even at zero, greyed rather than hidden. A count
	// that vanished would shift the first pill sideways every time an agent
	// finished — exactly when you are looking at it.
	notify := t.IdleBG
	if c.Attention() > 0 {
		notify = t.NotifyBG
	}
	return t.pill(t.AgentBG, t.AgentIco, strconv.Itoa(c.Open())) +
		t.pill(notify, t.NotifIco, strconv.Itoa(c.Attention()))
}

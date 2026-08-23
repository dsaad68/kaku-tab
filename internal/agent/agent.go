// SPDX-License-Identifier: MIT

// Package agent carries the state an AI coding agent (Claude Code or Devin CLI)
// publishes about itself, and the rules for reading it back.
//
// The transport is a tmux *pane* option, @kt_agent, written by `kaku-tab hook`
// from inside the agent's own process tree — which is how the pane is known at
// all: a hook process inherits $TMUX_PANE from the agent that spawned it.
//
// Storing it on the pane rather than in a state directory means staleness is
// structural rather than swept: close the pane and the record goes with it.
// The one leak that survives is an agent killed inside a pane that lives on,
// which is what the PID field is for.
package agent

import (
	"strconv"
	"strings"
	"syscall"
)

// PaneOption is the tmux pane option the state is published in.
//
// NEVER set this with `set-option -w`. tmux pane options inherit from window
// options, so a window-scoped @kt_agent would be read back by every agent-free
// pane in that window and the whole table would report agents. The window-level
// rollup deliberately uses a different name, see WindowOption.
const PaneOption = "@kt_agent"

// MsgOption carries the human-readable context for a pane's agent state: the
// prompt being worked on, the tool awaiting permission, the reply that ended the
// turn.
//
// A second option rather than a field in PaneOption, because a message is free
// text and PaneOption's format depends on every field being from a fixed
// alphabet. Both are pane-scoped, so both are written with -p only.
const MsgOption = "@kt_agent_msg"

// MaxMsg caps what is stored. A whole assistant reply in a tmux option helps
// nobody, and the box shows a few lines at most.
const MaxMsg = 300

// WindowOption is the per-window rollup, written only by `kaku-tab agents
// --refresh` for use in tmux window formats. It is a distinct name precisely
// because setting PaneOption at window scope would corrupt the per-pane read.
const WindowOption = "@kt_agent_win"

// Agent names. These are the only two values Record.Agent ever holds.
const (
	Claude = "claude"
	Devin  = "devin"
)

// State is what an agent is doing right now.
type State string

const (
	// None is the zero value: no agent, or a record we rejected.
	None State = ""
	// Busy: working. Not something you owe a response to.
	Busy State = "busy"
	// Perm: blocked asking permission for a tool call.
	Perm State = "perm"
	// Ask: blocked on a question it put to you.
	Ask State = "ask"
	// Done: finished a turn; there is output waiting for you.
	Done State = "done"
	// Err: the turn ended on an error.
	Err State = "err"
)

// Record is one agent's published state.
type Record struct {
	Agent string // Claude or Devin
	State State
	PID   int   // the hook process's parent, i.e. the agent itself
	At    int64 // unix seconds, for display only

	// Msg is context for State, and belongs to it: it is stored tagged with the
	// state it was written for and dropped on read when the two disagree. That
	// is what stops a permission request from still being displayed after the
	// approval moved the pane back to Busy.
	Msg string
}

// Empty reports whether there is no agent here.
func (r Record) Empty() bool { return r.State == None }

// Attention reports whether this state is one you owe a response to. Busy is
// the only state that is not: everything else is the agent waiting on you.
func (r Record) Attention() bool {
	switch r.State {
	case Perm, Ask, Done, Err:
		return true
	default:
		return false
	}
}

// Rank orders records by how much they want you, lowest first. Perm and Ask
// outrank Err because a blocked agent is burning wall-clock right now, where a
// failed turn has already stopped. Used to roll panes up to a window and
// windows up to a session header.
func (r Record) Rank() int {
	switch r.State {
	case Perm:
		return 1
	case Ask:
		return 2
	case Err:
		return 3
	case Done:
		return 4
	case Busy:
		return 5
	default:
		return 6
	}
}

// Format renders a record for the pane option.
//
// Colon-separated, where the rest of this tool uses \x1f: every field here is
// from a fixed alphabet — two agent names, five state names, two integers — so
// unlike a tmux window name none of them can contain the separator.
func Format(r Record) string {
	if r.Empty() {
		return ""
	}
	return r.Agent + ":" + string(r.State) + ":" +
		strconv.Itoa(r.PID) + ":" + strconv.FormatInt(r.At, 10)
}

// Parse reads a pane option value back. Anything malformed returns the zero
// Record: a value we cannot understand is treated as no agent rather than as an
// error, because it is user-writable tmux state and the picker must not care.
func Parse(s string) Record {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 4 {
		return Record{}
	}
	name := parts[0]
	if name != Claude && name != Devin {
		return Record{}
	}
	st := State(parts[1])
	switch st {
	case Busy, Perm, Ask, Done, Err:
	default:
		return Record{}
	}
	pid, err := strconv.Atoi(parts[2])
	if err != nil || pid <= 1 {
		return Record{}
	}
	at, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || at < 0 {
		return Record{}
	}
	return Record{Agent: name, State: st, PID: pid, At: at}
}

// FormatMsg renders the message option, tagging it with the state it describes.
// Returns "" when there is nothing worth storing, which the caller writes as an
// unset rather than an empty option.
func FormatMsg(st State, msg string) string {
	msg = sanitize(msg)
	if st == None || msg == "" {
		return ""
	}
	return string(st) + ":" + msg
}

// ParseMsg reads the message option back, given the state the pane currently
// reports. A message tagged with a different state is stale — the pane has moved
// on since it was written — and is discarded.
func ParseMsg(now State, v string) string {
	tag, msg, ok := strings.Cut(v, ":")
	if !ok || State(tag) != now || now == None {
		return ""
	}
	return strings.TrimSpace(msg)
}

// sanitize reduces a hook payload's text to one line worth showing.
//
// The text is whatever the agent produced — an assistant reply is a whole
// markdown document, headings and code blocks and all. Flattening the lot onto
// one line yields nonsense, and any box-drawing characters in it land inside
// the picker's own box and read as a rendering fault. So: take the first line
// that says something, drop the line art, collapse the whitespace, cap it.
func sanitize(s string) string {
	line := firstMeaningfulLine(s)

	var b strings.Builder
	space := true // also trims the leading whitespace
	for _, r := range line {
		switch {
		case isLineArt(r):
			continue
		case r < 0x20 || r == 0x7f:
			r = ' '
		}
		if r == ' ' {
			if space {
				continue
			}
			space = true
		} else {
			space = false
		}
		b.WriteRune(r)
		if b.Len() >= MaxMsg {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// firstMeaningfulLine picks the first line with prose in it, skipping the
// blanks, code fences and pure line-art rules that open so many replies, and
// shedding a leading markdown marker so the text starts at a word.
func firstMeaningfulLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			continue
		}
		if onlyLineArt(line) {
			continue
		}
		return stripMarker(line)
	}
	return ""
}

// stripMarker removes an unambiguous leading markdown marker. Bullets need the
// trailing space to qualify, so a command line beginning with a flag is left
// alone.
func stripMarker(line string) string {
	switch {
	case strings.HasPrefix(line, "#"):
		return strings.TrimSpace(strings.TrimLeft(line, "#"))
	case strings.HasPrefix(line, ">"):
		return strings.TrimSpace(strings.TrimLeft(line, ">"))
	case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "):
		return strings.TrimSpace(line[2:])
	}
	return line
}

// isLineArt reports whether a rune is from the Box Drawing or Block Elements
// blocks. These are never prose, and inside the picker's box they masquerade as
// its borders.
func isLineArt(r rune) bool { return r >= 0x2500 && r <= 0x259f }

func onlyLineArt(line string) bool {
	for _, r := range line {
		if r != ' ' && !isLineArt(r) {
			return false
		}
	}
	return true
}

// Live reports whether the agent process is still running. This is the backstop
// for the one case pane-scoped storage cannot handle on its own: an agent killed
// outright, so no SessionEnd hook ever fires, inside a pane that survives it.
func Live(r Record) bool {
	if r.Empty() || r.PID <= 1 {
		return false
	}
	err := syscall.Kill(r.PID, 0)
	// nil: ours and alive. EPERM: alive, just not ours to signal.
	return err == nil || err == syscall.EPERM
}

// Best returns the most actionable of a set of records, and whether there was
// one at all.
func Best(rs []Record) Record {
	var best Record
	for _, r := range rs {
		if r.Empty() {
			continue
		}
		if best.Empty() || r.Rank() < best.Rank() {
			best = r
		}
	}
	return best
}

// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dsaad68/kaku-tab/internal/agent"
	"github.com/dsaad68/kaku-tab/internal/model"
)

func agentSample() []model.Window {
	perm := agent.Record{Agent: agent.Claude, State: agent.Perm, PID: 100, At: 1}
	busy := agent.Record{Agent: agent.Devin, State: agent.Busy, PID: 200, At: 1}
	return []model.Window{
		{RawWindow: model.RawWindow{Session: "api", ID: "@1", Index: "1", Name: "claude", Panes: 2, Path: "/home/u/api"},
			Status: model.AttachedHidden, TabID: "8", ClientSession: "api", Agent: perm,
			Panes_: []model.Pane{
				{ID: "%1", Index: "1", Cmd: "node", Path: "/home/u/api", Active: true, Agent: perm},
				{ID: "%2", Index: "2", Cmd: "zsh", Path: "/home/u/api"},
			}},
		{RawWindow: model.RawWindow{Session: "api", ID: "@2", Index: "2", Name: "just", Panes: 1, Path: "/home/u/api/cmd"},
			Status: model.Visible, TabID: "8", ClientSession: "api", Agent: busy,
			Panes_: []model.Pane{{ID: "%3", Index: "1", Cmd: "devin", Path: "/home/u", Active: true, Agent: busy}}},
		{RawWindow: model.RawWindow{Session: "scratch", ID: "@3", Index: "1", Name: "", Panes: 1, Path: "/home/u/td"},
			Status: model.Detached,
			Panes_: []model.Pane{{ID: "%4", Index: "1", Cmd: "zsh", Path: "/home/u", Active: true}}},
	}
}

// The agent column is reserved on every row, agent or not. An indicator drawn
// only where there is an agent would shift every other column on those rows and
// nowhere else — the same class of bug the badge column was built to avoid.
func TestAgentCellIsAlwaysAgentCellsWide(t *testing.T) {
	for _, r := range []agent.Record{
		{},
		{Agent: agent.Claude, State: agent.Perm, PID: 2},
		{Agent: agent.Claude, State: agent.Ask, PID: 2},
		{Agent: agent.Claude, State: agent.Done, PID: 2},
		{Agent: agent.Claude, State: agent.Err, PID: 2},
		{Agent: agent.Claude, State: agent.Busy, PID: 2},
		{Agent: agent.Devin, State: agent.Perm, PID: 2},
		{Agent: agent.Devin, State: agent.Busy, PID: 2},
	} {
		if w := ansi.StringWidth(agentCell(r)); w != agentCells {
			t.Errorf("agentCell(%+v) is %d cells, want %d", r, w, agentCells)
		}
	}
}

// Every glyph in the column must be exactly one cell. A double-width one would
// still satisfy the total above by luck of pairing, and then break the moment
// it appeared next to a different partner.
func TestEveryAgentGlyphIsOneCell(t *testing.T) {
	for _, g := range []string{
		glyphClaude, glyphDevin, glyphPerm, glyphAsk, glyphDone, glyphErr, glyphBusy,
	} {
		if w := ansi.StringWidth(g); w != 1 {
			t.Errorf("glyph %q is %d cells, want 1", g, w)
		}
	}
}

// The column budget in renderRow is an exact sum of every fixed cell. Getting it
// wrong pushes the row past the list width and truncateANSI eats the badge —
// the rightmost column, and the whole point of the tool. Pinned at a narrow
// width, in both list modes, where the budget is tightest.
func TestAgentColumnKeepsRowsInBudget(t *testing.T) {
	for _, paneMode := range []bool{false, true} {
		for _, w := range []int{60, 80, 150} {
			m := New(agentSample(), Options{Tree: true, PaneMode: paneMode, SelfTab: "8"})
			m.width, m.height = w, 30
			m.refilter()
			for i, r := range m.rows {
				line := m.renderRow(r, i == 0)
				if got := ansi.StringWidth(line); got > m.rowWidth() {
					t.Errorf("panes=%v width=%d row %d: %d cells > rowWidth %d",
						paneMode, w, i, got, m.rowWidth())
				}
				if r.kind != kindHeader && !strings.Contains(ansi.Strip(line), "⟦") {
					t.Errorf("panes=%v width=%d row %d: badge truncated away: %q",
						paneMode, w, i, ansi.Strip(line))
				}
			}
		}
	}
}

// The two halves are independent: the shape says which agent, the shape beside
// it says what it wants. Neither may be inferable only from the other's colour.
func TestAgentAndStateAreBothShown(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30
	var sawClaudePerm, sawDevinBusy bool
	for _, r := range m.rows {
		cell := ansi.Strip(agentCell(r.agent))
		if cell == glyphClaude+" "+glyphPerm {
			sawClaudePerm = true
		}
		if cell == glyphDevin+" "+glyphBusy {
			sawDevinBusy = true
		}
	}
	if !sawClaudePerm || !sawDevinBusy {
		t.Errorf("expected both agent/state pairs (claude+perm=%v devin+busy=%v)",
			sawClaudePerm, sawDevinBusy)
	}
}

// The filter keeps what is waiting on you, which is not the same as what has an
// agent: a window whose agent is merely working is not waiting.
func TestAgentsOnlyKeepsOnlyAttention(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, AgentsOnly: true, SelfTab: "8"})
	m.width, m.height = 150, 30
	for _, r := range m.rows {
		if r.kind == kindWindow && r.win.ID != "@1" {
			t.Errorf("window %s survived the agents filter with agent %+v", r.win.ID, r.win.Agent)
		}
	}
	if len(m.rows) == 0 {
		t.Fatal("agents filter emptied a list that has a waiting agent")
	}
}

// A session header has to say "something in here wants you" without the user
// first opening the group.
func TestHeaderInheritsMostActionableAgent(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30
	for _, r := range m.rows {
		if r.kind == kindHeader && r.group == "api" && r.agent.State != agent.Perm {
			t.Errorf("api header agent = %+v, want the blocked one", r.agent)
		}
	}
}

// Typing an agent name should narrow to its windows even though the name is
// never drawn as text.
func TestAgentNameIsSearchable(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30
	for _, r := range m.rows {
		if r.kind == kindWindow && r.win.ID == "@2" && !strings.Contains(r.search, "devin") {
			t.Errorf("window @2 search text %q lacks the agent name", r.search)
		}
	}
}

// The glyphs are compact and nothing on screen says what they mean; the footer
// is where you find out, for whichever row the cursor is on.
func TestFooterSpellsOutTheSelectedAgent(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30

	// A row with no agent must not describe one. Probed by the box's own border
	// rather than by wording: the help bar has a "waiting agents" key of its
	// own, and a substring probe hits that instead. The line *count* is no
	// probe either — the footer is padded to a constant height so the list does
	// not move as the cursor does.
	for i, vi := range m.view {
		if m.rows[vi].agent.Empty() {
			m.cursor = i
			if got := ansi.Strip(strings.Join(m.footerLines(), "\n")); strings.Contains(got, "╭") {
				t.Errorf("a box was drawn for an agent-free row:\n%s", got)
			}
			break
		}
	}

	// A row with one must name the agent and say what it wants, in words.
	for i, vi := range m.view {
		if m.rows[vi].agent.State == agent.Perm {
			m.cursor = i
			got := ansi.Strip(strings.Join(m.footerLines(), " "))
			if !strings.Contains(got, "claude") || !strings.Contains(got, "waiting for permission") {
				t.Errorf("footer = %q, want the agent and its state named", got)
			}
			return
		}
	}
	t.Fatal("no perm row in the sample to select")
}

// Regression: the agent box is up to six lines and appears only under the
// cursor, so on a short popup View emitted more lines than the frame had — the
// title and the prompt scrolled off the top. tmux fixes a popup's height at
// creation, so there is no growing out of it.
//
// 60x17 is the documented compact default (60%,70%) on a 24-row terminal, which
// is where this was reproduced.
func TestViewNeverOverflowsTheFrame(t *testing.T) {
	ws := agentSample()
	ws[0].Agent.Msg = strings.Repeat("a message long enough to wrap several times ", 6)
	for _, size := range [][2]int{{60, 17}, {80, 12}, {100, 10}, {150, 40}, {60, 9}} {
		m := New(ws, Options{Tree: true, SelfTab: "8"})
		m.width, m.height = size[0], size[1]
		for i := range m.view {
			m.cursor = i
			m.ensureVisible()
			if got := strings.Count(m.View(), "\n") + 1; got > m.height {
				t.Errorf("%dx%d row %d: View is %d lines, frame is %d",
					size[0], size[1], i, got, m.height)
			}
		}
	}
}

// The footer is held at a constant height as the cursor moves. Sized to the box
// currently on screen it resized the viewport under your hands, so arrowing onto
// an agent row scrolled the list several rows in one keypress.
func TestListHeightDoesNotMoveWithTheCursor(t *testing.T) {
	ws := agentSample()
	ws[0].Agent.Msg = "Bash: git push origin main"
	m := New(ws, Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30

	first := m.listHeight()
	for i := range m.view {
		m.cursor = i
		if got := m.listHeight(); got != first {
			t.Fatalf("listHeight is %d on row %d but %d on row 0", got, i, first)
		}
	}
}

// Room is held for the box only when there is an agent to put in it, so a list
// with none is unchanged.
func TestNoAgentMeansNoReservedBox(t *testing.T) {
	plain := New(sample(), Options{Tree: true, SelfTab: "8"})
	plain.width, plain.height = 150, 30
	withAgents := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	withAgents.width, withAgents.height = 150, 30

	if plain.footerReserve() != len(plain.helpLines()) {
		t.Errorf("agent-free list reserved %d rows, want just the help bar (%d)",
			plain.footerReserve(), len(plain.helpLines()))
	}
	if withAgents.footerReserve() <= plain.footerReserve() {
		t.Errorf("a list with agents reserved %d rows, no more than the %d of one without",
			withAgents.footerReserve(), plain.footerReserve())
	}
}

// However tall the footer wants to be, the list keeps some rows.
func TestListKeepsAMinimumHeight(t *testing.T) {
	ws := agentSample()
	ws[0].Agent.Msg = strings.Repeat("long ", 100)
	for _, h := range []int{6, 9, 12, 20} {
		m := New(ws, Options{Tree: true, SelfTab: "8"})
		m.width, m.height = 60, h
		if got := m.listHeight(); got < minListRows {
			t.Errorf("height %d: listHeight %d, want at least %d", h, got, minListRows)
		}
	}
}

// The box appears with the cursor and vanishes with it, so a list holding no
// agents looks exactly as it did before any of this existed.
func TestAgentBoxFollowsTheCursor(t *testing.T) {
	ws := agentSample()
	ws[0].Agent.Msg = "Bash: git push origin main"
	m := New(ws, Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30

	var withAgent, without int
	for i, vi := range m.view {
		m.cursor = i
		if m.rows[vi].agent.Empty() {
			without = len(m.agentBox(maxAgentBox))
		} else {
			withAgent = len(m.agentBox(maxAgentBox))
		}
	}
	if without != 0 {
		t.Errorf("box drawn (%d lines) for a row with no agent", without)
	}
	if withAgent == 0 {
		t.Error("no box for a row with an agent")
	}
}

// It has to say what the agent wants, not just that it wants something.
func TestAgentBoxShowsStateAndMessage(t *testing.T) {
	ws := agentSample()
	ws[0].Agent.Msg = "Bash: git push origin main"
	m := New(ws, Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30

	for i, vi := range m.view {
		if m.rows[vi].agent.State != agent.Perm {
			continue
		}
		m.cursor = i
		got := ansi.Strip(strings.Join(m.agentBox(maxAgentBox), "\n"))
		for _, want := range []string{"claude", "waiting for permission", "git push origin main"} {
			if !strings.Contains(got, want) {
				t.Errorf("box missing %q:\n%s", want, got)
			}
		}
		return
	}
	t.Fatal("no perm row in the sample")
}

// Every line of the box must be the same width, or the right border frays.
func TestAgentBoxLinesAreUniformWidth(t *testing.T) {
	ws := agentSample()
	ws[0].Agent.Msg = strings.Repeat("a long message that has to wrap ", 12)
	for _, width := range []int{60, 90, 150, 220} {
		m := New(ws, Options{Tree: true, SelfTab: "8"})
		m.width, m.height = width, 40
		for i, vi := range m.view {
			if m.rows[vi].agent.State != agent.Perm {
				continue
			}
			m.cursor = i
			lines := m.agentBox(maxAgentBox)
			if len(lines) == 0 {
				t.Fatalf("width %d: no box", width)
			}
			first := ansi.StringWidth(lines[0])
			for j, l := range lines {
				if w := ansi.StringWidth(l); w != first {
					t.Errorf("width %d: box line %d is %d cells, line 0 is %d", width, j, w, first)
				}
			}
			// And it must fit inside the frame.
			if first > m.innerW() {
				t.Errorf("width %d: box is %d cells, frame inner is %d", width, first, m.innerW())
			}
		}
	}
}

// A long message is elided, never allowed to push the help bar off screen.
func TestAgentBoxCapsMessageLines(t *testing.T) {
	ws := agentSample()
	ws[0].Agent.Msg = strings.Repeat("word ", 500)
	m := New(ws, Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 100, 30
	for i, vi := range m.view {
		if m.rows[vi].agent.State != agent.Perm {
			continue
		}
		m.cursor = i
		// border top + state line + at most agentBoxLines + border bottom
		if got, max := len(m.agentBox(maxAgentBox)), maxAgentBox; got > max {
			t.Errorf("box is %d lines, want at most %d", got, max)
		}
		return
	}
}

// A header inherits its children's agent, and the child carrying it is the very
// next line — drawn on both, a one-window session showed the pair twice, one row
// apart. The record stays on the row so the box still opens; only the glyphs go.
func TestHeaderDoesNotDrawTheAgentGlyphs(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30

	var checkedHeader, checkedChild bool
	for _, r := range m.rows {
		line := ansi.Strip(m.renderRow(r, false))
		switch r.kind {
		case kindHeader:
			if r.agent.Empty() {
				continue
			}
			checkedHeader = true
			for _, g := range []string{glyphClaude, glyphDevin, glyphPerm, glyphBusy} {
				if strings.Contains(line, g) {
					t.Errorf("header %q still draws %q: %s", r.group, g, line)
				}
			}
			// ... but it still knows, so the box opens when the cursor rests here.
			if agentWords(r.agent) == "" {
				t.Errorf("header %q lost its agent record", r.group)
			}
		default:
			if r.agent.Empty() {
				continue
			}
			checkedChild = true
			if !strings.Contains(line, glyphClaude) && !strings.Contains(line, glyphDevin) {
				t.Errorf("child row lost its agent glyph: %s", line)
			}
		}
	}
	if !checkedHeader || !checkedChild {
		t.Fatalf("sample exercised header=%v child=%v, need both", checkedHeader, checkedChild)
	}
}

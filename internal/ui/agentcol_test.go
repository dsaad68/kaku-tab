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

	// A row with no agent must not describe one. Counted rather than matched on
	// wording: the help bar has a "waiting agents" key of its own, and a
	// substring probe hits that instead.
	for i, vi := range m.view {
		if m.rows[vi].agent.Empty() {
			m.cursor = i
			if got, want := len(m.footerLines()), len(m.helpLines()); got != want {
				t.Errorf("footer is %d lines on an agent-free row, want %d (help only)", got, want)
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

// The footer grows by a line when it describes an agent, so the list has to
// give one back — otherwise the bottom row is drawn over the frame.
func TestListHeightReservesTheAgentLine(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30

	var plain, withAgent int
	for i, vi := range m.view {
		m.cursor = i
		if m.rows[vi].agent.Empty() {
			plain = m.listHeight()
		} else {
			withAgent = m.listHeight()
		}
	}
	if plain == 0 || withAgent == 0 {
		t.Fatal("sample lacks both an agent row and an agent-free one")
	}
	if withAgent >= plain {
		t.Errorf("listHeight %d with the agent line, %d without; the line was not reserved",
			withAgent, plain)
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
			without = len(m.agentBox())
		} else {
			withAgent = len(m.agentBox())
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
		got := ansi.Strip(strings.Join(m.agentBox(), "\n"))
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
			lines := m.agentBox()
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
		if got, max := len(m.agentBox()), 3+agentBoxLines; got > max {
			t.Errorf("box is %d lines, want at most %d", got, max)
		}
		return
	}
}

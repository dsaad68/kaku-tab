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
func TestAgentCellIsAlwaysOneCell(t *testing.T) {
	for _, r := range []agent.Record{
		{},
		{Agent: agent.Claude, State: agent.Perm, PID: 1},
		{Agent: agent.Claude, State: agent.Ask, PID: 1},
		{Agent: agent.Claude, State: agent.Done, PID: 1},
		{Agent: agent.Claude, State: agent.Err, PID: 1},
		{Agent: agent.Devin, State: agent.Busy, PID: 1},
	} {
		if w := ansi.StringWidth(agentCell(r)); w != 1 {
			t.Errorf("agentCell(%+v) is %d cells, want 1", r, w)
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

func TestAgentLetterRendered(t *testing.T) {
	m := New(agentSample(), Options{Tree: true, SelfTab: "8"})
	m.width, m.height = 150, 30
	var claude, devin bool
	for _, r := range m.rows {
		switch ansi.Strip(agentCell(r.agent)) {
		case "C":
			claude = true
		case "D":
			devin = true
		}
	}
	if !claude || !devin {
		t.Errorf("expected both agent letters in the table (claude=%v devin=%v)", claude, devin)
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

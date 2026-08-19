// SPDX-License-Identifier: MIT

package resolve

import (
	"testing"

	"github.com/dsaad68/kaku-tab/internal/agent"
	"github.com/dsaad68/kaku-tab/internal/model"
)

// Two windows in one session: @1 holds a working agent beside a blocked one,
// @2 holds nothing.
type agentFixture struct{ fixture }

func (agentFixture) Windows() ([]model.RawWindow, error) {
	return []model.RawWindow{
		{Session: "api", ID: "@1", Index: "1", Name: "work"},
		{Session: "api", ID: "@2", Index: "2", Name: "idle"},
	}, nil
}

func (agentFixture) Panes() (map[string][]model.Pane, error) {
	return map[string][]model.Pane{
		"@1": {
			{ID: "%1", Index: "1", Cmd: "node", Agent: agent.Record{Agent: agent.Claude, State: agent.Busy, PID: 10, At: 1}},
			{ID: "%2", Index: "2", Cmd: "node", Agent: agent.Record{Agent: agent.Claude, State: agent.Perm, PID: 11, At: 2}},
			{ID: "%3", Index: "3", Cmd: "zsh"},
		},
		"@2": {{ID: "%4", Index: "1", Cmd: "zsh"}},
	}, nil
}

// A window's row reports the pane that most wants you, so one pane blocked on a
// permission prompt is visible even when the others are merely working.
func TestWindowRollsUpMostActionableAgent(t *testing.T) {
	ws, err := Resolve(agentFixture{}, Options{WithAgents: true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]agent.Record{}
	for _, w := range ws {
		got[w.ID] = w.Agent
	}
	if got["@1"].State != agent.Perm || got["@1"].PID != 11 {
		t.Errorf("@1 agent = %+v, want the blocked pane", got["@1"])
	}
	if !got["@2"].Empty() {
		t.Errorf("@2 agent = %+v, want empty", got["@2"])
	}
}

// WithAgents buys the rollup without attaching the panes themselves, so the
// default (non-pane) picker can show an agent column for the price of the one
// list-panes query.
func TestWithAgentsDoesNotAttachPanes(t *testing.T) {
	ws, err := Resolve(agentFixture{}, Options{WithAgents: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range ws {
		if w.Panes_ != nil {
			t.Errorf("%s carried %d panes; WithAgents should only roll up", w.ID, len(w.Panes_))
		}
	}
}

// Without either flag the pane query is skipped entirely, so nothing pays for
// agent awareness it did not ask for.
func TestNoPaneQueryMeansNoAgent(t *testing.T) {
	ws, err := Resolve(agentFixture{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range ws {
		if !w.Agent.Empty() {
			t.Errorf("%s got an agent without WithAgents: %+v", w.ID, w.Agent)
		}
	}
}

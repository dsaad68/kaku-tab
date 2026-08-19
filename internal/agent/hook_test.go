// SPDX-License-Identifier: MIT

package agent

import "testing"

func decide(t *testing.T, payload string) (Action, State) {
	t.Helper()
	return Decide([]byte(payload))
}

func TestDecideClaudeEvents(t *testing.T) {
	cases := []struct {
		payload string
		act     Action
		state   State
	}{
		{`{"hook_event_name":"SessionStart"}`, Set, Busy},
		{`{"hook_event_name":"UserPromptSubmit"}`, Set, Busy},
		{`{"hook_event_name":"PostToolUse"}`, Set, Busy},
		{`{"hook_event_name":"PostToolBatch"}`, Set, Busy},
		{`{"hook_event_name":"Stop"}`, Set, Done},
		{`{"hook_event_name":"StopFailure"}`, Set, Err},
		{`{"hook_event_name":"SessionEnd"}`, Clear, None},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, Set, Perm},
		{`{"hook_event_name":"Notification","notification_type":"elicitation_dialog"}`, Set, Ask},
		{`{"hook_event_name":"Notification","notification_type":"agent_needs_input"}`, Set, Ask},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, Set, Done},
		{`{"hook_event_name":"Notification","notification_type":"agent_completed"}`, Set, Done},
		{`{"hook_event_name":"Notification","notification_type":"elicitation_complete"}`, Set, Busy},
	}
	for _, tc := range cases {
		act, st := decide(t, tc.payload)
		if act != tc.act || st != tc.state {
			t.Errorf("%s -> (%v,%q), want (%v,%q)", tc.payload, act, st, tc.act, tc.state)
		}
	}
}

// Devin CLI reports a pending permission through its own event rather than
// through Claude Code's Notification.
func TestDecideDevinPermissionRequest(t *testing.T) {
	if act, st := decide(t, `{"hook_event_name":"PermissionRequest","tool_name":"exec"}`); act != Set || st != Perm {
		t.Errorf("PermissionRequest -> (%v,%q), want (Set,perm)", act, st)
	}
}

// Both CLIs read one shared hooks block, so each routinely delivers events the
// other does not have. That is normal traffic, not a fault.
func TestDecideIgnoresUnknown(t *testing.T) {
	for _, p := range []string{
		`{"hook_event_name":"PreToolUse"}`,
		`{"hook_event_name":"PreCompact"}`,
		`{"hook_event_name":"PostCompaction"}`,
		`{"hook_event_name":"SubagentStop"}`,
		`{"hook_event_name":"Notification","notification_type":"auth_success"}`,
		`{"hook_event_name":"Notification"}`,
		`{}`,
		`not json at all`,
	} {
		if act, st := decide(t, p); act != Ignore || st != None {
			t.Errorf("%s -> (%v,%q), want Ignore", p, act, st)
		}
	}
}

// A subagent finishing is not the user's turn ending. Without this guard every
// Task call would flash the pane green mid-turn.
func TestDecideIgnoresSubagentPayloads(t *testing.T) {
	for _, p := range []string{
		`{"hook_event_name":"Stop","agent_id":"a1","agent_type":"Explore"}`,
		`{"hook_event_name":"SessionEnd","agent_id":"a1"}`,
		`{"hook_event_name":"Notification","notification_type":"permission_prompt","agent_id":"a1"}`,
	} {
		if act, st := decide(t, p); act != Ignore || st != None {
			t.Errorf("%s -> (%v,%q), want Ignore", p, act, st)
		}
	}
}

func TestDetect(t *testing.T) {
	devin := func(k string) string {
		if k == "DEVIN_PROJECT_DIR" {
			return "/repo"
		}
		return ""
	}
	if got := Detect(devin); got != Devin {
		t.Errorf("Detect with DEVIN_PROJECT_DIR = %q, want devin", got)
	}
	claude := func(k string) string {
		if k == "CLAUDE_PROJECT_DIR" {
			return "/repo"
		}
		return ""
	}
	if got := Detect(claude); got != Claude {
		t.Errorf("Detect with CLAUDE_PROJECT_DIR = %q, want claude", got)
	}
	if got := Detect(func(string) string { return "" }); got != Claude {
		t.Errorf("Detect with nothing set = %q, want claude", got)
	}
}

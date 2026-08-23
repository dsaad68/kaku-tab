// SPDX-License-Identifier: MIT

package agent

import "testing"

func decide(t *testing.T, payload string) (Action, State) {
	t.Helper()
	d := Decide([]byte(payload))
	return d.Action, d.State
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

// The box is only worth having if it says what the agent actually wants, so the
// events that carry text must surrender it.
func TestDecideExtractsMessages(t *testing.T) {
	cases := []struct{ payload, want string }{
		{`{"hook_event_name":"UserPromptSubmit","prompt":"fix the flaky test"}`, "fix the flaky test"},
		{`{"hook_event_name":"Stop","last_assistant_message":"Done - two files changed."}`, "Done - two files changed."},
		{`{"hook_event_name":"StopFailure","error_type":"rate_limit"}`, "rate_limit"},
		{`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"git push"}}`, "Bash: git push"},
		{`{"hook_event_name":"PermissionRequest","tool_name":"Edit","tool_input":{"file_path":"/tmp/x.go"}}`, "Edit: /tmp/x.go"},
		{`{"hook_event_name":"PermissionRequest","tool_name":"Weird","tool_input":{"n":3}}`, "Weird"},
	}
	for _, tc := range cases {
		if got := Decide([]byte(tc.payload)).Msg; got != tc.want {
			t.Errorf("%s\n got %q\nwant %q", tc.payload, got, tc.want)
		}
	}
}

// Events with no text must leave the message alone rather than blanking it, so
// a prompt survives a whole turn of tool calls.
func TestDecideLeavesMessageEmptyWhenNoneCarried(t *testing.T) {
	for _, p := range []string{
		`{"hook_event_name":"PostToolUse","tool_name":"Bash"}`,
		`{"hook_event_name":"PostToolBatch"}`,
		`{"hook_event_name":"SessionStart"}`,
		`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`,
	} {
		if got := Decide([]byte(p)).Msg; got != "" {
			t.Errorf("%s carried a message %q", p, got)
		}
	}
}

// An MCP elicitation is the only event that can say what the question actually
// is; Notification/elicitation_dialog reports only that one is open. The field
// name is not pinned down by the reference, so every plausible one is read.
func TestDecideElicitation(t *testing.T) {
	for _, field := range []string{"message", "question", "description", "prompt"} {
		payload := `{"hook_event_name":"Elicitation","` + field + `":"Which branch?"}`
		d := Decide([]byte(payload))
		if d.Action != Set || d.State != Ask {
			t.Errorf("%s -> (%v,%q), want (Set,ask)", payload, d.Action, d.State)
		}
		if d.Msg != "Which branch?" {
			t.Errorf("%s -> msg %q", payload, d.Msg)
		}
	}
	// Answered or declined, so the agent is working again.
	if d := Decide([]byte(`{"hook_event_name":"ElicitationResult"}`)); d.Action != Set || d.State != Busy {
		t.Errorf("ElicitationResult -> (%v,%q), want (Set,busy)", d.Action, d.State)
	}
}

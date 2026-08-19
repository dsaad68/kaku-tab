// SPDX-License-Identifier: MIT

package agent

import "encoding/json"

// Action is what a hook event asks us to do to the pane's record.
type Action int

const (
	// Ignore: an event we do not track. Most events are this.
	Ignore Action = iota
	// Set: write State to the pane.
	Set
	// Clear: the agent is gone; unset the pane option.
	Clear
)

// payload is the subset of the hook JSON we read. Claude Code and Devin CLI
// share this envelope; the fields we use are common to both.
type payload struct {
	Event    string `json:"hook_event_name"`
	NotifyOn string `json:"notification_type"`
	// AgentID is present only when the event comes from a subagent or an
	// `--agent` run. A subagent finishing is not your turn ending, so any
	// payload carrying this is ignored outright — otherwise every Task call
	// would flash the pane to "done" mid-turn.
	AgentID string `json:"agent_id"`
}

// Decide maps one hook payload to a pane-record action.
//
// Unknown events return Ignore rather than an error: both CLIs are subscribed
// through one shared hooks block, so each of them routinely delivers events the
// other does not have, and that is normal traffic rather than a fault.
func Decide(b []byte) (Action, State) {
	var p payload
	if json.Unmarshal(b, &p) != nil {
		return Ignore, None
	}
	if p.AgentID != "" {
		return Ignore, None
	}

	switch p.Event {
	case "SessionEnd":
		return Clear, None

	case "SessionStart", "UserPromptSubmit", "PostToolUse", "PostToolBatch":
		// The PostTool* events are not just a liveness heartbeat: they are what
		// flips a pane out of Perm once you approve a call and the agent resumes.
		// Without them an approved pane keeps counting as waiting until the turn
		// ends. PreToolUse would do the same job but fires on the agent's hot
		// path, so it is deliberately not subscribed.
		return Set, Busy

	case "Stop":
		return Set, Done

	case "StopFailure":
		return Set, Err

	// Devin's permission-decision event. Claude Code reports the same situation
	// through Notification/permission_prompt below.
	case "PermissionRequest":
		return Set, Perm

	case "Notification":
		switch p.NotifyOn {
		case "permission_prompt":
			return Set, Perm
		case "elicitation_dialog", "agent_needs_input":
			return Set, Ask
		case "idle_prompt", "agent_completed":
			// "done and waiting for your next prompt" — the same meaning as
			// Stop, not a distinct question.
			return Set, Done
		case "elicitation_complete":
			// The form was answered or dismissed; the agent is working again.
			return Set, Busy
		}
		return Ignore, None
	}
	return Ignore, None
}

// Detect names the agent that invoked the hook. Both CLIs read hooks from
// ~/.claude/settings.json, so one block serves both and the agent has to be
// told apart from its environment rather than from an argv flag: each sets its
// own project-directory variable.
func Detect(getenv func(string) string) string {
	if getenv("DEVIN_PROJECT_DIR") != "" {
		return Devin
	}
	return Claude
}

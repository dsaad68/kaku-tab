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

	// The rest is text for the picker's agent box. Which of these is populated
	// depends on the event, and several events carry none of them.
	Prompt    string         `json:"prompt"`                 // UserPromptSubmit
	LastReply string         `json:"last_assistant_message"` // Stop
	ToolName  string         `json:"tool_name"`              // PermissionRequest
	ToolInput map[string]any `json:"tool_input"`             // PermissionRequest
	ErrorType string         `json:"error_type"`             // StopFailure
}

// Decision is what one hook event asks us to do to the pane's record.
type Decision struct {
	Action Action
	State  State
	// Msg is human-readable context for the state — the prompt being worked on,
	// the tool awaiting permission, the reply that ended the turn. Empty for
	// events that carry no text, which includes every Notification: that
	// payload has a type and nothing to read.
	Msg string
}

// Decide maps one hook payload to a pane-record action.
//
// Unknown events return Ignore rather than an error: both CLIs are subscribed
// through one shared hooks block, so each of them routinely delivers events the
// other does not have, and that is normal traffic rather than a fault.
func Decide(b []byte) Decision {
	var p payload
	if json.Unmarshal(b, &p) != nil {
		return Decision{}
	}
	if p.AgentID != "" {
		return Decision{}
	}

	switch p.Event {
	case "SessionEnd":
		return Decision{Action: Clear}

	case "UserPromptSubmit":
		// The prompt is the single most useful line the box can carry: it is
		// what the agent is doing, in the user's own words.
		return Decision{Action: Set, State: Busy, Msg: p.Prompt}

	case "SessionStart", "PostToolUse", "PostToolBatch":
		// The PostTool* events are not just a liveness heartbeat: they are what
		// flips a pane out of Perm once you approve a call and the agent
		// resumes. Without them an approved pane keeps counting as waiting
		// until the turn ends. PreToolUse would do the same job but fires on
		// the agent's hot path, so it is deliberately not subscribed.
		//
		// They carry no text of their own, which is exactly right: the message
		// is tagged with the state it was written for, so the permission
		// request stops being shown the moment the state leaves Perm.
		return Decision{Action: Set, State: Busy}

	case "Stop":
		return Decision{Action: Set, State: Done, Msg: p.LastReply}

	case "StopFailure":
		return Decision{Action: Set, State: Err, Msg: p.ErrorType}

	// Devin's permission-decision event. Claude Code reports the same situation
	// through Notification/permission_prompt below, but only this one says what
	// is actually being asked for.
	case "PermissionRequest":
		return Decision{Action: Set, State: Perm, Msg: toolSummary(p.ToolName, p.ToolInput)}

	case "Notification":
		switch p.NotifyOn {
		case "permission_prompt":
			return Decision{Action: Set, State: Perm}
		case "elicitation_dialog", "agent_needs_input":
			return Decision{Action: Set, State: Ask}
		case "idle_prompt", "agent_completed":
			// "done and waiting for your next prompt" — the same meaning as
			// Stop, not a distinct question.
			return Decision{Action: Set, State: Done}
		case "elicitation_complete":
			// The form was answered or dismissed; the agent is working again.
			return Decision{Action: Set, State: Busy}
		}
		return Decision{}
	}
	return Decision{}
}

// toolArgKeys are the tool_input fields worth showing, most specific first. A
// permission prompt is only useful if it says what is being run, and the field
// that holds it differs per tool.
var toolArgKeys = []string{"command", "file_path", "path", "url", "pattern", "description"}

// toolSummary renders a pending tool call as one line: "Bash: git push origin".
func toolSummary(name string, input map[string]any) string {
	if name == "" {
		return ""
	}
	for _, k := range toolArgKeys {
		if v, ok := input[k].(string); ok && v != "" {
			return name + ": " + v
		}
	}
	return name
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

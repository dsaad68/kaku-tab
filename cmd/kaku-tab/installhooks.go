// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookEvents are the lifecycle events we subscribe to, in the order they are
// written. The two CLIs are subscribed through one shared block and each
// ignores the events it does not have, so this is the union of both:
//
//	Claude Code only  Notification, PostToolBatch, StopFailure, Elicitation
//	Devin CLI only    PermissionRequest
//
// PreToolUse is deliberately absent. It fires on every single tool call, on the
// agent's hot path, and PostToolUse already carries what we need — including the
// transition that clears a pane out of "waiting for permission" once approved.
var hookEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PostToolUse",
	"PostToolBatch",
	"PermissionRequest",
	"Elicitation",
	"ElicitationResult",
	"Notification",
	"Stop",
	"StopFailure",
	"SessionEnd",
}

// notificationMatcher limits the Notification event to the types that actually
// mean something changed for the user. Without it the hook would also fire on
// auth and elicitation-response traffic that says nothing about whether the
// agent is waiting.
const notificationMatcher = "permission_prompt|idle_prompt|agent_needs_input|" +
	"agent_completed|elicitation_dialog|elicitation_complete"

// hookCommand is the shell-form command written into the settings file.
//
// Shell form with an explicit `exec`, rather than Claude Code's exec form
// (`args`), for two reasons. Devin CLI's hook schema has no `args` field, so an
// exec-form entry there would invoke the binary with no arguments at all — and
// kaku-tab with no arguments opens the picker. And `exec` replaces the wrapping
// shell, so the hook's parent is the agent itself, which is the pid the record
// stores to know when the agent has died.
func hookCommand(bin string) string {
	return "exec '" + strings.ReplaceAll(bin, "'", `'\''`) + "' hook"
}

// encode renders the settings file.
//
// Through an Encoder with HTML escaping off, not json.MarshalIndent: the
// default turns every & < > in the user's own prose into a \u0026 escape. It
// still parses, but it silently rewrites text this tool has no business
// touching — and this file holds their permission policy.
func encode(settings map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// ours reports whether a matcher group was written by us, so reinstalling
// replaces it instead of stacking a second copy beside it.
func ours(group any, bin string) bool {
	g, ok := group.(map[string]any)
	if !ok {
		return false
	}
	hs, ok := g["hooks"].([]any)
	if !ok || len(hs) == 0 {
		return false
	}
	for _, h := range hs {
		hm, ok := h.(map[string]any)
		if !ok {
			return false
		}
		cmd, _ := hm["command"].(string)
		if !strings.Contains(cmd, bin) {
			return false
		}
	}
	return true
}

// mergeHooks adds our entries to an existing settings object, leaving every
// other key — and every hook the user wrote themselves — untouched.
func mergeHooks(settings map[string]any, bin string) {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	entry := map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": hookCommand(bin),
		}},
	}

	for _, ev := range hookEvents {
		mine := map[string]any{}
		for k, v := range entry {
			mine[k] = v
		}
		if ev == "Notification" {
			mine["matcher"] = notificationMatcher
		}

		var kept []any
		for _, g := range asSlice(hooks[ev]) {
			if !ours(g, bin) {
				kept = append(kept, g)
			}
		}
		hooks[ev] = append(kept, mine)
	}
	settings["hooks"] = hooks
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// installHooks merges the agent hooks into ~/.claude/settings.json.
//
// Both CLIs read that file — Devin CLI treats it as one of its user-level hook
// sources — so a single block instruments both, and `kaku-tab hook` tells them
// apart from the environment rather than from an argument.
func installHooks(args []string) error {
	dry := false
	for _, a := range args {
		if a == "--dry-run" || a == "-n" {
			dry = true
		}
	}

	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if bin, err = filepath.EvalSymlinks(bin); err != nil {
		return err
	}

	path, err := settingsPath()
	if err != nil {
		return err
	}
	settings := map[string]any{}
	switch data, err := os.ReadFile(path); {
	case err == nil && len(data) > 0:
		if err := json.Unmarshal(data, &settings); err != nil {
			// Refuse rather than clobber: this file holds permissions and
			// auto-mode policy, and rewriting it from a failed parse would
			// throw all of that away.
			return fmt.Errorf("parse %s: %w", path, err)
		}
	case err != nil && !os.IsNotExist(err):
		return err
	}

	mergeHooks(settings, bin)
	out, err := encode(settings)
	if err != nil {
		return err
	}

	if dry {
		fmt.Printf("# would write %s\n%s", path, out)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Keep a copy of whatever was there. Rewriting the file loses key order,
	// and this is the file the user's permission policy lives in.
	if prev, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".kaku-tab.bak", prev, 0o600)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return err
	}
	fmt.Printf("installed agent hooks in %s\n", path)
	fmt.Printf("  command: %s\n", hookCommand(bin))
	fmt.Println("  restart any running Claude Code / Devin CLI session to pick them up")
	return nil
}

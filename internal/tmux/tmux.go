// SPDX-License-Identifier: MIT

// Package tmux wraps the tmux CLI.
//
// Every query uses \x1f as the field separator rather than tab. tmux window
// names are routinely empty (automatic-rename can produce
// ""), and with any whitespace separator an empty field is indistinguishable
// from padding — which silently shifted every later field in the shell version.
// \x1f cannot occur in a session name, window name, path or command.
package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/dsaad68/kaku-tab/internal/model"
)

const FS = "\x1f"

// Run executes a tmux command and returns trimmed stdout.
func Run(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func query(args ...string) ([][]string, error) {
	out, err := Run(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var rows [][]string
	for _, line := range strings.Split(out, "\n") {
		rows = append(rows, strings.Split(line, FS))
	}
	return rows, nil
}

func f(fields ...string) string { return strings.Join(fields, FS) }

func at(r []string, i int) string {
	if i < len(r) {
		return r[i]
	}
	return ""
}

func boolAt(r []string, i int) bool { return at(r, i) == "1" }

func intAt(r []string, i int) int {
	n, _ := strconv.Atoi(at(r, i))
	return n
}

// Clients lists attached tmux clients. WindowID is the window that client's
// session currently displays — a per-session property, so all clients of one
// session report the same value. That is exactly why satellites exist.
func Clients() ([]model.Client, error) {
	rows, err := query("list-clients", "-F", f("#{client_tty}", "#{client_session}", "#{window_id}"))
	if err != nil {
		return nil, err
	}
	cs := make([]model.Client, 0, len(rows))
	for _, r := range rows {
		if at(r, 0) == "" {
			continue
		}
		cs = append(cs, model.Client{TTY: at(r, 0), Session: at(r, 1), WindowID: at(r, 2)})
	}
	return cs, nil
}

// Windows lists every window in every session.
func Windows() ([]model.RawWindow, error) {
	rows, err := query("list-windows", "-a", "-F", f(
		"#{session_name}", "#{window_id}", "#{window_index}", "#{window_name}",
		"#{window_panes}", "#{pane_current_path}", "#{window_activity_flag}",
		"#{window_zoomed_flag}", "#{session_grouped}", "#{session_group}"))
	if err != nil {
		return nil, err
	}
	ws := make([]model.RawWindow, 0, len(rows))
	for _, r := range rows {
		if at(r, 0) == "" {
			continue
		}
		ws = append(ws, model.RawWindow{
			Session: at(r, 0), ID: at(r, 1), Index: at(r, 2), Name: at(r, 3),
			Panes: intAt(r, 4), Path: at(r, 5),
			Activity: boolAt(r, 6), Zoomed: boolAt(r, 7),
			Grouped: boolAt(r, 8), Group: at(r, 9),
		})
	}
	return ws, nil
}

// Panes returns every pane in the server, keyed by tmux window id.
func Panes() (map[string][]model.Pane, error) {
	rows, err := query("list-panes", "-a", "-F", f(
		"#{window_id}", "#{pane_id}", "#{pane_index}",
		"#{pane_current_command}", "#{pane_current_path}", "#{pane_active}"))
	if err != nil {
		return nil, err
	}
	m := make(map[string][]model.Pane)
	for _, r := range rows {
		w := at(r, 0)
		if w == "" {
			continue
		}
		m[w] = append(m[w], model.Pane{
			ID: at(r, 1), Index: at(r, 2), Cmd: at(r, 3),
			Path: at(r, 4), Active: boolAt(r, 5),
		})
	}
	return m, nil
}

// Sessions returns session names mapped to their attached-client count.
func Sessions() (map[string]int, error) {
	rows, err := query("list-sessions", "-F", f("#{session_name}", "#{session_attached}"))
	if err != nil {
		return nil, err
	}
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		if at(r, 0) != "" {
			m[at(r, 0)] = intAt(r, 1)
		}
	}
	return m, nil
}

// Target builds a session-qualified tmux target. Always qualify: a bare "@42"
// is ambiguous once a grouped session shares the window, and tmux resolves it
// to an arbitrary member of the group.
func Target(session, windowID string) string { return session + ":" + windowID }

func SelectWindow(session, windowID string) error {
	_, err := Run("select-window", "-t", Target(session, windowID))
	return err
}

func SelectPane(paneID string) error {
	_, err := Run("select-pane", "-t", paneID)
	return err
}

func KillWindow(session, windowID string) error {
	_, err := Run("kill-window", "-t", Target(session, windowID))
	return err
}

func RenameWindow(session, windowID, name string) error {
	_, err := Run("rename-window", "-t", Target(session, windowID), name)
	return err
}

func RenameSession(old, name string) error {
	_, err := Run("rename-session", "-t", "="+old, name)
	return err
}

func DetachClient(tty string) error {
	_, err := Run("detach-client", "-t", tty)
	return err
}

func KillSession(name string) error {
	_, err := Run("kill-session", "-t", "="+name)
	return err
}

// NewGroupedSession creates a satellite sharing base's window list but owning
// its own current-window.
func NewGroupedSession(base, name string) error {
	_, err := Run("new-session", "-d", "-t", "="+base, "-s", name)
	return err
}

func HasSession(name string) bool {
	_, err := Run("has-session", "-t", "="+name)
	return err == nil
}

// CapturePane returns a pane's visible content with ANSI escapes preserved.
func CapturePane(target string, historyLines int) (string, error) {
	args := []string{"capture-pane", "-p", "-e"}
	if historyLines > 0 {
		args = append(args, "-S", "-"+strconv.Itoa(historyLines))
	}
	args = append(args, "-t", target)
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Option reads a global tmux option, returning def when unset.
func Option(name, def string) string {
	out, err := Run("show-option", "-gqv", name)
	if err != nil || strings.TrimSpace(out) == "" {
		return def
	}
	return strings.TrimSpace(out)
}

// SetOption writes a global tmux option. Used for the small amount of state
// kaku-tab keeps on the server rather than on disk.
func SetOption(name, value string) error {
	_, err := Run("set-option", "-g", name, value)
	return err
}

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

	"github.com/dsaad68/kaku-tab/internal/agent"
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
//
// The agent record rides along in this one query rather than in a second pass:
// @kt_agent is a pane option, so `list-panes` can report it as just another
// format field, and agent awareness costs no extra process.
func Panes() (map[string][]model.Pane, error) {
	rows, err := query("list-panes", "-a", "-F", f(
		"#{window_id}", "#{pane_id}", "#{pane_index}",
		"#{pane_current_command}", "#{pane_current_path}", "#{pane_active}",
		"#{"+agent.PaneOption+"}"))
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
			Agent: liveAgent(at(r, 6)),
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

// liveAgent parses a pane's agent record and drops it if the agent process is
// gone. Pane-scoped storage self-cleans when the pane closes, but an agent
// killed outright inside a surviving pane leaves a record no SessionEnd hook
// will ever clear, so the display must not believe it. `kaku-tab agents` is
// what actually removes it from tmux; see PaneAgents.
func liveAgent(v string) agent.Record {
	r := agent.Parse(v)
	if !agent.Live(r) {
		return agent.Record{}
	}
	return r
}

// PaneAgents returns every pane's agent record verbatim, keyed by pane id, with
// no liveness filtering — the sweeper needs to see dead records in order to
// clear them, which is exactly what Panes() hides.
func PaneAgents() (map[string]agent.Record, error) {
	rows, err := query("list-panes", "-a", "-F", f("#{pane_id}", "#{"+agent.PaneOption+"}"))
	if err != nil {
		return nil, err
	}
	m := make(map[string]agent.Record, len(rows))
	for _, r := range rows {
		if id := at(r, 0); id != "" {
			m[id] = agent.Parse(at(r, 1))
		}
	}
	return m, nil
}

// SetPaneOption writes a pane-scoped option.
//
// -p, always. tmux pane options inherit from window options, so writing
// @kt_agent at window scope would have every agent-free pane in that window
// read back an agent that is not there.
func SetPaneOption(pane, name, value string) error {
	_, err := Run("set-option", "-p", "-t", pane, name, value)
	return err
}

// UnsetPaneOption clears a pane-scoped option.
func UnsetPaneOption(pane, name string) error {
	_, err := Run("set-option", "-p", "-u", "-t", pane, name)
	return err
}

// SetWindowOption writes a window-scoped option. Session-qualified like every
// other target here.
func SetWindowOption(session, windowID, name, value string) error {
	_, err := Run("set-option", "-w", "-t", Target(session, windowID), name, value)
	return err
}

// UnsetWindowOption clears a window-scoped option.
func UnsetWindowOption(session, windowID, name string) error {
	_, err := Run("set-option", "-w", "-u", "-t", Target(session, windowID), name)
	return err
}

// RefreshStatus redraws the status line on every attached client, which is what
// makes the agent counter update the moment a hook fires rather than at the next
// status-interval tick. Every client is named explicitly: a bare refresh-client
// picks one, and this setup routinely has a client per terminal tab.
func RefreshStatus() {
	cs, err := Clients()
	if err != nil {
		return
	}
	for _, c := range cs {
		_, _ = Run("refresh-client", "-S", "-t", c.TTY)
	}
}

// Options reads several global options in one subprocess.
//
// Every tmux.Option is a fork, and the status segment needs a whole palette on
// a timer. display-message expands them all in a single pass, which keeps the
// status bar's per-tick cost at one process rather than one per colour.
func Options(names ...string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "#{E:" + n + "}"
	}
	out, err := Run("display-message", "-p", "-F", f(parts...))
	if err != nil {
		return nil
	}
	vals := strings.Split(out, FS)
	m := make(map[string]string, len(names))
	for i, n := range names {
		if i < len(vals) && vals[i] != "" {
			m[n] = vals[i]
		}
	}
	return m
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

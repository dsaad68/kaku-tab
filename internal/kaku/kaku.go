// SPDX-License-Identifier: MIT

// Package kaku wraps the `kaku cli` multiplexer interface (a re-export of
// WezTerm's mux CLI).
package kaku

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"sync"

	"github.com/dsaad68/kaku-tab/internal/model"
)

var (
	binOnce sync.Once
	binName string
)

// SetBinary pins the mux CLI to use. Call before anything else; an empty name
// leaves auto-detection in place.
func SetBinary(name string) {
	if name == "" {
		return
	}
	binOnce.Do(func() { binName = name })
}

// Binary is the multiplexer CLI to drive.
//
// Kaku is a WezTerm fork and re-exports WezTerm's mux CLI unchanged, so
// `wezterm cli` speaks the same protocol — same `list --format json` with
// tty_name/tab_id, same activate-tab, same spawn. Auto-detecting both means
// this works on either terminal.
func Binary() string {
	binOnce.Do(func() {
		for _, c := range []string{"kaku", "wezterm"} {
			if _, err := exec.LookPath(c); err == nil {
				binName = c
				return
			}
		}
	})
	return binName
}

// Available reports whether a supported mux CLI is on PATH. When it is not,
// the picker still works as a plain tmux window switcher.
func Available() bool { return Binary() != "" }

func run(args ...string) ([]byte, error) {
	bin := Binary()
	if bin == "" {
		return nil, exec.ErrNotFound
	}
	return exec.Command(bin, args...).Output()
}

var (
	cacheOnce sync.Once
	cached    []model.KakuPane
	cachedErr error
)

// Panes lists every Kaku pane. Cached for the process lifetime: a picker
// invocation is short-lived and re-shelling out per keystroke was a large part
// of the shell version's latency.
func Panes() ([]model.KakuPane, error) {
	cacheOnce.Do(func() {
		if !Available() {
			cached = nil
			return
		}
		out, err := run("cli", "list", "--format", "json")
		if err != nil {
			cachedErr = err
			return
		}
		cachedErr = json.Unmarshal(out, &cached)
	})
	return cached, cachedErr
}

// Invalidate drops the pane cache after an action that changes tab layout.
func Invalidate() {
	cacheOnce = sync.Once{}
	cached, cachedErr = nil, nil
}

type client struct {
	FocusedPaneID int `json:"focused_pane_id"`
}

// FocusedPane returns the pane id the GUI currently has focus on.
func FocusedPane() (int, bool) {
	out, err := run("cli", "list-clients", "--format", "json")
	if err != nil {
		return 0, false
	}
	var cs []client
	if json.Unmarshal(out, &cs) != nil || len(cs) == 0 {
		return 0, false
	}
	return cs[0].FocusedPaneID, true
}

// ByTTY finds the Kaku pane hosting a given tty. This is the join that makes
// the whole tool work.
func ByTTY(tty string) (model.KakuPane, bool) {
	ps, err := Panes()
	if err != nil {
		return model.KakuPane{}, false
	}
	for _, p := range ps {
		if p.TTYName == tty {
			return p, true
		}
	}
	return model.KakuPane{}, false
}

// TTYForTab returns the tty of the tmux client living in a Kaku tab.
func TTYForTab(tabID string) (string, bool) {
	id, err := strconv.Atoi(tabID)
	if err != nil {
		return "", false
	}
	ps, err := Panes()
	if err != nil {
		return "", false
	}
	for _, p := range ps {
		if p.TabID == id {
			return p.TTYName, true
		}
	}
	return "", false
}

func ActivateTab(tabID string) error {
	_, err := run("cli", "activate-tab", "--tab-id", tabID)
	return err
}

func SetTabTitle(tabID, title string) error {
	_, err := run("cli", "set-tab-title", "--tab-id", tabID, title)
	return err
}

func KillPane(paneID string) error {
	_, err := run("cli", "kill-pane", "--pane-id", paneID)
	return err
}

// Spawn opens a new Kaku tab running prog. When selfPane is non-empty the tab
// is created in the GUI window holding that pane, so it lands in the window the
// user is actually looking at rather than an arbitrary one.
func Spawn(selfPane string, prog ...string) (string, error) {
	args := []string{"cli", "spawn"}
	if selfPane != "" {
		args = append(args, "--pane-id", selfPane)
	}
	args = append(args, "--")
	args = append(args, prog...)
	out, err := run(args...)
	if err != nil {
		return "", err
	}
	Invalidate()
	return string(out), nil
}

// Raise brings the terminal application forward. The mux CLI can activate a
// tab but has no command to raise a different OS window, so this is the
// fallback for cross-GUI-window jumps. macOS only; a no-op elsewhere.
func Raise() {
	app := "Kaku"
	if Binary() == "wezterm" {
		app = "WezTerm"
	}
	_ = exec.Command("osascript", "-e",
		`tell application "`+app+`" to activate`).Run()
}

// SPDX-License-Identifier: MIT

// Package action carries out what the picker decides: focus a Kaku tab,
// retarget one, or spawn a new one.
package action

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dsaad68/kaku-tab/internal/kaku"
	"github.com/dsaad68/kaku-tab/internal/model"
	"github.com/dsaad68/kaku-tab/internal/tmux"
)

// Mode selects what Enter does for a window that is not already visible.
type Mode int

const (
	// Reuse retargets the session's existing Kaku tab to this window. Default:
	// it keeps the tab count flat.
	Reuse Mode = iota
	// New always opens another Kaku tab, creating a satellite when the session
	// already has one, so two windows of one session can show at once.
	New
)

// Ctx is what the caller knows about where the request came from.
type Ctx struct {
	SelfTTY  string // tty of the invoking tmux client; the Kaku-tab join key
	Suffix   string
	AttachSh string // absolute path to this binary, re-invoked as `attach`
}

func (c Ctx) selfPane() string {
	if c.SelfTTY == "" {
		return ""
	}
	if p, ok := kaku.ByTTY(c.SelfTTY); ok {
		return strconv.Itoa(p.PaneID)
	}
	return ""
}

func (c Ctx) selfGUIWin() string {
	if c.SelfTTY == "" {
		return ""
	}
	if p, ok := kaku.ByTTY(c.SelfTTY); ok {
		return strconv.Itoa(p.WindowID)
	}
	return ""
}

// Go performs the default action for a window: focus it if it is already
// visible, otherwise apply mode.
func Go(w model.Window, paneID string, mode Mode, c Ctx) error {
	if c.Suffix == "" {
		c.Suffix = model.DefaultSatelliteSuffix
	}

	if w.Status == model.Visible {
		focusPane(paneID)
		return activate(w, c)
	}
	// Nothing to reuse when no client is attached anywhere.
	if mode == New || w.Status == model.Detached {
		return openNewTab(w, paneID, c)
	}
	return retarget(w, paneID, c)
}

// retarget switches the tab that already holds this session to this window.
func retarget(w model.Window, paneID string, c Ctx) error {
	// Aim at the CLIENT's session, which may be a satellite. current-window is
	// per-session, so selecting inside the base while the tab holds
	// "api~kaku2" focuses the right tab but leaves it on the wrong window.
	target := w.ClientSession
	if target == "" {
		target = w.Session
	}
	if err := tmux.SelectWindow(target, w.ID); err != nil {
		return err
	}
	focusPane(paneID)
	return activate(w, c)
}

func activate(w model.Window, c Ctx) error {
	if w.TabID == "" || !kaku.Available() {
		// Degraded mode: no Kaku, just move the current tmux client.
		_, err := tmux.Run("switch-client", "-t", tmux.Target(w.Session, w.ID))
		return err
	}
	if err := kaku.ActivateTab(w.TabID); err != nil {
		return err
	}
	// activate-tab selects a tab but cannot raise a different OS window.
	if self := c.selfGUIWin(); self != "" && w.GUIWin != "" && w.GUIWin != self {
		kaku.Raise()
	}
	return nil
}

func openNewTab(w model.Window, paneID string, c Ctx) error {
	sessions, err := tmux.Sessions()
	if err != nil {
		return err
	}

	target := w.Session
	satellite := ""
	if sessions[w.Session] > 0 {
		// The session already has a client. A second Kaku tab attached to the
		// same session would drag the first along, because tmux stores
		// current-window per session. A grouped session shares the window list
		// while owning its own current-window, which is what lets one window
		// map to one tab.
		satellite = model.NextSatellite(w.Session, c.Suffix, func(n string) bool {
			_, taken := sessions[n]
			return taken
		})
		if err := tmux.NewGroupedSession(w.Session, satellite); err != nil {
			return fmt.Errorf("create satellite for %s: %w", w.Session, err)
		}
		target = satellite
	}

	if err := tmux.SelectWindow(target, w.ID); err != nil {
		return err
	}

	if !kaku.Available() {
		_, err := tmux.Run("switch-client", "-t", tmux.Target(target, w.ID))
		focusPane(paneID)
		return err
	}

	self, _ := os.Executable()
	if c.AttachSh != "" {
		self = c.AttachSh
	}
	if _, err := kaku.Spawn(c.selfPane(), self, "attach", target, w.ID, satellite); err != nil {
		return fmt.Errorf("kaku cli spawn: %w", err)
	}
	focusPane(paneID)
	return nil
}

func focusPane(paneID string) {
	if paneID != "" {
		_ = tmux.SelectPane(paneID)
	}
}

// RenameSession renames a base session, carrying its satellites with it.
//
// A satellite is tied to its base purely by name (api~kaku2), so renaming
// the base alone would orphan every satellite from its group: BaseSession()
// would no longer resolve to a real session, and those tabs would stop being
// attributed to it.
func RenameSession(old, newName, suffix string) error {
	if suffix == "" {
		suffix = model.DefaultSatelliteSuffix
	}
	sessions, err := tmux.Sessions()
	if err != nil {
		return err
	}
	for name := range sessions {
		if !model.IsSatellite(name, suffix) || model.BaseSession(name, suffix) != old {
			continue
		}
		idx := strings.TrimPrefix(name, old+suffix)
		_ = tmux.RenameSession(name, newName+suffix+idx)
	}
	return tmux.RenameSession(old, newName)
}

// Kill removes a window and reaps any satellite left behind.
func Kill(w model.Window, suffix string) error {
	if err := tmux.KillWindow(w.Session, w.ID); err != nil {
		return err
	}
	return Prune(suffix)
}

// Detach closes the Kaku tab showing a window by detaching its tmux client.
func Detach(w model.Window, suffix string) error {
	if w.Status != model.Visible || w.TabID == "" {
		return nil
	}
	tty, ok := kaku.TTYForTab(w.TabID)
	if !ok {
		return nil
	}
	if err := tmux.DetachClient(tty); err != nil {
		return err
	}
	return Prune(suffix)
}

// Prune reaps satellite sessions with no client.
//
// destroy-unattached is deliberately not used: a satellite is briefly detached
// between creation and attach, so that option can destroy it out from under us.
// Pruning on demand has no such race.
func Prune(suffix string) error {
	if suffix == "" {
		suffix = model.DefaultSatelliteSuffix
	}
	sessions, err := tmux.Sessions()
	if err != nil {
		return err
	}
	for name, attached := range sessions {
		if attached == 0 && model.IsSatellite(name, suffix) {
			_ = tmux.KillSession(name)
		}
	}
	return nil
}

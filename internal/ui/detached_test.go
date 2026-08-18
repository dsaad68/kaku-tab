// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dsaad68/kaku-tab/internal/model"
)

func toggleDetached(m *Model) {
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
}

// sample() has "termdown" detached and "api" attached.
func TestHideDetachedDropsWholeSessions(t *testing.T) {
	m := New(sample(), Options{Tree: true, SelfTab: "8", HideDetached: true})

	if got := sessions(m); strings.Join(got, ",") != "api" {
		t.Errorf("got %v, want only the attached session", got)
	}
	for _, r := range m.rows {
		if r.status == model.Detached {
			t.Errorf("detached row survived: %s", r.search)
		}
	}
}

// The header would be left behind pointing at nothing.
func TestHideDetachedRemovesTheHeaderToo(t *testing.T) {
	m := New(sample(), Options{Tree: true, SelfTab: "8", HideDetached: true})
	for _, r := range m.rows {
		if r.kind == kindHeader && r.group == "termdown" {
			t.Error("header for a fully hidden session is still listed")
		}
	}
}

func TestToggleDetachedIsReversible(t *testing.T) {
	m := New(sample(), Options{Tree: true, SelfTab: "8"})
	before := len(m.rows)

	toggleDetached(m)
	if len(m.rows) >= before {
		t.Errorf("hiding changed nothing: %d -> %d rows", before, len(m.rows))
	}
	if !m.opt.HideDetached {
		t.Error("flag not set")
	}

	toggleDetached(m)
	if len(m.rows) != before {
		t.Errorf("unhiding did not restore the list: %d -> %d rows", before, len(m.rows))
	}
}

// Unhiding inserts whole sessions, which can be above the cursor. Without
// holding the row, the selection lands on something unrelated and Enter goes
// somewhere the user did not choose.
func TestToggleDetachedKeepsTheCursorOnItsRow(t *testing.T) {
	m := New(sample(), Options{Tree: true, SelfTab: "8", HideDetached: true})
	m.width, m.height = 150, 30

	// Land on a window row of "api", the one session that survives hiding.
	for i, vi := range m.view {
		if m.rows[vi].kind == kindWindow {
			m.cursor = i
			break
		}
	}
	want, _ := m.current()

	toggleDetached(m)

	got, ok := m.current()
	if !ok {
		t.Fatal("cursor left the view")
	}
	if got.win.ID != want.win.ID {
		t.Errorf("cursor moved from window %s to %s", want.win.ID, got.win.ID)
	}
}

// "no matches" reads as "you have no sessions" when the real cause is a filter
// the user may have forgotten they turned on.
func TestEmptyListExplainsTheFilter(t *testing.T) {
	ws := []model.Window{
		{RawWindow: model.RawWindow{Session: "api", ID: "@1", Index: "1"}, Status: model.Detached},
	}
	m := New(ws, Options{Tree: true, HideDetached: true})
	m.width, m.height = 150, 30

	if got := m.View(); !strings.Contains(got, "^e shows detached") {
		t.Error("empty list does not say the filter is why")
	}
}

func TestHelpBarNamesTheDirectionOfTheToggle(t *testing.T) {
	shown := New(sample(), Options{Tree: true})
	if !hasPair(shown.helpPairs(), "^e", "hide detached") {
		t.Errorf("with detached shown, help says %v", shown.helpPairs())
	}

	hidden := New(sample(), Options{Tree: true, HideDetached: true})
	if !hasPair(hidden.helpPairs(), "^e", "show detached") {
		t.Errorf("with detached hidden, help says %v", hidden.helpPairs())
	}
}

func hasPair(pairs [][2]string, key, label string) bool {
	for _, p := range pairs {
		if p[0] == key && p[1] == label {
			return true
		}
	}
	return false
}

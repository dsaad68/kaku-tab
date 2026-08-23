// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dsaad68/kaku-tab/internal/model"
)

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestCursorMovementStaysInRange(t *testing.T) {
	m := newTestModel(t)
	n := len(m.view)
	if n < 2 {
		t.Fatalf("sample gives %d visible rows, need at least 2", n)
	}

	for i := 0; i < n+5; i++ {
		m.Update(key(tea.KeyDown))
	}
	if m.cursor != n-1 {
		t.Errorf("cursor ran to %d past the end of %d rows", m.cursor, n)
	}
	for i := 0; i < n+5; i++ {
		m.Update(key(tea.KeyUp))
	}
	if m.cursor != 0 {
		t.Errorf("cursor ran to %d past the start", m.cursor)
	}

	m.Update(key(tea.KeyEnd))
	if m.cursor != n-1 {
		t.Errorf("end put the cursor at %d, want %d", m.cursor, n-1)
	}
	m.Update(key(tea.KeyHome))
	if m.cursor != 0 {
		t.Errorf("home put the cursor at %d, want 0", m.cursor)
	}
}

// Esc and ctrl+c leave without choosing anything: the caller must not act on a
// cancelled picker.
func TestEscapeCancelsWithoutChoosing(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC} {
		m := newTestModel(t)
		m.Update(key(k))
		if !m.quitting {
			t.Errorf("%v did not quit", k)
		}
		if m.Result().Chosen {
			t.Errorf("%v reported a choice", k)
		}
	}
}

func TestTypingFiltersAndCtrlUClears(t *testing.T) {
	m := newTestModel(t)
	all := len(m.view)

	typeText(m, "termdown")
	if len(m.view) == 0 {
		t.Fatal("query matched nothing at all")
	}
	if len(m.view) >= all {
		t.Errorf("query did not narrow the list: %d of %d rows", len(m.view), all)
	}

	m.Update(key(tea.KeyCtrlU))
	if m.query != "" {
		t.Errorf("ctrl+u left query %q", m.query)
	}
	if len(m.view) != all {
		t.Errorf("clearing restored %d rows, want %d", len(m.view), all)
	}
}

// Regression: the query was sliced by byte. A single backspace over a
// multi-byte character left half a rune behind — and this picker is aimed at
// nerd-font and CJK window names, where that is the common case, not the edge.
func TestBackspaceDeletesWholeRunes(t *testing.T) {
	for _, s := range []string{"é", "日本", "󰚩", "aé"} {
		m := newTestModel(t)
		typeText(m, s)
		if m.query != s {
			t.Fatalf("typing %q produced %q", s, m.query)
		}
		m.Update(key(tea.KeyBackspace))

		if !utf8.ValidString(m.query) {
			t.Errorf("backspacing %q left invalid UTF-8: %q", s, m.query)
		}
		want := string([]rune(s)[:len([]rune(s))-1])
		if m.query != want {
			t.Errorf("backspacing %q gave %q, want %q", s, m.query, want)
		}
	}
}

func TestBackspaceOnEmptyQueryIsSafe(t *testing.T) {
	m := newTestModel(t)
	m.Update(key(tea.KeyBackspace))
	if m.query != "" {
		t.Errorf("query became %q", m.query)
	}
}

// Same slicing bug, same fix, in the rename buffer — which is prefilled with a
// window name, exactly the place a nerd-font glyph shows up.
func TestRenameBackspaceDeletesWholeRunes(t *testing.T) {
	m := newTestModel(t)
	m.renaming, m.rename = true, "󰚩 claude"
	m.Update(key(tea.KeyBackspace))
	if !utf8.ValidString(m.rename) {
		t.Errorf("rename buffer is invalid UTF-8: %q", m.rename)
	}
	if want := "󰚩 claud"; m.rename != want {
		t.Errorf("rename = %q, want %q", m.rename, want)
	}

	m.rename = "󰚩"
	m.Update(key(tea.KeyBackspace))
	if m.rename != "" {
		t.Errorf("deleting the only rune left %q", m.rename)
	}
}

// The rename field opens prefilled, so clearing it has to be cheap.
func TestRenameEditingKeys(t *testing.T) {
	m := newTestModel(t)
	m.renaming, m.rename = true, "api server"

	m.Update(key(tea.KeyCtrlW))
	if want := "api"; m.rename != want {
		t.Errorf("ctrl+w gave %q, want %q", m.rename, want)
	}
	m.Update(key(tea.KeyCtrlU))
	if m.rename != "" {
		t.Errorf("ctrl+u gave %q", m.rename)
	}

	// Escape abandons the rename rather than applying an empty name.
	m.rename = "half-typed"
	m.Update(key(tea.KeyEsc))
	if m.renaming || m.rename != "" {
		t.Errorf("esc left renaming=%v rename=%q", m.renaming, m.rename)
	}
}

// While renaming, ordinary keys edit the buffer instead of driving the list.
func TestRenameSwallowsNavigation(t *testing.T) {
	m := newTestModel(t)
	before := m.cursor
	m.renaming, m.rename = true, ""
	m.Update(key(tea.KeyDown))
	if m.cursor != before {
		t.Errorf("down moved the cursor to %d during a rename", m.cursor)
	}
}

func TestTabFoldsAndUnfoldsASession(t *testing.T) {
	m := newTestModel(t)
	all := len(m.view)

	// Land on a header, then fold it.
	for i, vi := range m.view {
		if m.rows[vi].kind == kindHeader {
			m.cursor = i
			break
		}
	}
	r, _ := m.current()
	group := r.group

	m.Update(key(tea.KeyTab))
	if !m.collapse[group] {
		t.Fatalf("tab did not fold %q", group)
	}
	if len(m.view) >= all {
		t.Errorf("folding %q left %d rows of %d", group, len(m.view), all)
	}

	m.Update(key(tea.KeyTab))
	if m.collapse[group] {
		t.Errorf("tab did not unfold %q", group)
	}
	if len(m.view) != all {
		t.Errorf("unfolding restored %d rows, want %d", len(m.view), all)
	}
}

// Tab on a child folds the group it belongs to and lands on its header, rather
// than doing nothing.
func TestTabOnAChildFoldsItsGroup(t *testing.T) {
	m := newTestModel(t)
	for i, vi := range m.view {
		if m.rows[vi].kind != kindHeader {
			m.cursor = i
			break
		}
	}
	child, _ := m.current()
	m.Update(key(tea.KeyTab))

	if !m.collapse[child.group] {
		t.Fatalf("group %q not folded", child.group)
	}
	now, ok := m.current()
	if !ok || now.kind != kindHeader || now.group != child.group {
		t.Errorf("cursor landed on %+v, want the header for %q", now, child.group)
	}
}

func TestShiftTabFoldsThenUnfoldsEverything(t *testing.T) {
	m := newTestModel(t)
	m.Update(key(tea.KeyShiftTab))
	for _, r := range m.rows {
		if r.kind == kindHeader && !m.collapse[r.group] {
			t.Fatalf("%q left open", r.group)
		}
	}
	m.Update(key(tea.KeyShiftTab))
	for _, r := range m.rows {
		if r.kind == kindHeader && m.collapse[r.group] {
			t.Errorf("%q left folded", r.group)
		}
	}
}

// A detached session has no terminal tab, so hiding them leaves exactly what
// you can switch to right now.
func TestCtrlEHidesDetachedSessions(t *testing.T) {
	m := newTestModel(t)
	m.Update(key(tea.KeyCtrlE))
	if !m.opt.HideDetached {
		t.Fatal("ctrl+e did not set HideDetached")
	}
	for _, r := range m.rows {
		if r.kind != kindHeader && r.win.Status == model.Detached {
			t.Errorf("detached window %s survived", r.win.ID)
		}
	}

	m.Update(key(tea.KeyCtrlE))
	if m.opt.HideDetached {
		t.Error("ctrl+e did not toggle back")
	}
	var sawDetached bool
	for _, r := range m.rows {
		if r.kind != kindHeader && r.win.Status == model.Detached {
			sawDetached = true
		}
	}
	if !sawDetached {
		t.Error("detached windows did not come back")
	}
}

// The filter can empty the list, which reads as a broken picker unless it says
// why.
func TestCtrlAExplainsAnEmptyResult(t *testing.T) {
	m := newTestModel(t)
	m.Update(key(tea.KeyCtrlA))
	if !m.opt.AgentsOnly {
		t.Fatal("ctrl+a did not set AgentsOnly")
	}
	if len(m.view) != 0 {
		t.Fatalf("sample has no agents, expected an empty list, got %d rows", len(m.view))
	}
	if !strings.Contains(m.status, "no agent") {
		t.Errorf("status = %q, want an explanation", m.status)
	}

	// Toggling back must clear the message, or it stands over the full list.
	m.Update(key(tea.KeyCtrlA))
	if m.status != "" {
		t.Errorf("status %q survived the toggle back", m.status)
	}
	if len(m.view) == 0 {
		t.Error("list did not come back")
	}
}

// State has to survive a preview-toggle relaunch, which closes and reopens the
// popup: tmux cannot resize one in place.
func TestStateRoundTripsThroughARelaunch(t *testing.T) {
	m := newTestModel(t)
	typeText(m, "api")
	m.Update(key(tea.KeyCtrlE))
	for i, vi := range m.view {
		if m.rows[vi].kind == kindHeader {
			m.cursor = i
			m.Update(key(tea.KeyTab))
			break
		}
	}

	st := m.State()
	restored := New(sample(), Options{Tree: true, SelfTab: "8", Restore: st})
	if restored.query != st.Query {
		t.Errorf("query %q, want %q", restored.query, st.Query)
	}
	if restored.cursor != st.Cursor {
		t.Errorf("cursor %d, want %d", restored.cursor, st.Cursor)
	}
	for g, want := range st.Collapse {
		if restored.collapse[g] != want {
			t.Errorf("fold state for %q = %v, want %v", g, restored.collapse[g], want)
		}
	}
}

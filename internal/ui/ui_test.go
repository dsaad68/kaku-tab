// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/dsaad68/kaku-tab/internal/model"
)

func sample() []model.Window {
	return []model.Window{
		{RawWindow: model.RawWindow{Session: "api", ID: "@1", Index: "1", Name: "claude", Panes: 2, Path: "/home/u/api"},
			Status: model.AttachedHidden, TabID: "8", GUIWin: "0", ClientSession: "api"},
		{RawWindow: model.RawWindow{Session: "api", ID: "@2", Index: "2", Name: "just", Panes: 2, Path: "/home/u/api/cmd", Activity: true},
			Status: model.Visible, TabID: "8", GUIWin: "0", ClientSession: "api"},
		{RawWindow: model.RawWindow{Session: "termdown", ID: "@3", Index: "1", Name: "", Panes: 1, Path: "/home/u/td"},
			Status: model.Detached},
	}
}

func newTestModel(t *testing.T) *Model {
	t.Helper()
	m := New(sample(), Options{Tree: true, Preview: true, SelfTab: "8"})
	m.width, m.height = 150, 30
	return m
}

// Regression: the row cursor is styled when selected, and measuring that styled
// string with a rune-width function counts the ANSI escape bytes as printable.
// That shrank the column budget for the selected row only, so every column
// jumped left as the cursor moved down the list.
func TestSelectedRowKeepsColumnAlignment(t *testing.T) {
	m := newTestModel(t)
	for i, r := range m.rows {
		off := m.renderRow(r, false)
		on := m.renderRow(r, true)

		if wOff, wOn := ansi.StringWidth(off), ansi.StringWidth(on); wOff != wOn {
			t.Errorf("row %d (%s): width selected=%d unselected=%d", i, r.group, wOn, wOff)
		}
		// The badge is the column that matters most; pin its position too.
		if a, b := badgeCol(off), badgeCol(on); a != b {
			t.Errorf("row %d (%s): badge column moved %d -> %d when selected", i, r.group, a, b)
		}
	}
}

// Display column of the badge, not the byte offset: "▸" is three bytes and a
// space is one, so strings.Index alone reports a phantom shift on rows that
// are perfectly aligned on screen.
func badgeCol(s string) int {
	plain := ansi.Strip(s)
	i := strings.Index(plain, "⟦")
	if i < 0 {
		return -1
	}
	return runewidth.StringWidth(plain[:i])
}

func TestRowsNeverExceedListWidth(t *testing.T) {
	m := newTestModel(t)
	for _, r := range m.rows {
		for _, sel := range []bool{false, true} {
			if got := ansi.StringWidth(m.renderRow(r, sel)); got > m.listWidth() {
				t.Errorf("row %q width %d exceeds list width %d", r.group, got, m.listWidth())
			}
		}
	}
}

// A header stands in for its session, so folding must work from anywhere in the
// group — pressing it on a child used to do nothing at all.
func TestFoldFromChildCollapsesItsGroup(t *testing.T) {
	m := newTestModel(t)
	before := len(m.view)

	// Put the cursor on the first child of "api".
	idx := -1
	for i, vi := range m.view {
		if m.rows[vi].kind != kindHeader && m.rows[vi].group == "api" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no child row found")
	}
	m.cursor = idx

	m.collapse["api"] = true
	m.refilter()

	if len(m.view) >= before {
		t.Errorf("collapsing hid nothing: %d -> %d rows", before, len(m.view))
	}
	for _, vi := range m.view {
		if m.rows[vi].kind != kindHeader && m.rows[vi].group == "api" {
			t.Error("collapsed group still shows children")
		}
	}
}

// Collapse used to be honoured only when no query was active, so during a
// search the ▾/▸ arrow flipped while the children stayed on screen.
func TestCollapseAppliesDuringSearch(t *testing.T) {
	m := newTestModel(t)
	m.collapse["api"] = true
	m.query = "api"
	m.refilter()

	sawHeader := false
	for _, vi := range m.view {
		r := m.rows[vi]
		if r.group != "api" {
			continue
		}
		if r.kind == kindHeader {
			sawHeader = true
			continue
		}
		t.Error("collapsed group leaked a child while searching")
	}
	if !sawHeader {
		t.Error("collapsed group should still show its header when it matches")
	}
}

// A header matching on behalf of its children is what lets child rows omit the
// session name entirely — the compromise the fzf version could not avoid.
func TestHeaderMatchRevealsChildren(t *testing.T) {
	m := newTestModel(t)
	m.query = "api"
	m.refilter()

	children := 0
	for _, vi := range m.view {
		if m.rows[vi].group == "api" && m.rows[vi].kind != kindHeader {
			children++
		}
	}
	if children != 2 {
		t.Errorf("header match revealed %d children, want 2", children)
	}
}

func TestEmptyWindowNameRenders(t *testing.T) {
	m := newTestModel(t)
	for _, r := range m.rows {
		if r.kind != kindHeader && r.win.Name == "" {
			if got := ansi.StringWidth(m.renderRow(r, false)); got == 0 {
				t.Error("row with empty window name rendered nothing")
			}
			return
		}
	}
	t.Fatal("fixture lost its empty-name window")
}

// Regression: renderRow reserved a flat 15 cells for the badge, but
// "⟦hidden 10⟧ ← here" is 18, so the marker clipped to "←…" on exactly the row
// telling you where you already are. The reserve is now measured.
func TestBadgeAndHereMarkerNotClipped(t *testing.T) {
	ws := sample()
	ws[1].TabID = "10" // widest realistic badge
	m := New(ws, Options{Tree: true, Preview: true, SelfTab: "10"})
	m.width, m.height = 150, 30

	found := false
	for _, r := range m.rows {
		if r.kind == kindHeader || r.status != model.Visible {
			continue
		}
		found = true
		for _, sel := range []bool{false, true} {
			got := ansi.Strip(m.renderRow(r, sel))
			if !strings.Contains(got, "⟦kaku 10⟧") {
				t.Errorf("selected=%v: badge missing or clipped in %q", sel, got)
			}
			if !strings.Contains(got, "<- here") {
				t.Errorf("selected=%v: here-marker clipped in %q", sel, got)
			}
		}
	}
	if !found {
		t.Fatal("fixture lost its visible window")
	}
}

// Every shortcut must be reachable on screen; the compact popup is narrow
// enough that a single-line bar silently dropped half of them.
func TestHelpWrapsInsteadOfDroppingKeys(t *testing.T) {
	m := newTestModel(t)
	m.width = 100 // compact popup
	lines := m.helpLines()
	joined := ansi.Strip(strings.Join(lines, " "))
	for _, key := range []string{"enter", "^/", "^t", "tab", "^p", "^r", "^x", "^d", "^u"} {
		if !strings.Contains(joined, key) {
			t.Errorf("help omits %q (lines=%d): %s", key, len(lines), joined)
		}
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.innerW()-2 {
			t.Errorf("help line %d width %d exceeds %d", i, w, m.innerW()-2)
		}
	}
}

// Regression: column widths were derived from each row's OWN badge, so a row
// carrying "⟦kaku 7⟧ ← here" squeezed its columns while "⟦kaku 8⟧" did not, and
// the whole table went ragged. Every child row must lay out identically.
func TestColumnsAlignAcrossRows(t *testing.T) {
	ws := sample()
	ws[0].TabID, ws[0].Status = "8", model.AttachedHidden // ⟦hidden 8⟧
	ws[1].TabID, ws[1].Status = "10", model.Visible       // ⟦kaku 10⟧ ← here
	m := New(ws, Options{Tree: true, Preview: true, SelfTab: "10"})
	m.width, m.height = 150, 30

	// Badges are right-aligned in a fixed-width column, so every child row ends
	// at the same place and the columns before it line up.
	want := -1
	for _, r := range m.rows {
		if r.kind == kindHeader {
			continue
		}
		plain := ansi.Strip(m.renderRow(r, false))
		end := runewidth.StringWidth(plain)
		if want < 0 {
			want = end
			continue
		}
		if end != want {
			t.Errorf("row ends at column %d, want %d — %q", end, want, plain)
		}
	}
	if want < 0 {
		t.Fatal("no child rows rendered")
	}
}

// The badge column must clear the frame border, not butt against it.
func TestRightMarginPresent(t *testing.T) {
	m := newTestModel(t)
	for _, r := range m.rows {
		if r.kind == kindHeader {
			continue
		}
		line := ansi.Strip(m.renderRow(r, false))
		if w := runewidth.StringWidth(line); w > m.listWidth()-rightMargin {
			t.Errorf("row reaches column %d, leaving no margin before %d: %q",
				w, m.listWidth(), line)
		}
	}
}

// Sessions you can actually switch between should lead; detached ones sink.
func TestAttachedSessionsSortFirst(t *testing.T) {
	ws := []model.Window{
		{RawWindow: model.RawWindow{Session: "aaa-detached", ID: "@1", Index: "1"}, Status: model.Detached},
		{RawWindow: model.RawWindow{Session: "zzz-attached", ID: "@2", Index: "1"}, Status: model.Visible, TabID: "3"},
		{RawWindow: model.RawWindow{Session: "mmm-hidden", ID: "@3", Index: "1"}, Status: model.AttachedHidden, TabID: "4"},
		{RawWindow: model.RawWindow{Session: "bbb-detached", ID: "@4", Index: "1"}, Status: model.Detached},
	}
	m := New(ws, Options{Tree: true})
	m.width, m.height = 150, 30

	var groups []string
	for _, vi := range m.view {
		if m.rows[vi].kind == kindHeader {
			groups = append(groups, m.rows[vi].group)
		}
	}
	want := []string{"mmm-hidden", "zzz-attached", "aaa-detached", "bbb-detached"}
	for i := range want {
		if i >= len(groups) || groups[i] != want[i] {
			t.Fatalf("group order = %v, want %v", groups, want)
		}
	}
}

// Regression: the footer indent was applied to the JOINED string, so only the
// first line got it and a wrapped footer came out ragged.
func TestFooterLinesIndentEqually(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 90, 24 // narrow enough that the help wraps

	var leads []int
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		i := strings.Index(l, "│")
		if i < 0 {
			continue
		}
		body := l[i+len("│"):]
		if !strings.Contains(body, "switch") && !strings.Contains(body, "rename") {
			continue
		}
		leads = append(leads, len(body)-len(strings.TrimLeft(body, " ")))
	}
	if len(leads) < 2 {
		t.Fatalf("footer did not wrap; got %d lines", len(leads))
	}
	for _, l := range leads[1:] {
		if l != leads[0] {
			t.Errorf("footer indents differ: %v", leads)
		}
	}
	if leads[0] != len(footerPad) {
		t.Errorf("footer indent = %d, want %d", leads[0], len(footerPad))
	}
}

// A blank line separates the footer from the list.
func TestBlankLineAboveFooter(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 90, 24
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, l := range lines {
		if strings.Contains(l, "enter switch") {
			if i == 0 {
				t.Fatal("footer is the first line")
			}
			prev := lines[i-1]
			if j := strings.Index(prev, "│"); j >= 0 {
				if strings.TrimSpace(strings.Trim(prev[j:], "│")) != "" {
					t.Errorf("no blank line above footer, got %q", prev)
				}
			}
			return
		}
	}
	t.Fatal("footer not found")
}

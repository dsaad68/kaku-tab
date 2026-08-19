// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dsaad68/kaku-tab/internal/agent"
	"github.com/dsaad68/kaku-tab/internal/model"
)

// Two sessions of one window each, nothing multi-pane, nothing flagged — the
// shape that used to render six rows of which three were duplicates, in a table
// whose columns were mostly padding.
func flatSample() []model.Window {
	return []model.Window{
		{RawWindow: model.RawWindow{Session: "api", ID: "@1", Index: "1", Name: "zsh", Panes: 1, Path: "/home/u/api"},
			Status: model.Visible, TabID: "5", ClientSession: "api"},
		{RawWindow: model.RawWindow{Session: "kaku-tab", ID: "@2", Index: "1", Name: "claude", Panes: 1, Path: "/home/u/kaku-tab"},
			Status: model.Visible, TabID: "6", ClientSession: "kaku-tab"},
	}
}

func modelAt(t *testing.T, ws []model.Window, opt Options, w, h int) *Model {
	t.Helper()
	m := New(ws, opt)
	m.width, m.height = w, h
	m.relayout()
	return m
}

// A session with one window needs no header: there is nothing to group and
// nothing to fold, and the header only repeated the row beneath it.
func TestMergeSingleDropsRedundantHeaders(t *testing.T) {
	m := modelAt(t, flatSample(), Options{Tree: true, MergeSingle: true}, 150, 30)
	if len(m.rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one per session)", len(m.rows))
	}
	for _, r := range m.rows {
		if r.kind == kindHeader {
			t.Errorf("header survived for a one-window session: %+v", r.group)
		}
		if !r.merged {
			t.Errorf("row %s not marked merged", r.group)
		}
	}

	// Off, the tree is unchanged: a header plus its child for each session.
	m = modelAt(t, flatSample(), Options{Tree: true}, 150, 30)
	if len(m.rows) != 4 {
		t.Fatalf("with MergeSingle off got %d rows, want 4", len(m.rows))
	}
}

// A merged row carries the session name, since it stands in for the header that
// would have shown it.
func TestMergedRowShowsSessionName(t *testing.T) {
	m := modelAt(t, flatSample(), Options{Tree: true, MergeSingle: true}, 150, 30)
	var got []string
	for _, r := range m.rows {
		got = append(got, m.rowLabel(r))
	}
	want := "api kaku-tab"
	if strings.Join(got, " ") != want {
		t.Errorf("merged labels = %q, want %q", strings.Join(got, " "), want)
	}
}

// Merging only applies to a group of one. A session with two windows keeps its
// header, because there is now something to fold.
func TestMergeSingleLeavesRealGroupsAlone(t *testing.T) {
	ws := append(flatSample(), model.Window{
		RawWindow: model.RawWindow{Session: "api", ID: "@3", Index: "2", Name: "vim", Panes: 1, Path: "/home/u/api"},
		Status:    model.Visible, TabID: "5", ClientSession: "api"})
	m := modelAt(t, ws, Options{Tree: true, MergeSingle: true}, 150, 30)

	headers, merged := 0, 0
	for _, r := range m.rows {
		if r.kind == kindHeader {
			headers++
			if r.group != "api" {
				t.Errorf("unexpected header for %q", r.group)
			}
		}
		if r.merged {
			merged++
		}
	}
	if headers != 1 || merged != 1 {
		t.Errorf("headers=%d merged=%d, want 1 and 1", headers, merged)
	}
}

// A column whose every cell reads the same carries no information. The pane
// count is "1p" on every row of a table with no split windows, and the flags
// column is blank when nothing is flagged.
func TestConstantColumnsAreDropped(t *testing.T) {
	m := modelAt(t, flatSample(), Options{Tree: true, MergeSingle: true}, 150, 30)
	if m.lay.panes != 0 {
		t.Errorf("pane-count column drawn (%d) with no multi-pane window", m.lay.panes)
	}
	if m.lay.flags != 0 {
		t.Errorf("flags column drawn (%d) with nothing flagged", m.lay.flags)
	}
	for _, r := range m.rows {
		if strings.Contains(ansi.Strip(m.renderRow(r, false)), "1p") {
			t.Errorf("row still renders a pane count: %q", ansi.Strip(m.renderRow(r, false)))
		}
	}

	// One split window is enough to bring the column back for the whole table.
	ws := flatSample()
	ws[0].Panes = 3
	ws[0].Activity = true
	m = modelAt(t, ws, Options{Tree: true, MergeSingle: true}, 150, 30)
	if m.lay.panes != paneCountCells {
		t.Errorf("pane-count column missing (%d) with a 3-pane window", m.lay.panes)
	}
	if m.lay.flags != flagCells {
		t.Errorf("flags column missing (%d) with an activity flag", m.lay.flags)
	}
}

// Columns are sized to what is in them, not to a proportion of the frame. The
// old fixed split spent 22% of the width on a column holding "1".
func TestColumnsSizedToContent(t *testing.T) {
	m := modelAt(t, flatSample(), Options{Tree: true, MergeSingle: true}, 150, 30)
	if want := len("kaku-tab"); m.lay.label != want {
		t.Errorf("label column = %d, want %d (its widest value)", m.lay.label, want)
	}
	if want := len("claude"); m.lay.name != want {
		t.Errorf("name column = %d, want %d", m.lay.name, want)
	}
	// And the row is nowhere near the frame width it used to be padded out to.
	for _, r := range m.rows {
		if w := ansi.StringWidth(m.renderRow(r, false)); w > m.rowWidth()/2 {
			t.Errorf("row is %d cells of a %d-cell frame; columns are not fitted",
				w, m.rowWidth())
		}
	}
}

// An outlier must not squeeze the other columns out of existence.
func TestColumnCapsBindOnOutliers(t *testing.T) {
	ws := flatSample()
	ws[0].Name = strings.Repeat("x", 400)
	ws[0].Path = "/" + strings.Repeat("y", 400)
	m := modelAt(t, ws, Options{Tree: true, MergeSingle: true}, 100, 30)
	for _, r := range m.rows {
		line := m.renderRow(r, false)
		if w := ansi.StringWidth(line); w > m.rowWidth() {
			t.Errorf("row %d cells > rowWidth %d", w, m.rowWidth())
		}
		if !strings.Contains(ansi.Strip(line), "⟦") {
			t.Errorf("badge truncated away by an outlier column: %q", ansi.Strip(line))
		}
	}
}

// Measure is what lets the popup open at the size it needs: tmux fixes a
// popup's geometry at creation and cannot resize it afterwards.
func TestMeasureFitsContent(t *testing.T) {
	opt := Options{Tree: true, MergeSingle: true}
	cols, rows := Measure(flatSample(), opt)

	if cols < minPopupCols {
		t.Errorf("cols = %d, below the floor %d", cols, minPopupCols)
	}
	if cols > 200 {
		t.Errorf("cols = %d for two short rows; not fitted", cols)
	}
	// Two rows plus frame, prompt, rule, blank and a footer that wraps at this
	// width — comfortably under a screenful either way.
	if rows < 8 || rows > 16 {
		t.Errorf("rows = %d for a two-row list, want a snug fit", rows)
	}

	// More windows must ask for more rows, or nothing is being measured.
	big := flatSample()
	for i := 0; i < 20; i++ {
		big = append(big, model.Window{RawWindow: model.RawWindow{
			Session: "api", ID: "@x", Index: "9", Name: "w", Panes: 1, Path: "/home/u"}})
	}
	_, bigRows := Measure(big, opt)
	if bigRows <= rows {
		t.Errorf("20 more windows asked for %d rows, no more than %d", bigRows, rows)
	}
}

// The glyphs are compact and nothing on screen says what they mean; the footer
// is where you find out, for whichever row the cursor is on.
func TestFooterSpellsOutTheSelectedAgent(t *testing.T) {
	ws := flatSample()
	ws[1].Agent = agent.Record{Agent: agent.Claude, State: agent.Perm, PID: 2, At: 1}
	m := modelAt(t, ws, Options{Tree: true, MergeSingle: true}, 150, 30)

	m.cursor = 0
	if got := strings.Join(m.footerLines(), " "); strings.Contains(got, "waiting for permission") {
		t.Errorf("footer described an agent for a row that has none: %q", got)
	}
	m.cursor = 1
	got := ansi.Strip(strings.Join(m.footerLines(), " "))
	if !strings.Contains(got, "claude") || !strings.Contains(got, "waiting for permission") {
		t.Errorf("footer = %q, want it to name the agent and its state", got)
	}
}

// The footer grows by a line when it describes an agent, so the list has to
// give one back — otherwise the bottom row is drawn over the frame.
func TestListHeightReservesTheFooter(t *testing.T) {
	ws := flatSample()
	ws[1].Agent = agent.Record{Agent: agent.Claude, State: agent.Perm, PID: 2, At: 1}
	m := modelAt(t, ws, Options{Tree: true, MergeSingle: true}, 150, 30)

	m.cursor = 0
	plain := m.listHeight()
	m.cursor = 1
	withAgent := m.listHeight()
	if withAgent >= plain {
		t.Errorf("listHeight %d with the agent line, %d without; the line was not reserved",
			withAgent, plain)
	}
}

// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/dsaad68/kaku-tab/internal/model"
)

func paneSample() []model.Window {
	return []model.Window{
		{
			RawWindow: model.RawWindow{Session: "api", ID: "@1", Index: "1", Name: "nvim", Panes: 2, Path: "/home/u/api"},
			Status:    model.Visible, TabID: "8", GUIWin: "0", ClientSession: "api",
			Panes_: []model.Pane{
				{ID: "%1", Index: "1", Cmd: "nvim", Path: "/home/u/api", Active: true},
				{ID: "%2", Index: "2", Cmd: "zsh", Path: "/home/u/api"},
			},
		},
	}
}

func paneModel(t *testing.T) *Model {
	t.Helper()
	m := New(paneSample(), Options{Tree: true, PaneMode: true, SelfTab: "8"})
	m.width, m.height = 150, 30
	return m
}

// The marker used to hang directly off the glyph — "●*" read as one smudged
// symbol rather than a status and a flag.
func TestActivePaneMarkerIsNotFlushAgainstTheGlyph(t *testing.T) {
	m := paneModel(t)
	for _, r := range m.rows {
		if r.kind != kindPane || !r.pane.Active {
			continue
		}
		plain := ansi.Strip(m.renderRow(r, false))
		if strings.Contains(plain, "●*") || strings.Contains(plain, "◍*") || strings.Contains(plain, "○*") {
			t.Errorf("marker is flush against the glyph: %q", strings.TrimRight(plain, " "))
		}
		return
	}
	t.Fatal("no active pane row")
}

// Reserving the marker column on every pane row is what keeps the columns
// straight: when only the active row carried it, that row's label, command and
// path all sat one cell right of its neighbours'.
func TestPaneRowColumnsAlignRegardlessOfTheMarker(t *testing.T) {
	m := paneModel(t)

	var cols []int
	for _, r := range m.rows {
		if r.kind != kindPane {
			continue
		}
		plain := ansi.Strip(m.renderRow(r, false))
		i := strings.Index(plain, "1.")
		if i < 0 {
			t.Fatalf("no pane label in %q", plain)
		}
		cols = append(cols, runewidth.StringWidth(plain[:i]))
	}
	if len(cols) < 2 {
		t.Fatal("need at least two pane rows")
	}
	for _, c := range cols[1:] {
		if c != cols[0] {
			t.Errorf("label column drifts across pane rows: %v", cols)
			break
		}
	}
}

// The cursor bar and a collapsed session's fold arrow used to be the same
// glyph, sitting two columns apart and meaning different things.
func TestCursorGlyphIsNotTheFoldArrow(t *testing.T) {
	m := newTestModel(t)
	for _, r := range m.rows {
		sel := ansi.Strip(m.renderRow(r, true))
		if strings.HasPrefix(strings.TrimLeft(sel, " "), "▸") {
			t.Errorf("cursor still renders as the fold arrow: %q", sel)
		}
	}
}

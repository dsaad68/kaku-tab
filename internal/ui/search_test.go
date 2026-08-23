// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/dsaad68/kaku-tab/internal/model"
)

// withColor forces a colour profile for the duration of one test. lipgloss
// renders plain when it cannot see a terminal, so without this every styled
// string in a test is indistinguishable from an unstyled one — and "did this
// get highlighted" is exactly what needs asserting.
func withColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// searchWith builds a model around a fixed set of scrollback lines, standing in
// for what index() would have captured from the panes.
func searchWith(lines ...string) *SearchModel {
	w := model.Window{
		RawWindow: model.RawWindow{Session: "api", ID: "@1", Index: "1", Name: "zsh"},
		Status:    model.Visible, TabID: "5",
	}
	p := model.Pane{ID: "%1", Index: "1"}
	m := NewSearch([]model.Window{w}, Options{Tree: true}, "")
	for i, l := range lines {
		m.hits = append(m.hits, hit{win: w, pane: p, line: i + 1, text: l, low: strings.ToLower(l)})
	}
	m.indexing = false
	return m
}

// An empty query shows nothing rather than everything: the whole scrollback of
// every pane is not a useful default screenful.
func TestSearchEmptyQueryMatchesNothing(t *testing.T) {
	m := searchWith("alpha", "beta")
	m.refilter()
	if len(m.view) != 0 {
		t.Errorf("empty query matched %d lines, want 0", len(m.view))
	}
}

func TestSearchIsCaseInsensitiveSubstring(t *testing.T) {
	m := searchWith("Error: connection refused", "all good", "MINOR ERROR")
	for _, q := range []string{"error", "ERROR", "ErRoR"} {
		m.query = q
		m.refilter()
		if len(m.view) != 2 {
			t.Errorf("query %q matched %d lines, want 2", q, len(m.view))
		}
	}

	m.query = "refused"
	m.refilter()
	if len(m.view) != 1 || m.hits[m.view[0]].text != "Error: connection refused" {
		t.Errorf("substring query matched %v", m.view)
	}
}

func TestSearchQueryIsTrimmed(t *testing.T) {
	m := searchWith("needle in here")
	m.query = "  needle  "
	m.refilter()
	if len(m.view) != 1 {
		t.Errorf("padded query matched %d lines, want 1", len(m.view))
	}
}

// The result set is capped: a one-letter query against a full scrollback would
// otherwise build a slice of every line in every pane.
func TestSearchCapsResults(t *testing.T) {
	lines := make([]string, 3000)
	for i := range lines {
		lines[i] = "match"
	}
	m := searchWith(lines...)
	m.query = "match"
	m.refilter()
	if len(m.view) != 2000 {
		t.Errorf("matched %d lines, want the 2000 cap", len(m.view))
	}
}

// Narrowing the query must not leave the cursor pointing past the end of the
// results — the row it names is what Enter acts on.
func TestSearchCursorClampsWhenResultsShrink(t *testing.T) {
	m := searchWith("aaa one", "aaa two", "aaa three", "bbb")
	m.query = "aaa"
	m.refilter()
	m.cursor = len(m.view) - 1

	m.query = "aaa one"
	m.refilter()
	if m.cursor >= len(m.view) {
		t.Errorf("cursor %d past %d results", m.cursor, len(m.view))
	}
	if m.cursor < 0 {
		t.Errorf("cursor went negative: %d", m.cursor)
	}

	m.query = "no such thing"
	m.refilter()
	if m.cursor != 0 {
		t.Errorf("cursor %d with no results, want 0", m.cursor)
	}
}

// A hit is one line out of thousands; without the match marked you still have
// to hunt for it.
func TestHighlightMarksTheMatch(t *testing.T) {
	withColor(t)
	got := highlight("connection refused here", "refused")
	if ansi.Strip(got) != "connection refused here" {
		t.Errorf("highlight altered the text: %q", ansi.Strip(got))
	}
	if got == "connection refused here" {
		t.Error("nothing was styled")
	}
	// The styling must wrap the match, not the whole line.
	if !strings.HasPrefix(got, "connection ") {
		t.Errorf("text before the match was styled: %q", got)
	}
}

func TestHighlightIsCaseInsensitive(t *testing.T) {
	withColor(t)
	got := highlight("Connection REFUSED", "refused")
	if ansi.Strip(got) != "Connection REFUSED" {
		t.Errorf("highlight altered the text: %q", ansi.Strip(got))
	}
	if got == "Connection REFUSED" {
		t.Error("a differently-cased match was not styled")
	}
}

// A query that does not appear, or is empty, leaves the line exactly as it was.
func TestHighlightLeavesNonMatchesAlone(t *testing.T) {
	withColor(t)
	for _, q := range []string{"", "   ", "absent"} {
		if got := highlight("a plain line", q); got != "a plain line" {
			t.Errorf("query %q altered the line: %q", q, got)
		}
	}
}

// The scrollback view scrolls, so the cursor has to stay on screen or Enter
// acts on a row you cannot see.
func TestSearchEnsureVisibleFollowsTheCursor(t *testing.T) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "match"
	}
	m := searchWith(lines...)
	m.width, m.height = 100, 20
	m.query = "match"
	m.refilter()

	h := m.listHeight()
	m.cursor = 300
	m.ensureVisible()
	if m.cursor < m.offset || m.cursor >= m.offset+h {
		t.Errorf("cursor %d outside the window [%d,%d)", m.cursor, m.offset, m.offset+h)
	}

	m.cursor = 0
	m.ensureVisible()
	if m.offset != 0 {
		t.Errorf("offset %d after returning to the top", m.offset)
	}
}

func TestSearchBadgeNamesTheDestination(t *testing.T) {
	m := searchWith("x")
	cases := []struct {
		st   model.Status
		want string
	}{
		{model.Visible, "kaku 5"},
		{model.AttachedHidden, "hidden"},
		{model.Detached, "new tab"},
	}
	for _, tc := range cases {
		w := model.Window{Status: tc.st, TabID: "5"}
		if got := ansi.Strip(m.badge(w)); !strings.Contains(got, tc.want) {
			t.Errorf("badge for %v = %q, want it to mention %q", tc.st, got, tc.want)
		}
	}
}

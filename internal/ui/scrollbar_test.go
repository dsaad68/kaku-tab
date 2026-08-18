// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func blanks(n, w int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strings.Repeat(" ", w)
	}
	return out
}

// The gutter is what tells you the list did not end at the frame.
func TestScrollbarAppearsOnlyWhenTheListOverflows(t *testing.T) {
	fits := withScrollbar(blanks(5, 10), 10, 5, 0)
	for i, l := range fits {
		if got := ansi.Strip(l); strings.ContainsAny(got, "│┃") {
			t.Errorf("line %d drew a bar for a list that fits: %q", i, got)
		}
	}

	overflows := withScrollbar(blanks(5, 10), 10, 40, 0)
	if !strings.ContainsAny(ansi.Strip(strings.Join(overflows, "")), "│┃") {
		t.Error("no bar drawn for a list four times the viewport")
	}
}

// A gutter that came and went with the query would re-budget every column.
func TestScrollbarWidthIsConstant(t *testing.T) {
	for _, total := range []int{1, 5, 6, 500} {
		lines := withScrollbar(blanks(5, 10), 10, total, 0)
		for i, l := range lines {
			if got := ansi.StringWidth(l); got != 10+scrollbarCells {
				t.Errorf("total=%d line %d width %d, want %d", total, i, got, 10+scrollbarCells)
			}
		}
	}
}

// The thumb has to reach the bottom at the bottom, or a list you have scrolled
// all the way through still looks like it continues.
func TestScrollbarThumbTracksTheOffset(t *testing.T) {
	const h, total = 5, 40

	top := thumbRows(withScrollbar(blanks(h, 4), 4, total, 0))
	if len(top) == 0 || top[0] != 0 {
		t.Errorf("at offset 0 the thumb sits at rows %v, want it to start at 0", top)
	}

	bottom := thumbRows(withScrollbar(blanks(h, 4), 4, total, total-h))
	if len(bottom) == 0 || bottom[len(bottom)-1] != h-1 {
		t.Errorf("at the last offset the thumb sits at rows %v, want it to end at %d", bottom, h-1)
	}
}

// On a long list an exactly proportional thumb rounds to zero cells and the bar
// disappears at the size where it matters most.
func TestScrollbarThumbSurvivesAVeryLongList(t *testing.T) {
	if got := thumbRows(withScrollbar(blanks(5, 4), 4, 100000, 0)); len(got) == 0 {
		t.Error("thumb vanished on a 100k-row list")
	}
}

// An offset past the end must not index outside the gutter.
func TestScrollbarClampsAnOutOfRangeOffset(t *testing.T) {
	lines := withScrollbar(blanks(5, 4), 4, 40, 999)
	if got := thumbRows(lines); len(got) == 0 || got[len(got)-1] != 4 {
		t.Errorf("thumb rows %v, want it pinned to the bottom", got)
	}
}

func thumbRows(lines []string) []int {
	var out []int
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "┃") {
			out = append(out, i)
		}
	}
	return out
}

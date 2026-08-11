// SPDX-License-Identifier: MIT

package ui

import (
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// truncateANSI cuts a styled string to w display cells without severing an
// escape sequence — plain slicing would leave the terminal stuck in a colour.
func truncateANSI(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func stripANSI(s string) string { return ansi.Strip(s) }

// padToWidth pads a styled string to w display cells, so the selected-row
// highlight spans the full list width instead of stopping at the text.
func padToWidth(s string, w int) string {
	n := ansi.StringWidth(s)
	if n >= w {
		return truncateANSI(s, w)
	}
	return s + strings.Repeat(" ", w-n)
}

// scrollbarCells is the gutter reserved at the right edge of a list.
//
// Reserved always, even when every row fits. A gutter that appeared only on
// overflow would re-budget every column the moment a query filtered a row away,
// and column widths are a property of the table, not of the query.
const scrollbarCells = 1

// withScrollbar pads each line to width and appends the gutter: a track with a
// thumb sized to the fraction of the list on screen.
//
// Without it a list that runs past the viewport looks exactly like a list that
// ends there. The rows simply stop at the frame, with nothing to say the next
// session is one keypress below.
func withScrollbar(lines []string, width, total, offset int) []string {
	h := len(lines)
	if h == 0 {
		return lines
	}
	if total <= h {
		for i := range lines {
			lines[i] = padToWidth(lines[i], width) + " "
		}
		return lines
	}

	// Never smaller than one cell: on a long enough list an exactly
	// proportional thumb rounds to zero and vanishes where it is needed most.
	size := maxInt(1, h*h/total)
	start := offset * (h - size) / (total - h)
	if start < 0 {
		start = 0
	}
	if start > h-size {
		start = h - size
	}

	for i := range lines {
		cell := cBorder.Render("│")
		if i >= start && i < start+size {
			cell = cThumb.Render("┃")
		}
		lines[i] = padToWidth(lines[i], width) + cell
	}
	return lines
}

var (
	homeOnce sync.Once
	home     string
)

func homeDir() string {
	homeOnce.Do(func() { home, _ = os.UserHomeDir() })
	return home
}

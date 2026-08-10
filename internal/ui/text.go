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

var (
	homeOnce sync.Once
	home     string
)

func homeDir() string {
	homeOnce.Do(func() { home, _ = os.UserHomeDir() })
	return home
}

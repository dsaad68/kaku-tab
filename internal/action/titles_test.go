// SPDX-License-Identifier: MIT

package action

import "testing"

// Nerd-font window namers pad their icons generously, and a terminal tab is
// narrow. A title arriving as "󰚩   claude" wastes half the tab on whitespace.
func TestSqueeze(t *testing.T) {
	cases := map[string]string{
		"  claude  ":       "claude",
		"a   b":            "a b",
		"\tclaude\tcode\t": "claude code",
		"claude\ncode":     "claude code",
		"":                 "",
		"   ":              "",
		"single":           "single",
		" 󰚩  claude ":      "󰚩 claude",
	}
	for in, want := range cases {
		if got := squeeze(in); got != want {
			t.Errorf("squeeze(%q) = %q, want %q", in, got, want)
		}
	}
}

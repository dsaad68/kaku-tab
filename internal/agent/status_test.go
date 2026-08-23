// SPDX-License-Identifier: MIT

package agent

import (
	"strings"
	"testing"
)

func testTheme() Theme {
	return Theme{
		Sep: "<", IconFG: "crust", TextFG: "fg", TextBG: "surf",
		AgentBG: "mauve", NotifyBG: "peach", IdleBG: "grey",
		AgentIco: "A", NotifIco: "N",
	}
}

func TestTally(t *testing.T) {
	got := Tally([]Record{
		{State: Perm}, {State: Ask}, {State: Done}, {State: Err},
		{State: Busy}, {State: Busy}, {State: None},
	})
	want := Counts{Waiting: 2, Done: 1, Failed: 1, Working: 2}
	if got != want {
		t.Fatalf("Tally = %+v, want %+v", got, want)
	}
	// Open counts every agent; Attention counts only what you owe a reply to,
	// which is the whole distinction the two pills exist to draw.
	if got.Open() != 6 {
		t.Errorf("Open = %d, want 6", got.Open())
	}
	if got.Attention() != 4 {
		t.Errorf("Attention = %d, want 4", got.Attention())
	}
}

// Nothing running must render nothing at all — no icon, no zero, no separator.
// A status bar that always carries the segment is one you stop reading.
func TestSegmentEmptyWhenNoAgents(t *testing.T) {
	if s := testTheme().Segment(Counts{}); s != "" {
		t.Errorf("Segment of nothing = %q, want empty", s)
	}
}

func TestSegmentShowsBothCounts(t *testing.T) {
	s := testTheme().Segment(Counts{Waiting: 1, Working: 2})
	if !strings.Contains(s, "] 3") {
		t.Errorf("segment %q does not report 3 agents open", s)
	}
	if !strings.Contains(s, "] 1") {
		t.Errorf("segment %q does not report 1 wanting attention", s)
	}
	// Notifications lead — that is the number being scanned for.
	if strings.Index(s, "N") > strings.Index(s, "A") {
		t.Errorf("segment %q puts the open count before the notification count", s)
	}
}

// The second pill stays drawn at zero rather than disappearing: a count that
// vanished would shift the first pill sideways every time an agent finished.
func TestSegmentKeepsNotifyPillAtZero(t *testing.T) {
	s := testTheme().Segment(Counts{Working: 2})
	if !strings.Contains(s, "N") {
		t.Fatalf("segment %q dropped the notify pill at zero", s)
	}
	if !strings.Contains(s, "grey") {
		t.Errorf("segment %q should grey the notify pill at zero, got %q", s, s)
	}
	if strings.Contains(s, "peach") {
		t.Errorf("segment %q highlighted the notify pill with nothing waiting", s)
	}
}

func TestSegmentHighlightsNotifyPillWhenWaiting(t *testing.T) {
	for _, c := range []Counts{{Waiting: 1}, {Done: 1}, {Failed: 1}} {
		s := testTheme().Segment(c)
		if !strings.Contains(s, "peach") {
			t.Errorf("Segment(%+v) = %q, want the notify pill highlighted", c, s)
		}
	}
}

// Each pill is a rounded separator, an icon on its own background, then the
// value on the shared module background — catppuccin's shape, so these sit
// flush against the modules beside them.
func TestSegmentPillShape(t *testing.T) {
	s := testTheme().Segment(Counts{Waiting: 1})
	// Notifications first, then the open count.
	want := "#[fg=peach]<#[fg=crust,bg=peach] N #[fg=fg,bg=surf] 1#[fg=surf] " +
		"#[fg=mauve]<#[fg=crust,bg=mauve] A #[fg=fg,bg=surf] 1#[fg=surf] "
	if s != want {
		t.Errorf("segment shape\n got %q\nwant %q", s, want)
	}
}

// A message is stored tagged with the state it describes, and dropped on read
// when the pane has moved on — otherwise an approved permission request would
// still be displayed while the agent is back at work.
func TestMsgIsTiedToItsState(t *testing.T) {
	v := FormatMsg(Perm, "Bash: git push")
	if got := ParseMsg(Perm, v); got != "Bash: git push" {
		t.Errorf("ParseMsg with the matching state = %q", got)
	}
	if got := ParseMsg(Busy, v); got != "" {
		t.Errorf("ParseMsg with a moved-on state = %q, want empty", got)
	}
	if got := ParseMsg(None, v); got != "" {
		t.Errorf("ParseMsg with no state = %q, want empty", got)
	}
}

// A colon in the message must not confuse the tag, which is split off once.
func TestMsgKeepsColons(t *testing.T) {
	const msg = "Bash: cd /x && make test: run it"
	if got := ParseMsg(Perm, FormatMsg(Perm, msg)); got != msg {
		t.Errorf("round trip = %q, want %q", got, msg)
	}
}

func TestFormatMsgEmptyCases(t *testing.T) {
	for _, tc := range []struct {
		st  State
		msg string
	}{
		{Perm, ""}, {Perm, "   "}, {None, "something"}, {Perm, "\n\t "},
	} {
		if got := FormatMsg(tc.st, tc.msg); got != "" {
			t.Errorf("FormatMsg(%q, %q) = %q, want empty", tc.st, tc.msg, got)
		}
	}
}

// The value is read back through a \x1f-separated format string and rendered on
// one line, so control characters have to be gone before it is ever stored.
func TestMsgSanitized(t *testing.T) {
	got := ParseMsg(Done, FormatMsg(Done, "one\ttwo\x1fthree   spaced"))
	if strings.ContainsAny(got, "\n\t\x1f") {
		t.Errorf("control characters survived: %q", got)
	}
	if got != "one two three spaced" {
		t.Errorf("got %q", got)
	}
}

// An assistant reply is a whole markdown document. Flattening the lot onto one
// line produced nonsense, and the box-drawing characters in it landed inside the
// picker's own box and read as a rendering fault. Only the first line that says
// something is kept.
func TestMsgTakesFirstMeaningfulLine(t *testing.T) {
	reply := "Done. Move onto a row with an agent and you get:\n\n" +
		"```\n" +
		"╭─ claude ────────────╮\n" +
		"│ waiting for permission │\n" +
		"╰────────────────────────╯\n" +
		"```\n\n" +
		"Move off it and the box disappears."
	got := ParseMsg(Done, FormatMsg(Done, reply))
	if got != "Done. Move onto a row with an agent and you get:" {
		t.Errorf("got %q", got)
	}
}

// Line art anywhere in the kept line is dropped: one stray ╮ inside the box
// reads as its border.
func TestMsgDropsLineArt(t *testing.T) {
	for _, in := range []string{
		"result ╭─────╮ here",
		"▄▄▄ progress ▄▄▄ done",
		"────────── heading",
	} {
		got := ParseMsg(Done, FormatMsg(Done, in))
		for _, r := range got {
			if r >= 0x2500 && r <= 0x259f {
				t.Errorf("line art %q survived in %q", r, got)
			}
		}
		if got == "" {
			t.Errorf("%q was reduced to nothing", in)
		}
	}
}

// Leading markdown markers are shed so the text starts at a word — but a
// command that begins with a flag is not a bullet and must survive intact.
func TestMsgStripsMarkdownMarkers(t *testing.T) {
	cases := map[string]string{
		"## Summary of changes": "Summary of changes",
		"- fixed the resolver":  "fixed the resolver",
		"* fixed the resolver":  "fixed the resolver",
		"> quoted note":         "quoted note",
		"Bash: rm -rf ./build":  "Bash: rm -rf ./build",
		"Bash: ls -la":          "Bash: ls -la",
		"-flag-looking-thing":   "-flag-looking-thing",
	}
	for in, want := range cases {
		if got := ParseMsg(Perm, FormatMsg(Perm, in)); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// A reply that opens with a fence, a rule, or blank lines still has to yield
// its first real sentence.
func TestMsgSkipsOpeningNoise(t *testing.T) {
	in := "\n\n```go\nfunc main() {}\n```\n──────\nHere is what changed."
	if got := ParseMsg(Done, FormatMsg(Done, in)); got != "func main() {}" {
		t.Errorf("got %q", got)
	}
}

func TestMsgCapped(t *testing.T) {
	got := ParseMsg(Done, FormatMsg(Done, strings.Repeat("word ", 400)))
	if len(got) > MaxMsg {
		t.Errorf("stored %d bytes, cap is %d", len(got), MaxMsg)
	}
	if got == "" {
		t.Error("capping threw the whole message away")
	}
}

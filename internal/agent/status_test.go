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
	want := "#[fg=mauve]<#[fg=crust,bg=mauve] A #[fg=fg,bg=surf] 1#[fg=surf] " +
		"#[fg=peach]<#[fg=crust,bg=peach] N #[fg=fg,bg=surf] 1#[fg=surf] "
	if s != want {
		t.Errorf("segment shape\n got %q\nwant %q", s, want)
	}
}

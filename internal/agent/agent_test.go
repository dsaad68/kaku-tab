// SPDX-License-Identifier: MIT

package agent

import (
	"os"
	"testing"
)

func TestFormatParseRoundTrip(t *testing.T) {
	for _, r := range []Record{
		{Agent: Claude, State: Perm, PID: 4242, At: 1787137188},
		{Agent: Devin, State: Busy, PID: 2, At: 0},
		{Agent: Claude, State: Err, PID: 999999, At: 1},
	} {
		got := Parse(Format(r))
		if got != r {
			t.Errorf("round trip %+v -> %q -> %+v", r, Format(r), got)
		}
	}
}

func TestFormatEmpty(t *testing.T) {
	if s := Format(Record{}); s != "" {
		t.Errorf("empty record formatted as %q, want empty", s)
	}
}

// A pane option is user-writable tmux state, so anything we cannot understand
// must read as "no agent" rather than as a half-populated record.
func TestParseRejectsGarbage(t *testing.T) {
	for _, s := range []string{
		"",
		"claude",
		"claude:perm:1",
		"claude:perm:100:1:extra",
		"emacs:perm:100:1",     // unknown agent
		"claude:napping:100:1", // unknown state
		"claude:perm:abc:1",    // non-numeric pid
		"claude:perm:1:1",      // pid <= 1
		"claude:perm:100:x",    // non-numeric timestamp
		"claude:perm:100:-5",   // negative timestamp
	} {
		if got := Parse(s); !got.Empty() {
			t.Errorf("Parse(%q) = %+v, want empty", s, got)
		}
	}
}

func TestParseTolerantOfWhitespace(t *testing.T) {
	if got := Parse("  claude:done:77:5\n"); got.State != Done || got.PID != 77 {
		t.Errorf("Parse trimmed = %+v", got)
	}
}

func TestAttention(t *testing.T) {
	want := map[State]bool{Perm: true, Ask: true, Done: true, Err: true, Busy: false, None: false}
	for st, w := range want {
		if got := (Record{Agent: Claude, State: st}).Attention(); got != w {
			t.Errorf("Attention(%q) = %v, want %v", st, got, w)
		}
	}
}

// Perm and Ask outrank Err: a blocked agent is burning wall-clock right now
// where a failed turn has already stopped.
func TestRankOrder(t *testing.T) {
	order := []State{Perm, Ask, Err, Done, Busy, None}
	for i := 1; i < len(order); i++ {
		prev := Record{Agent: Claude, State: order[i-1]}.Rank()
		cur := Record{Agent: Claude, State: order[i]}.Rank()
		if prev >= cur {
			t.Errorf("rank(%q)=%d not before rank(%q)=%d", order[i-1], prev, order[i], cur)
		}
	}
}

func TestBest(t *testing.T) {
	rs := []Record{
		{Agent: Claude, State: Busy, PID: 2},
		{Agent: Devin, State: Perm, PID: 3},
		{Agent: Claude, State: Done, PID: 4},
	}
	if got := Best(rs); got.State != Perm || got.Agent != Devin {
		t.Errorf("Best = %+v, want the devin/perm record", got)
	}
	if got := Best([]Record{{}, {}}); !got.Empty() {
		t.Errorf("Best of empties = %+v, want empty", got)
	}
	if got := Best(nil); !got.Empty() {
		t.Errorf("Best(nil) = %+v, want empty", got)
	}
}

func TestLiveRejectsUnknownAndDead(t *testing.T) {
	if Live(Record{}) {
		t.Error("empty record reported live")
	}
	if Live(Record{Agent: Claude, State: Busy, PID: 0}) {
		t.Error("pid 0 reported live")
	}
	if !Live(Record{Agent: Claude, State: Busy, PID: os.Getpid()}) {
		t.Error("our own pid reported dead")
	}
}

// SPDX-License-Identifier: MIT

package mru

import (
	"strings"
	"testing"
)

type memStore struct{ v string }

func (s *memStore) Get() string { return s.v }

func (s *memStore) Set(v string) error { s.v = v; return nil }

func TestRecordPushesToFrontAndDeduplicates(t *testing.T) {
	s := &memStore{}
	for _, id := range []string{"@1", "@2", "@3", "@2"} {
		if err := Record(s, id); err != nil {
			t.Fatal(err)
		}
	}
	want := "@2,@3,@1"
	if s.v != want {
		t.Errorf("got %q want %q", s.v, want)
	}
}

// The option lives on a long-running tmux server, so an unbounded list would
// grow for as long as that server does.
func TestRecordCapsTheList(t *testing.T) {
	s := &memStore{}
	for i := 0; i < Cap*2; i++ {
		if err := Record(s, "@"+itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(List(s)); got != Cap {
		t.Errorf("stored %d entries, want %d", got, Cap)
	}
	if got := List(s)[0]; got != "@"+itoa(Cap*2-1) {
		t.Errorf("most recent is %q, want the last recorded", got)
	}
}

func TestListIgnoresBlanksAndEmptyOption(t *testing.T) {
	if got := List(&memStore{}); got != nil {
		t.Errorf("empty option gave %v, want nil", got)
	}
	got := List(&memStore{v: " @1 , ,@2, "})
	if strings.Join(got, ",") != "@1,@2" {
		t.Errorf("got %v", got)
	}
}

// The head of the list is always the window you just switched to, i.e. where
// you are now. Ranking it first would put the cursor on a row whose Enter does
// nothing — the one outcome an MRU order exists to prevent.
func TestRanksDemotesTheCurrentWindow(t *testing.T) {
	r := Ranks([]string{"@1", "@2", "@3"}, "@1")
	if r["@2"] != 0 {
		t.Errorf("@2 rank %d, want 0", r["@2"])
	}
	if r["@1"] != 1 {
		t.Errorf("@1 rank %d, want 1", r["@1"])
	}
	if r["@3"] != 2 {
		t.Errorf("@3 rank %d, want 2", r["@3"])
	}
}

func TestRanksLeavesListAloneWhenCurrentIsNotHead(t *testing.T) {
	list := []string{"@1", "@2"}
	r := Ranks(list, "@2")
	if r["@1"] != 0 || r["@2"] != 1 {
		t.Errorf("got %v, want @1=0 @2=1", r)
	}
	// Ranks must not reorder the caller's slice: the picker rebuilds rows on
	// every reload from the same list.
	if list[0] != "@1" || list[1] != "@2" {
		t.Errorf("input slice mutated: %v", list)
	}
}

func TestRanksHandlesDegenerateInput(t *testing.T) {
	if got := Ranks(nil, "@1"); len(got) != 0 {
		t.Errorf("nil list gave %v", got)
	}
	if got := Ranks([]string{"@1"}, "@1"); got["@1"] != 0 {
		t.Errorf("single entry gave %v, want @1=0", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

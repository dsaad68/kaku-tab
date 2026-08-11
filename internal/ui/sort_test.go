// SPDX-License-Identifier: MIT

package ui

import (
	"strings"
	"testing"

	"github.com/dsaad68/kaku-tab/internal/model"
)

// sessions lists header groups in the order they were built.
func sessions(m *Model) []string {
	var out []string
	for _, r := range m.rows {
		if r.kind == kindHeader {
			out = append(out, r.group)
		}
	}
	return out
}

// windowIDs lists the window rows of one group, in order.
func windowIDs(m *Model, group string) []string {
	var out []string
	for _, r := range m.rows {
		if r.kind == kindWindow && r.group == group {
			out = append(out, r.win.ID)
		}
	}
	return out
}

func TestSortTabsIsTheDefault(t *testing.T) {
	m := New(sample(), Options{Tree: true, SelfTab: "8"})
	if got := sessions(m); strings.Join(got, ",") != "api,termdown" {
		t.Errorf("got %v, want attached session first", got)
	}
	if got := windowIDs(m, "api"); strings.Join(got, ",") != "@1,@2" {
		t.Errorf("got %v, want resolve's index order", got)
	}
}

// sample() has api:@2 visible in the invoking tab, so @2 heads any MRU list and
// must be demoted: the session holding the next-most-recent window sorts first.
func TestSortMRUOrdersSessionsAndWindowsByRecency(t *testing.T) {
	m := New(sample(), Options{
		Tree: true, SelfTab: "8",
		Sort: SortMRU,
		MRU:  []string{"@2", "@3", "@1"},
	})

	if got := sessions(m); strings.Join(got, ",") != "termdown,api" {
		t.Errorf("session order %v, want termdown first — @3 outranks the demoted @2", got)
	}
	if got := windowIDs(m, "api"); strings.Join(got, ",") != "@2,@1" {
		t.Errorf("window order %v, want @2 before @1", got)
	}
}

// Enter on the top header must not land on the window you are already in — the
// whole point of demoting the current window one place.
func TestSortMRUHeaderDoesNotTargetTheCurrentWindow(t *testing.T) {
	m := New(sample(), Options{
		Tree: true, SelfTab: "8",
		Sort: SortMRU,
		MRU:  []string{"@2", "@1"}, // only session "api" has been visited
	})
	for _, r := range m.rows {
		if r.kind == kindHeader && r.group == "api" {
			if r.win.ID == "@2" {
				t.Error("header targets @2, the window already showing in this tab")
			}
			return
		}
	}
	t.Fatal("no api header")
}

// Nothing recorded yet — a fresh tmux server — must look exactly like the
// default, or turning the option on would scramble the list until you had used
// the picker enough times to fill it.
func TestSortMRUWithNoHistoryMatchesDefault(t *testing.T) {
	def := New(sample(), Options{Tree: true, SelfTab: "8"})
	byMRU := New(sample(), Options{Tree: true, SelfTab: "8", Sort: SortMRU})

	if a, b := sessions(def), sessions(byMRU); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("sessions %v != %v", b, a)
	}
	if a, b := windowIDs(def, "api"), windowIDs(byMRU, "api"); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("windows %v != %v", b, a)
	}
}

// Windows that have never been picked keep their index order behind the ones
// that have, rather than being shuffled by map iteration.
func TestSortMRUKeepsUnvisitedWindowsInIndexOrder(t *testing.T) {
	ws := []model.Window{
		{RawWindow: model.RawWindow{Session: "api", ID: "@1", Index: "1"}, Status: model.Detached},
		{RawWindow: model.RawWindow{Session: "api", ID: "@2", Index: "2"}, Status: model.Detached},
		{RawWindow: model.RawWindow{Session: "api", ID: "@3", Index: "3"}, Status: model.Detached},
	}
	m := New(ws, Options{Tree: true, Sort: SortMRU, MRU: []string{"@3"}})
	if got := windowIDs(m, "api"); strings.Join(got, ",") != "@3,@1,@2" {
		t.Errorf("got %v, want the visited window first then index order", got)
	}
}

func TestSortNameIgnoresWhetherASessionHasATab(t *testing.T) {
	ws := append(sample(),
		model.Window{RawWindow: model.RawWindow{Session: "aaa", ID: "@9", Index: "1"}, Status: model.Detached})

	byTabs := New(ws, Options{Tree: true, SelfTab: "8"})
	if got := sessions(byTabs); got[0] != "api" {
		t.Errorf("default put %q first, want the session with a tab", got[0])
	}

	byName := New(ws, Options{Tree: true, SelfTab: "8", Sort: SortName})
	if got := sessions(byName); strings.Join(got, ",") != "aaa,api,termdown" {
		t.Errorf("got %v, want plain alphabetical", got)
	}
}

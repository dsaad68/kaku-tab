// SPDX-License-Identifier: MIT

package resolve

import (
	"testing"

	"github.com/dsaad68/kaku-tab/internal/model"
)

// A miniature tmux/terminal world covering every case the join has to get
// right. Ordered as the resolver returns it:
//
//	web        @6  idx1            (hidden: tab 1 shows @45)
//	              @45 idx2  <- shown by tab 1
//	api       @29 idx1
//	              @46 idx2  <- shown by tab 9 via satellite api~kaku2
//	              @49 idx3  <- shown by tab 5
//	scratch        @70 idx1, EMPTY NAME  (no client anywhere)
//	api~kaku2 duplicates api's windows and must never be listed
type fixture struct{}

func (fixture) KakuPanes() ([]model.KakuPane, error) {
	return []model.KakuPane{
		{WindowID: 0, TabID: 1, PaneID: 1, TTYName: "/dev/ttys001", TabTitle: "web"},
		{WindowID: 0, TabID: 5, PaneID: 5, TTYName: "/dev/ttys018", TabTitle: "api"},
		{WindowID: 1, TabID: 9, PaneID: 9, TTYName: "/dev/ttys099", TabTitle: "sat"},
	}, nil
}

func (fixture) Clients() ([]model.Client, error) {
	return []model.Client{
		{TTY: "/dev/ttys001", Session: "web", WindowID: "@45"},
		{TTY: "/dev/ttys018", Session: "api", WindowID: "@49"},
		{TTY: "/dev/ttys099", Session: "api~kaku2", WindowID: "@46"},
		{TTY: "/dev/ttysXXX", Session: "elsewhere", WindowID: "@1"}, // not in Kaku
	}, nil
}

func (fixture) Windows() ([]model.RawWindow, error) {
	return []model.RawWindow{
		{Session: "web", ID: "@6", Index: "1", Name: "shell", Panes: 2, Path: "/home/u"},
		{Session: "web", ID: "@45", Index: "2", Name: "nvim", Panes: 1, Path: "/home/u/p", Activity: true},
		{Session: "api", ID: "@29", Index: "1", Name: "claude", Panes: 2, Path: "/home/u/m", Grouped: true, Group: "api"},
		{Session: "api", ID: "@46", Index: "2", Name: "just", Panes: 2, Path: "/home/u/m", Zoomed: true, Grouped: true, Group: "api"},
		{Session: "api", ID: "@49", Index: "3", Name: "claude", Panes: 2, Path: "/home/u/m", Grouped: true, Group: "api"},
		{Session: "scratch", ID: "@70", Index: "1", Name: "", Panes: 2, Path: "/home/u/z"},
		{Session: "popup", ID: "@99", Index: "1", Name: "popup", Panes: 1, Path: "/home/u"},
		{Session: "api~kaku2", ID: "@29", Index: "1", Name: "claude", Panes: 2, Path: "/home/u/m", Grouped: true, Group: "api"},
		{Session: "api~kaku2", ID: "@46", Index: "2", Name: "just", Panes: 2, Path: "/home/u/m", Grouped: true, Group: "api"},
		{Session: "api~kaku2", ID: "@49", Index: "3", Name: "claude", Panes: 2, Path: "/home/u/m", Grouped: true, Group: "api"},
	}, nil
}

func (fixture) Panes() (map[string][]model.Pane, error) {
	all := map[string][]model.Pane{}
	for _, id := range []string{"@6", "@45", "@29", "@46", "@49", "@70"} {
		all[id] = []model.Pane{
			{ID: "%1", Index: "1", Cmd: "claude", Path: "/home/u", Active: true},
			{ID: "%2", Index: "2", Cmd: "zsh", Path: "/home/u"},
		}
	}
	return all, nil
}

func resolved(t *testing.T, opt Options) []model.Window {
	t.Helper()
	opt.Ignore = append(opt.Ignore, "popup")
	ws, err := Resolve(fixture{}, opt)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return ws
}

func get(t *testing.T, ws []model.Window, id string) model.Window {
	t.Helper()
	w, ok := Find(ws, id)
	if !ok {
		t.Fatalf("window %s not found", id)
	}
	return w
}

func TestStatus(t *testing.T) {
	ws := resolved(t, Options{})
	for _, tc := range []struct {
		id   string
		want model.Status
		why  string
	}{
		{"@45", model.Visible, "window its own tab is showing"},
		{"@6", model.AttachedHidden, "same session, tab shows another window"},
		{"@70", model.Detached, "no client anywhere"},
		{"@46", model.Visible, "shown through a satellite client"},
		{"@49", model.Visible, "shown by the base session's tab"},
		{"@29", model.AttachedHidden, "api attached, this window not shown"},
	} {
		if got := get(t, ws, tc.id).Status; got != tc.want {
			t.Errorf("%s (%s): status = %v, want %v", tc.id, tc.why, got, tc.want)
		}
	}
}

func TestTabAttribution(t *testing.T) {
	ws := resolved(t, Options{})
	if got := get(t, ws, "@46").TabID; got != "9" {
		t.Errorf("@46 shown via satellite: TabID = %q, want 9", got)
	}
	if got := get(t, ws, "@46").GUIWin; got != "1" {
		t.Errorf("@46 GUIWin = %q, want 1 (second Kaku GUI window)", got)
	}
	if got := get(t, ws, "@45").TabID; got != "1" {
		t.Errorf("@45 TabID = %q, want 1", got)
	}
}

// Retargeting must aim at the client's real session. tmux keeps current-window
// per session, so selecting inside the base while the tab holds a satellite
// focuses the right tab but leaves it on the wrong window.
func TestClientSessionIsSatelliteNotBase(t *testing.T) {
	if got := get(t, resolved(t, Options{}), "@46").ClientSession; got != "api~kaku2" {
		t.Errorf("ClientSession = %q, want api~kaku2", got)
	}
}

// A session attached only through a satellite must not read as Detached, or
// its hidden windows would spawn new tabs instead of retargeting.
func TestSatelliteOnlyAttachmentIsNotDetached(t *testing.T) {
	src := satelliteOnly{}
	ws, err := Resolve(src, Options{Ignore: []string{"popup"}})
	if err != nil {
		t.Fatal(err)
	}
	w := get(t, ws, "@29")
	if w.Status != model.AttachedHidden {
		t.Errorf("status = %v, want ATTACHED_HIDDEN", w.Status)
	}
	if w.TabID != "9" {
		t.Errorf("TabID = %q, want 9", w.TabID)
	}
}

type satelliteOnly struct{ fixture }

func (satelliteOnly) Clients() ([]model.Client, error) {
	return []model.Client{{TTY: "/dev/ttys099", Session: "api~kaku2", WindowID: "@46"}}, nil
}

func TestSatellitesAreNeverListed(t *testing.T) {
	ws := resolved(t, Options{})
	n := 0
	for _, w := range ws {
		if model.IsSatellite(w.Session, model.DefaultSatelliteSuffix) {
			t.Errorf("satellite session listed: %s", w.Session)
		}
		if w.Session == "api" {
			n++
		}
	}
	if n != 3 {
		t.Errorf("api listed %d times, want 3 (not 6)", n)
	}
	if len(ws) != 6 {
		t.Errorf("total rows = %d, want 6", len(ws))
	}
}

func TestIgnoredSessionOmitted(t *testing.T) {
	for _, w := range resolved(t, Options{}) {
		if w.Session == "popup" {
			t.Error("popup session should be ignored")
		}
	}
}

// Regression: in the shell version an empty window name collapsed tab-delimited
// fields and shifted every later field left. Structs make it unrepresentable —
// this pins the behaviour.
func TestEmptyWindowNameKeepsFields(t *testing.T) {
	w := get(t, resolved(t, Options{}), "@70")
	if w.Name != "" {
		t.Errorf("Name = %q, want empty", w.Name)
	}
	if w.Panes != 2 {
		t.Errorf("Panes = %d, want 2", w.Panes)
	}
	if w.Path != "/home/u/z" {
		t.Errorf("Path = %q, want /home/u/z", w.Path)
	}
}

func TestFlags(t *testing.T) {
	ws := resolved(t, Options{})
	if !get(t, ws, "@45").Activity {
		t.Error("@45 activity flag lost")
	}
	if !get(t, ws, "@46").Zoomed {
		t.Error("@46 zoom flag lost")
	}
}

func TestOrderingGroupsBySession(t *testing.T) {
	ws := resolved(t, Options{})
	var order []string
	for _, w := range ws {
		order = append(order, w.Session+":"+w.Index)
	}
	want := []string{"api:1", "api:2", "api:3", "scratch:1", "web:1", "web:2"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestScope(t *testing.T) {
	ws := resolved(t, Options{Scope: "session", SelfSession: "api"})
	if len(ws) != 3 {
		t.Errorf("session scope: %d rows, want 3", len(ws))
	}
	// A satellite's own name must still resolve to its base group.
	ws = resolved(t, Options{Scope: "group", SelfSession: "api~kaku2"})
	if len(ws) != 3 {
		t.Errorf("group scope from satellite: %d rows, want 3", len(ws))
	}
}

func TestPaneMode(t *testing.T) {
	ws := resolved(t, Options{WithPanes: true})
	w := get(t, ws, "@49")
	if len(w.Panes_) != 2 {
		t.Fatalf("panes = %d, want 2", len(w.Panes_))
	}
	if !w.Panes_[0].Active {
		t.Error("first pane should be active")
	}
}

func TestSatelliteNaming(t *testing.T) {
	s := model.DefaultSatelliteSuffix
	for _, tc := range []struct {
		name string
		sat  bool
		base string
	}{
		{"api~kaku2", true, "api"},
		{"api~kaku17", true, "api"},
		{"api", false, "api"},
		{"api~kaku", false, "api~kaku"}, // no index: not ours
		{"~kaku2", true, ""},
	} {
		if got := model.IsSatellite(tc.name, s); got != tc.sat {
			t.Errorf("IsSatellite(%q) = %v, want %v", tc.name, got, tc.sat)
		}
		if got := model.BaseSession(tc.name, s); got != tc.base {
			t.Errorf("BaseSession(%q) = %q, want %q", tc.name, got, tc.base)
		}
	}

	taken := map[string]bool{"m~kaku2": true, "m~kaku3": true}
	if got := model.NextSatellite("m", s, func(n string) bool { return taken[n] }); got != "m~kaku4" {
		t.Errorf("NextSatellite = %q, want m~kaku4", got)
	}
}

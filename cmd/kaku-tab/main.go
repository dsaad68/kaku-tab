// SPDX-License-Identifier: MIT

// Command kaku-tab is a tmux window ⇄ Kaku tab picker.
//
//	kaku-tab pick [client-tty] [session]   the picker (run under display-popup -E)
//	kaku-tab attach <session> <window> [satellite]
//	                                       runs inside a spawned Kaku tab
//	kaku-tab resolve                       print the join, for debugging
//	kaku-tab prune                         reap orphaned satellite sessions
//	kaku-tab restore [--windows] [--dry-run]
//	                                       a Kaku tab per detached session
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dsaad68/kaku-tab/internal/action"
	"github.com/dsaad68/kaku-tab/internal/kaku"
	"github.com/dsaad68/kaku-tab/internal/model"
	"github.com/dsaad68/kaku-tab/internal/mru"
	"github.com/dsaad68/kaku-tab/internal/resolve"
	"github.com/dsaad68/kaku-tab/internal/tmux"
	"github.com/dsaad68/kaku-tab/internal/ui"
)

// version is set at build time: -ldflags "-X main.version=$(git describe ...)"
var version = "dev"

type liveSource struct{}

func (liveSource) KakuPanes() ([]model.KakuPane, error)    { return kaku.Panes() }
func (liveSource) Clients() ([]model.Client, error)        { return tmux.Clients() }
func (liveSource) Windows() ([]model.RawWindow, error)     { return tmux.Windows() }
func (liveSource) Panes() (map[string][]model.Pane, error) { return tmux.Panes() }

func opts(selfSession string, withPanes bool) resolve.Options {
	return resolve.Options{
		Suffix:      tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix),
		Scope:       tmux.Option("@kaku-tab-scope", "all"),
		SelfSession: selfSession,
		WithPanes:   withPanes,
		Ignore:      ignored(),
	}
}

// sortOption reads @kaku-tab-sort. An unrecognised value falls back to the
// default rather than failing the picker over a typo in tmux.conf.
func sortOption() string {
	switch s := tmux.Option("@kaku-tab-sort", ui.SortTabs); s {
	case ui.SortMRU, ui.SortName:
		return s
	default:
		return ui.SortTabs
	}
}

// mruList is only read for the sort mode that uses it — every tmux.Option is a
// subprocess, and the picker runs one on every keypress-triggered popup.
func mruList(sortMode string) []string {
	if sortMode != ui.SortMRU {
		return nil
	}
	return mru.List(mru.Tmux{})
}

// ignored lists sessions to hide, e.g. a throwaway popup session bound to a
// key. Comma-separated in @kaku-tab-ignore; empty by default.
func ignored() []string {
	raw := tmux.Option("@kaku-tab-ignore", "")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func main() {
	// Honour an explicit mux CLI before anything touches it.
	kaku.SetBinary(tmux.Option("@kaku-tab-mux-cli", ""))

	cmd := "pick"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "popup":
		err = popup(arg(2), arg(3))
	case "pick":
		err = pick(arg(2), arg(3))
	case "search":
		err = search(arg(2), arg(3), arg(4))
	case "attach":
		err = attach(arg(2), arg(3), arg(4))
	case "resolve":
		err = printResolve()
	case "prune":
		err = action.Prune(tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix))
	case "restore":
		err = restore(os.Args[2:])
	case "titles":
		err = titles(len(os.Args) > 2 && os.Args[2] == "--dry-run")
	case "version", "--version", "-v":
		fmt.Printf("kaku-tab %s\n", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kaku-tab:", err)
		os.Exit(1)
	}
}

const usage = `kaku-tab — tmux window ⇄ Kaku tab picker

  popup [tty] [session]  open the picker in a tmux popup (run under: run-shell -b)
  pick [tty] [session]   the picker itself (needs a pty)
  search [tty] [session] [query]
  attach <sess> <win> [satellite]
  resolve                print the resolved join
  prune                  reap orphaned satellite sessions
  restore [--windows] [--dry-run]
  titles [--dry-run]     retitle terminal tabs after their tmux window
  version
`

func arg(i int) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return ""
}

// popup owns the tmux popup around the picker.
//
// tmux cannot resize an open popup — display-popup is the only popup command
// and its -w/-h are fixed at creation. So hiding the preview reopens the popup
// at a narrower size, and the picker's state is carried across in a temp file
// so nothing is lost. display-popup -E blocks until the popup exits, which is
// what makes this loop possible.
func popup(selfTTY, selfSession string) error {
	stateFile, err := os.CreateTemp("", "kaku-tab-state-*.json")
	if err != nil {
		return err
	}
	path := stateFile.Name()
	_ = stateFile.Close()
	defer func() { _ = os.Remove(path) }()

	self, _ := os.Executable()
	preview := tmux.Option("@kaku-tab-preview", "off") == "on"

	for i := 0; i < 16; i++ { // bounded: a toggle loop should never run away
		w, h := popupSize(preview)
		args := []string{"display-popup", "-E", "-B", "-w", w, "-h", h}
		if selfTTY != "" {
			args = append(args, "-c", selfTTY)
		}
		args = append(args, self, "pick", selfTTY, selfSession, path)
		if _, err := tmux.Run(args...); err != nil {
			return err
		}

		st, ok := readState(path)
		if !ok || !st.Relaunch {
			return nil
		}
		preview = st.Preview
		// Remember the choice for the next cold start: a toggle that forgets
		// itself the moment you close the picker is just an annoyance. This is
		// runtime-only and resets when the tmux config is reloaded.
		on := "off"
		if preview {
			on = "on"
		}
		_, _ = tmux.Run("set-option", "-g", "@kaku-tab-preview", on)
	}
	return nil
}

// popupSize picks the geometry for the current preview setting: without a
// preview pane the list alone needs far less width.
func popupSize(preview bool) (string, string) {
	size := tmux.Option("@kaku-tab-popup-size", "90%,85%")
	if !preview {
		size = tmux.Option("@kaku-tab-popup-size-compact", "60%,70%")
	}
	w, h, ok := strings.Cut(size, ",")
	if !ok {
		return "90%", "85%"
	}
	return strings.TrimSpace(w), strings.TrimSpace(h)
}

type persisted struct {
	ui.State
	Relaunch bool `json:"relaunch"`
}

func readState(path string) (persisted, bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return persisted{}, false
	}
	var st persisted
	if json.Unmarshal(b, &st) != nil {
		return persisted{}, false
	}
	return st, true
}

func writeState(path string, st persisted) {
	if path == "" {
		return
	}
	if b, err := json.Marshal(st); err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
}

func pick(selfTTY, selfSession string) error {
	statePath := arg(4)
	// An empty state file means a cold start; a populated one means we were
	// just reopened at a different size and must restore where the user was.
	restore, resumed := readState(statePath)
	suffix := tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix)
	_ = action.Prune(suffix)

	reload := func(panes bool) ([]model.Window, error) {
		return resolve.Resolve(liveSource{}, opts(selfSession, panes))
	}

	paneMode := restore.PaneMode
	ws, err := reload(paneMode)
	if err != nil {
		return err
	}
	if len(ws) == 0 {
		return fmt.Errorf("no tmux windows found")
	}

	selfTab := ""
	if p, ok := kaku.ByTTY(selfTTY); ok {
		selfTab = strconv.Itoa(p.TabID)
	}

	mode := action.Reuse
	if tmux.Option("@kaku-tab-open-mode", "reuse") == "go" {
		mode = action.New
	}

	preview := tmux.Option("@kaku-tab-preview", "off") == "on"
	if resumed {
		preview = restore.Preview
	}

	// Deliberately not written back the way @kaku-tab-preview is. A filter that
	// silently persisted would have you reopen the picker one day, find half
	// your sessions gone, and have no idea why.
	hideDetached := tmux.Option("@kaku-tab-detached", "on") == "off"
	if resumed {
		hideDetached = restore.HideDetached
	}

	self, _ := os.Executable()
	ctx := action.Ctx{SelfTTY: selfTTY, Suffix: suffix, AttachSh: self}

	sortMode := sortOption()
	m := ui.New(ws, ui.Options{
		Suffix:   suffix,
		SelfTab:  selfTab,
		Preview:  preview,
		Tree:     tmux.Option("@kaku-tab-tree", "on") == "on",
		PaneMode: paneMode,
		OpenMode: mode,
		Reload:   reload,
		Ctx:      ctx,
		Restore:  restore.State,
		Sort:     sortMode,
		MRU:      mruList(sortMode),

		HideDetached: hideDetached,
	})

	// The picker owns the popup's terminal; Kaku's own alt-screen is untouched.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
	if _, err := p.Run(); err != nil {
		return err
	}

	res := m.Result()
	writeState(statePath, persisted{State: res.State, Relaunch: res.Relaunch})
	if res.Relaunch || !res.Chosen {
		return nil
	}
	// Recorded unconditionally, not just under @kaku-tab-sort 'mru': the list
	// has to already be there for the option to do anything the first time it
	// is switched on.
	_ = mru.Record(mru.Tmux{}, res.Window.ID)
	return action.Go(res.Window, res.PaneID, res.Mode, ctx)
}

func search(selfTTY, selfSession, query string) error {
	suffix := tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix)
	// Panes are needed up front: search indexes every pane's scrollback.
	ws, err := resolve.Resolve(liveSource{}, opts(selfSession, true))
	if err != nil {
		return err
	}
	depth := 2000
	if n, err := strconv.Atoi(tmux.Option("@kaku-tab-search-depth", "2000")); err == nil && n > 0 {
		depth = n
	}
	self, _ := os.Executable()
	ctx := action.Ctx{SelfTTY: selfTTY, Suffix: suffix, AttachSh: self}

	mode := action.Reuse
	if tmux.Option("@kaku-tab-open-mode", "reuse") == "go" {
		mode = action.New
	}
	m := ui.NewSearch(ws, ui.Options{Suffix: suffix, OpenMode: mode, Depth: depth, Ctx: ctx}, query)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
	if _, err := p.Run(); err != nil {
		return err
	}
	res := m.Result()
	if !res.Chosen {
		return nil
	}
	_ = mru.Record(mru.Tmux{}, res.Window.ID)
	return action.Go(res.Window, res.PaneID, res.Mode, ctx)
}

// attach runs as the program inside a freshly spawned Kaku tab.
func attach(session, windowID, satellite string) error {
	if session == "" {
		return fmt.Errorf("attach: session required")
	}
	if windowID != "" {
		// Session-qualified: a bare @id is ambiguous once a grouped session
		// shares the window.
		_ = tmux.SelectWindow(session, windowID)
	}

	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	if satellite == "" {
		// Replace this process so the tab hosts tmux directly, with no stray
		// parent shell holding the pty open.
		return syscall.Exec(bin, []string{"tmux", "attach-session", "-t", "=" + session}, os.Environ())
	}

	// A satellite must be reaped when its tab closes, so we cannot exec away.
	c := exec.Command(bin, "attach-session", "-t", "="+session)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
	// Reached on clean detach. A force-closed tab skips this; `prune` on the
	// next picker run is the backstop.
	return tmux.KillSession(session)
}

func titles(dry bool) error {
	set, err := action.SyncTitles(
		tmux.Option("@kaku-tab-title-format", action.DefaultTitleFormat),
		tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix),
		dry)
	if err != nil {
		return err
	}
	if dry {
		for tab, title := range set {
			fmt.Printf("tab %-3s <- %s\n", tab, title)
		}
	}
	return nil
}

func printResolve() error {
	ws, err := resolve.Resolve(liveSource{}, opts("", false))
	if err != nil {
		return err
	}
	for _, w := range ws {
		fmt.Printf("%-16s %-5s %-14s idx=%-3s tab=%-3s gui=%-3s client=%-16s %s\n",
			w.Status, w.ID, w.Session, w.Index, dash(w.TabID), dash(w.GUIWin),
			dash(w.ClientSession), strings.TrimSpace(w.Name))
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func restore(args []string) error {
	dry, perWindow := false, false
	for _, a := range args {
		switch a {
		case "--dry-run":
			dry = true
		case "--windows":
			perWindow = true
		}
	}
	if !kaku.Available() {
		return fmt.Errorf("kaku CLI not found")
	}
	suffix := tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix)
	_ = action.Prune(suffix)

	ws, err := resolve.Resolve(liveSource{}, opts("", false))
	if err != nil {
		return err
	}

	self, _ := os.Executable()
	n := 0
	for _, w := range ws {
		if w.Status != model.Detached {
			continue
		}
		if !perWindow {
			cur, err := tmux.Run("display-message", "-p", "-t", "="+w.Session, "#{window_id}")
			if err != nil || cur != w.ID {
				continue
			}
		}
		if dry {
			fmt.Printf("would open  %s:%s  %s\n", w.Session, w.Index, strings.TrimSpace(w.Name))
			n++
			continue
		}
		if _, err := kaku.Spawn("", self, "attach", w.Session, w.ID, ""); err != nil {
			return err
		}
		fmt.Printf("opened %s:%s\n", w.Session, w.Index)
		n++
		time.Sleep(250 * time.Millisecond) // let the mux register the tab
	}
	if n == 0 {
		fmt.Println("nothing to restore — every session already has a tab")
	}
	return nil
}

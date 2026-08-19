// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dsaad68/kaku-tab/internal/agent"
	"github.com/dsaad68/kaku-tab/internal/model"
	"github.com/dsaad68/kaku-tab/internal/resolve"
	"github.com/dsaad68/kaku-tab/internal/tmux"
)

var paneIDRe = regexp.MustCompile(`^%[0-9]+$`)

// maxHookPayload caps what we read from a hook's stdin. The fields we want are
// in the first few hundred bytes, but a PostToolUse payload can carry a whole
// file's contents, and a hook must not be the thing that stalls the agent.
const maxHookPayload = 1 << 20

// hook is the publisher: it runs as a child of Claude Code or Devin CLI, learns
// the pane from the $TMUX_PANE it inherited, and records what the agent is doing
// on that pane.
//
// It is inert by contract. It never writes to stdout and never exits non-zero:
// on PermissionRequest and the PreToolUse family, stdout is a decision channel
// and a non-zero exit blocks the call, so a status reporter that got either
// wrong would start silently vetoing the user's own tool calls.
func hook() error {
	// A read error leaves the payload empty or truncated; either way Decide
	// rejects it and we exit quietly. The hook is never the thing that fails an
	// agent's turn.
	body, _ := io.ReadAll(io.LimitReader(os.Stdin, maxHookPayload))
	act, state := agent.Decide(body)
	if act == agent.Ignore {
		return nil
	}

	// No pane means the agent is not running under tmux — an IDE session, a
	// bare terminal, CI. Nothing to record, and nothing wrong.
	pane := os.Getenv("TMUX_PANE")
	if os.Getenv("TMUX") == "" || !paneIDRe.MatchString(pane) {
		return nil
	}

	if act == agent.Clear {
		_ = tmux.UnsetPaneOption(pane, agent.PaneOption)
		tmux.RefreshStatus()
		return nil
	}

	// The parent is the agent process itself, which is why the hooks are
	// installed in exec form (Claude Code's `args`) and with `exec` (Devin):
	// a shell wrapper would make this the pid of a shell that exits
	// immediately, and the record would read as dead the moment it was written.
	rec := agent.Record{
		Agent: agent.Detect(os.Getenv),
		State: state,
		PID:   os.Getppid(),
		At:    time.Now().Unix(),
	}
	_ = tmux.SetPaneOption(pane, agent.PaneOption, agent.Format(rec))
	tmux.RefreshStatus()
	return nil
}

// sweep reads every pane's record, clearing any whose agent process is gone.
// This is the one case pane-scoped storage cannot self-heal: an agent killed
// outright never fires SessionEnd, and its pane outlives it.
func sweep() (agent.Counts, error) {
	byPane, err := tmux.PaneAgents()
	if err != nil {
		return agent.Counts{}, err
	}
	live := make([]agent.Record, 0, len(byPane))
	for pane, r := range byPane {
		if r.Empty() {
			continue
		}
		if !agent.Live(r) {
			_ = tmux.UnsetPaneOption(pane, agent.PaneOption)
			continue
		}
		live = append(live, r)
	}
	return agent.Tally(live), nil
}

// loadTheme resolves the status-bar palette from tmux in one pass.
//
// Every value falls back twice: to the catppuccin palette when it is loaded,
// then to a plain terminal colour name when it is not — so the segment is
// themed without depending on a theme being installed.
func loadTheme() agent.Theme {
	o := tmux.Options(
		"@catppuccin_status_left_separator", "@thm_crust", "@thm_fg",
		"@catppuccin_status_module_text_bg", "@thm_mauve", "@thm_peach",
		"@thm_surface_2", "@kaku-tab-agent-color", "@kaku-tab-notify-color",
		"@kaku-tab-agent-icon", "@kaku-tab-notify-icon",
	)
	pick := func(def string, keys ...string) string {
		for _, k := range keys {
			if v := o[k]; v != "" {
				return v
			}
		}
		return def
	}
	return agent.Theme{
		Sep:      o["@catppuccin_status_left_separator"],
		IconFG:   pick("black", "@thm_crust"),
		TextFG:   pick("white", "@thm_fg"),
		TextBG:   pick("brightblack", "@catppuccin_status_module_text_bg"),
		AgentBG:  pick("magenta", "@kaku-tab-agent-color", "@thm_mauve"),
		NotifyBG: pick("yellow", "@kaku-tab-notify-color", "@thm_peach"),
		IdleBG:   pick("brightblack", "@thm_surface_2"),
		AgentIco: pick(agent.IconAgents, "@kaku-tab-agent-icon"),
		NotifIco: pick(agent.IconNotify, "@kaku-tab-notify-icon"),
	}
}

// refreshWindows writes the per-window rollup for use in tmux window formats.
//
// Deliberately a different option name from the pane record: tmux pane options
// inherit from window options, so reusing @kt_agent here would have every
// agent-free pane in the window read back an agent that is not there.
func refreshWindows() error {
	ws, err := resolve.Resolve(liveSource{}, resolve.Options{
		Suffix:     tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix),
		Scope:      "all",
		WithAgents: true,
	})
	if err != nil {
		return err
	}
	for _, w := range ws {
		if w.Agent.Empty() {
			_ = tmux.UnsetWindowOption(w.Session, w.ID, agent.WindowOption)
			continue
		}
		_ = tmux.SetWindowOption(w.Session, w.ID, agent.WindowOption, string(w.Agent.State))
	}
	return nil
}

// agents is the reader side: `--format tmux` for the status bar, `--refresh`
// for the per-window rollup, and a plain listing that answers "which pane is
// Claude running in" straight from the shell.
func agents(args []string) error {
	format, refresh := "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--refresh":
			refresh = true
		case "--format":
			if i+1 < len(args) {
				i++
				format = args[i]
			}
		default:
			if v, ok := strings.CutPrefix(args[i], "--format="); ok {
				format = v
			}
		}
	}

	c, err := sweep()
	if err != nil {
		return err
	}
	if refresh {
		if err := refreshWindows(); err != nil {
			return err
		}
	}
	if format == "tmux" {
		if s := loadTheme().Segment(c); s != "" {
			fmt.Println(s)
		}
		return nil
	}
	return listAgents()
}

func listAgents() error {
	ws, err := resolve.Resolve(liveSource{}, resolve.Options{
		Suffix:    tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix),
		Scope:     "all",
		WithPanes: true,
	})
	if err != nil {
		return err
	}

	type entry struct {
		where string
		rec   agent.Record
	}
	var rows []entry
	for _, w := range ws {
		for _, p := range w.Panes_ {
			if p.Agent.Empty() {
				continue
			}
			rows = append(rows, entry{
				where: fmt.Sprintf("%s:%s.%s %s", w.Session, w.Index, p.Index, p.ID),
				rec:   p.Agent,
			})
		}
	}
	if len(rows) == 0 {
		fmt.Println("no agent sessions")
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].rec.Rank() != rows[j].rec.Rank() {
			return rows[i].rec.Rank() < rows[j].rec.Rank()
		}
		return rows[i].where < rows[j].where
	})
	for _, r := range rows {
		fmt.Printf("%-6s %-5s %-28s pid=%-7d %s ago\n",
			r.rec.Agent, r.rec.State, r.where, r.rec.PID,
			time.Since(time.Unix(r.rec.At, 0)).Round(time.Second))
	}
	return nil
}

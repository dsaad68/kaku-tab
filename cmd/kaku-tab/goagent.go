// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/dsaad68/kaku-tab/internal/action"
	"github.com/dsaad68/kaku-tab/internal/agent"
	"github.com/dsaad68/kaku-tab/internal/model"
	"github.com/dsaad68/kaku-tab/internal/resolve"
	"github.com/dsaad68/kaku-tab/internal/tmux"
)

// cursorOption remembers which agent pane was last jumped to, so pressing the
// key again advances instead of landing on the same one.
//
// Server state rather than a file: it is meaningless once the panes are gone,
// and the panes die with the server.
const cursorOption = "@kaku-tab-agent-cursor"

// target is one pane with an agent that wants something.
type target struct {
	win  model.Window
	pane model.Pane
}

// goAgent jumps straight to the agent that most wants you, without going
// through the picker. Pressing the key again moves to the next one.
//
// The ordering is agent.Rank — blocked before failed before finished — so the
// first press always lands on whatever is costing you the most.
func goAgent(selfTTY, selfSession string) error {
	suffix := tmux.Option("@kaku-tab-satellite-suffix", model.DefaultSatelliteSuffix)
	// The client's own session, not "": the session and group scopes resolve
	// against it, and passing an empty one made every window fall out of scope —
	// go-agent then reported nothing waiting while an agent sat blocked.
	ws, err := resolve.Resolve(liveSource{}, opts(selfSession, true))
	if err != nil {
		return err
	}

	var ts []target
	for _, w := range ws {
		for _, p := range w.Panes_ {
			if p.Agent.Attention() {
				ts = append(ts, target{win: w, pane: p})
			}
		}
	}
	if len(ts) == 0 {
		_, _ = tmux.Run("display-message", "no agent is waiting on you")
		return nil
	}

	sort.SliceStable(ts, func(i, j int) bool {
		if ri, rj := ts[i].pane.Agent.Rank(), ts[j].pane.Agent.Rank(); ri != rj {
			return ri < rj
		}
		// Oldest first within a rank: the one that has been waiting longest is
		// the one being kept waiting.
		return ts[i].pane.Agent.At < ts[j].pane.Agent.At
	})

	pick := ts[0]
	if len(ts) > 1 {
		// Advance past whatever the last press landed on. A cursor naming a pane
		// that is gone, or not in this list, simply falls through to the front.
		last := tmux.Option(cursorOption, "")
		for i, t := range ts {
			if t.pane.ID == last {
				pick = ts[(i+1)%len(ts)]
				break
			}
		}
	}
	_ = tmux.SetOption(cursorOption, pick.pane.ID)

	self, _ := os.Executable()
	ctx := action.Ctx{SelfTTY: selfTTY, Suffix: suffix, AttachSh: self}
	if err := action.Go(pick.win, pick.pane.ID, action.Reuse, ctx); err != nil {
		return err
	}
	_, _ = tmux.Run("display-message",
		fmt.Sprintf("%s · %s — %s:%s.%s", pick.pane.Agent.Agent,
			agent.Words(pick.pane.Agent.State),
			pick.win.Session, pick.win.Index, pick.pane.Index))
	return nil
}

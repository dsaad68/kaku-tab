# Agents

Which pane is Claude Code or Devin CLI running in, and is it waiting on you.

## Why not just look at the process

`#{pane_current_command}` reports `node` for Claude Code, which is true and
useless. And even a correct process name cannot tell "thinking" from "blocked on
a permission prompt" — the difference that decides whether you should be looking
at that pane right now.

Both CLIs expose lifecycle hooks, and a hook process inherits `$TMUX_PANE` from
the agent that spawned it. So the agent reports its own pane and its own state,
and nothing has to be inferred.

## The transport is a tmux pane option

`kaku-tab hook` writes one pane-scoped user option:

```sh
tmux set-option -p -t "$TMUX_PANE" @kt_agent 'claude:perm:41337:1787137188'
```

`agent:state:pid:unix_ts`. Colon-separated where the rest of this tool uses
`\x1f`, because every field here is from a fixed alphabet — two agent names,
five state names, two integers — so none of them can contain the separator.

Two things fall out of storing it on the pane:

- `tmux list-panes` already runs on every picker invocation. `#{@kt_agent}` is
  one more field in a format string that was going to be evaluated anyway, so
  agent awareness costs **no extra process**.
- **Staleness is structural.** Close the pane and the record goes with it. There
  is no TTL, no sweeper thread, no directory to watch.

The one case that does not self-heal is an agent killed outright — no
`SessionEnd` fires, and its pane outlives it. That is what the pid in the record
is for: a record whose process is gone reads as absent, and `kaku-tab agents`
clears it.

> **`@kt_agent` is only ever set with `-p`.** From `tmux(1)`: *"Pane options
> inherit from window options."* Set it at window scope even once and every
> agent-free pane in that window reads back an agent that is not there. The
> per-window rollup is deliberately a different option, `@kt_agent_win`.

## States

| State  | Means                | Claude Code                                                          | Devin CLI          |
|--------|----------------------|----------------------------------------------------------------------|--------------------|
| `busy` | working              | `SessionStart` `UserPromptSubmit` `PostToolUse` `PostToolBatch`       | same, less the last |
| `perm` | wants permission     | `Notification:permission_prompt`                                     | `PermissionRequest` |
| `ask`  | asked you a question | `Notification:elicitation_dialog` / `agent_needs_input`              | —                  |
| `done` | finished a turn      | `Stop`, `Notification:idle_prompt` / `agent_completed`               | `Stop`             |
| `err`  | turn failed          | `StopFailure`                                                        | —                  |

`busy` is the only state you do not owe a response to; everything else counts as
waiting.

Two non-obvious choices:

- **`PostToolUse` earns its place.** It is not a heartbeat — it is what flips a
  pane out of `perm` once you approve a call and the agent resumes. Without it an
  approved pane keeps counting as waiting until the turn ends.
- **`PreToolUse` is not subscribed.** It fires on every tool call, on the agent's
  hot path, for information `PostToolUse` already carries.
- **Payloads carrying `agent_id` are ignored.** That field marks a subagent, and
  a subagent finishing is not your turn ending — otherwise every Task call would
  flash the pane green mid-turn.

## Installing the hooks

```sh
kaku-tab install-hooks          # --dry-run to see the merge first
```

This merges into `~/.claude/settings.json`, keeping every other key and every
hook you wrote yourself, and backs the old file up to
`settings.json.kaku-tab.bak`. Restart any running agent session to pick it up.

One file covers both CLIs: Devin CLI treats `~/.claude/settings.json` as one of
its user-level hook sources, and `kaku-tab hook` tells the two apart from the
environment (`DEVIN_PROJECT_DIR` vs `CLAUDE_PROJECT_DIR`) rather than from an
argument. The block is the union of both event sets; each CLI ignores the events
it does not have.

The command is written in shell form with an explicit `exec`:

```json
{ "type": "command", "command": "exec '/path/to/kaku-tab' hook" }
```

Not Claude Code's exec form (`args`), for two reasons. Devin CLI's hook schema
has no `args` field, so an exec-form entry there would invoke the binary with no
arguments — and kaku-tab with no arguments opens the picker. And `exec` replaces
the wrapping shell, which makes the hook's parent the agent itself: that is the
pid the record stores to know when the agent has died. **Re-run
`install-hooks` if you move the binary**; the path is absolute.

`kaku-tab hook` is inert by contract. It never writes to stdout and never exits
non-zero, because on `PermissionRequest` and the `PreToolUse` family stdout is a
decision channel and a non-zero exit blocks the call — a status reporter that got
either wrong would start silently vetoing your own tool calls.

## In the picker

Two cells sit between the status glyph and the window index. The first says
*which* agent, the second says what it wants:

| | Agent | | State |
|---|---|---|---|
| `` | Claude Code | `` | blocked asking permission |
| `󰚩` | Devin CLI | `` | blocked on a question |
| | | `` | finished a turn |
| | | `` | the turn failed |
| | | `󰔟` | working |

Splitting identity from state means neither has to be inferred from the other's
colour. Identity takes mauve and cyan, which no state uses, so the two halves
never read as one gradient. `busy` is the only muted state, deliberately: it is
the one thing here you do not owe a response to, and a column that shouted on
every working agent is one you would learn to ignore.

A window row shows the most actionable agent among its panes, and a session
header the most actionable among its windows, so a pane blocked three windows
deep is visible without unfolding anything. Switch to pane mode (<kbd>^p</kbd>)
for the exact pane.

The two glyphs are separated by a space — flush against each other they read as
one smudged symbol, which defeats the point of splitting them. The column is
reserved on every row of a table that has any agent at all, and dropped entirely
from one that has none: an indicator drawn only where there is an agent would
shift every other column on those rows and nowhere else. Every glyph is pinned
to one display cell by a test; a double-width one would break the table's column
budget.

Nothing on screen says what a glyph means, so the footer spells out the selected
row's agent in words — `claude · waiting for permission · 2m ago` — on whichever
row the cursor is on.

<kbd>^a</kbd> filters to windows with an agent that wants you — `perm`, `ask`,
`done` or `err`, but not `busy`. Typing an agent's name in the search box works
too; the name is part of each row's match text without being drawn.

## The status-bar counter

Two pills at the far right of `status-right`:

```
 󰂚 1   󰚩 3
```

- **󰂚 how many want you** — waiting, finished, or failed
- **󰚩 how many agents are open** — Claude Code and Devin CLI together

Notifications lead: that is the number you scan for, and the one that changes.
The open count behind it is context for it.

The notification pill stays drawn at zero, greyed rather than hidden. A count
that vanished would shift the pill beside it every time an agent finished, which
is exactly when you are looking at it. The whole segment disappears when no
agent is running at all.

```tmux
set -g @kaku-tab-agents 'on'
set -g status-interval  5
```

Opt-in, and off by default, because it appends to `status-right` — which most
people compose by hand. The plugin appends after whatever you have already set,
so it lands last.

### Putting it somewhere else

Appending is only the default, and the end of `status-right` is the far right of
the bar. To choose the position — to lead with the pills and keep a battery
module rightmost, say — place the segment yourself and leave `@kaku-tab-agents`
off, so the plugin does not append a second copy:

```tmux
set -g  status-right "#(kaku-tab agents --format tmux)"
set -ag status-right "#{E:@catppuccin_status_session}"
set -agF status-right "#{E:@catppuccin_status_battery}"

set -g @kaku-tab-agents 'off'
```

Plain `-g`/`-ag`, never `-F`: `-F` expands formats at load time, which would run
the `#()` once and freeze its output instead of leaving it for the status bar to
re-run on each redraw.

It draws catppuccin's own module shape — rounded separator, icon on its own
colour, value on the shared module background — resolved from the live `@thm_*`
palette, so it sits flush against the modules beside it. Without catppuccin
loaded it falls back to plain terminal colour names and still renders. It is
*not* a catppuccin module, because a real module always paints its icon and
separators, including around an empty value — which is what this is most of the
time.

| Option | Default | Meaning |
|---|---|---|
| `@kaku-tab-agent-color` | `@thm_mauve` | pill colour for the open count |
| `@kaku-tab-notify-color` | `@thm_peach` | pill colour when something wants you |
| `@kaku-tab-agent-icon` | `󰚩` | icon for the open count |
| `@kaku-tab-notify-icon` | `󰂚` | icon for the notification count |

Refresh has two halves. `status-interval` is only the ceiling on staleness — the
hook calls `refresh-client -S` on every attached client the moment an agent
changes state, so the count moves as it happens rather than up to five seconds
later.

For a per-window badge in the window list, `kaku-tab agents --refresh` writes
`@kt_agent_win` on each window, usable in `window-status-format` as
`#{@kt_agent_win}`.

## From the shell

```sh
kaku-tab agents                  # which pane, which agent, what state, how long
kaku-tab agents --format tmux    # the status segment
kaku-tab agents --refresh        # write the per-window rollup
kaku-tab resolve                 # the full join, agent column included
```

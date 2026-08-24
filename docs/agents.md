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

> **`@kt_agent` and `@kt_agent_msg` are only ever set with `-p`.** From `tmux(1)`: *"Pane options
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

A window row shows the most actionable agent among its panes, so a pane blocked
two panes deep is visible without switching to pane mode (<kbd>^p</kbd>) to find
which one.

Session headers do **not** draw the glyphs. A header inherits them from its
children and the child carrying them is the very next line, so for a one-window
session the pair was drawn twice, one row apart. The header still holds the
record — fold a session, rest on it, and the agent box tells you what is going
on inside without unfolding anything.

The two glyphs are separated by a space. Flush against each other they read as
one smudged symbol, which defeats the point of splitting identity from state.

The column is a fixed three cells and reserved on every row, agent or not: an
indicator drawn only where there is an agent would shift every other column on
those rows and nowhere else. Every glyph is pinned to one display cell by a
test — a double-width one would break the table's column budget.

### The agent box

Nothing on screen says what a glyph means, so moving onto a row with an agent
opens a box below the list — and moving off it closes the box again. A list with
no agents in it looks exactly as it did before any of this existed.

```
╭─  claude ───────────────────────────────────────────────╮
│ waiting for permission · 2m ago                          │
│ Bash: git push origin main --force-with-lease            │
╰──────────────────────────────────────────────────────────╯
```

The second line is what the agent is actually doing, taken from the hook payload
that set the state. Not every event carries one:

| State | Message, and where it comes from | Claude Code | Devin CLI |
|---|---|---|---|
| `busy` | the prompt you gave it — `UserPromptSubmit.prompt` | yes | yes |
| `perm` | the tool and its argument — `PermissionRequest`, e.g. `Bash: git push` | yes | yes |
| `done` | the reply that ended the turn — `Stop.last_assistant_message` | yes | **no** |
| `err`  | the error type — `StopFailure.error_type` | yes | — |
| `ask`  | the question — `Elicitation` | yes | — |

Devin CLI carries a message for the two states that matter most — what it is
working on, and what it wants to run — but not for `done`: its `Stop` payload
reports `stop_hook_active` and no reply text, so a finished Devin turn shows the
state and the age with no line under them. `err` and `ask` never arise at all,
since Devin has neither `StopFailure` nor `Elicitation`.

Separately from any of this, a `busy` record untouched for 30 minutes reads as
`no activity`. Every hook event refreshes the timestamp, so one that old has not
made a tool call in half an hour.

Only the **first line that says something** is kept. An assistant reply is a
whole markdown document — headings, code blocks, sometimes a rendered box of its
own — and flattening the lot onto one line produced nonsense whose box-drawing
characters landed inside this box and read as a rendering fault. Blank lines,
code fences and pure line-art rules are skipped, a leading markdown marker is
shed so the text starts at a word, and any Box Drawing or Block Elements rune
that survives is dropped.

What is left wraps to at most three lines, elided beyond that, and is capped at
300 characters when stored.

The message is kept in a **second pane option**, `@kt_agent_msg`, rather than as
a field of `@kt_agent`: it is free text, and the record's format depends on
every field coming from a fixed alphabet. It is stored tagged with the state it
describes and dropped on read when the two disagree — which is what stops a
permission request from still being displayed after your approval has moved the
pane back to `busy`. Control characters are stripped before it is stored, since
it is read back through a `\x1f`-separated format string.

Events that carry no text leave the option alone, so a prompt survives a whole
turn of tool calls.

> This is the one part of kaku-tab that stores what you typed. Prompts and tool
> arguments land in a tmux option, readable by anything that can talk to the
> server. `set -g @kaku-tab-agent-message 'off'` keeps the box and drops the
> message line.

<kbd>^a</kbd> filters to windows with an agent that wants you — `perm`, `ask`,
`done` or `err`, but not `busy`. Typing an agent's name in the search box works
too; the name is part of each row's match text without being drawn.

## Jumping to what wants you

```tmux
set -g @kaku-tab-agent-key 'M-a'
```

Jumps straight to the agent that most wants you, no picker in the way. Press it
again to move to the next one, and again to wrap — so a row of blocked sessions
is walked with one key rather than four.

The order is the same ranking the picker uses: blocked before failed before
finished, and oldest first within a rank, since the one that has been waiting
longest is the one being kept waiting. With nothing waiting it says so and does
nothing.

## A badge in the window list

`@kt_agent_win` carries the most actionable agent in each window, refreshed
whenever a pane changes state. Use it in your window format:

```tmux
set -g window-status-format         "#{?@kt_agent_win,#{@kt_agent_win} ,}#I:#W"
set -g window-status-current-format "#{?@kt_agent_win,#{@kt_agent_win} ,}#I:#W"
```

Only the windows whose rollup actually changed are written, because this runs
from a hook and a `set-option` per window per event would be the most expensive
thing in the path.

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
re-run on each redraw. And `#()` runs with the PATH the tmux *server* inherited
at start, which is often not your shell's — a server launched by launchd or
systemd typically has neither `/usr/local/bin` nor `~/go/bin`. Use an absolute
path if the bare name does not resolve.

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

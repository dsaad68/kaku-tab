# kaku-tab

[![CI](https://github.com/dsaad68/kaku-tab/actions/workflows/ci.yml/badge.svg)](https://github.com/dsaad68/kaku-tab/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/dsaad68/kaku-tab.svg)](https://pkg.go.dev/github.com/dsaad68/kaku-tab)

A tmux plugin that maps **one tmux window to one terminal tab**, and gives you a
picker over every window showing which tab it lives in.

Press <kbd>Alt</kbd>+<kbd>L</kbd>: jump to the window if it's already on screen,
or open it if it isn't — without hunting through tabs to find where a session
ended up.

Works with [Kaku](https://github.com/tw93/Kaku) and [WezTerm](https://wezterm.org)
(Kaku is a WezTerm fork and re-exports the same mux CLI).

```
╭── tmux ⇄ kaku ──────────────────────────────────────────────────────────────────╮
│  kaku-tab ❯                                                                17/17 │
├──────────────────────────────────────────────────────────────────────────────────┤
│➤  ▾ api  3 windows  ⟦kaku 15⟧                                                    │
│    ├ ◍ 1              nvim         2p    ~/src/api                  ⟦hidden 15⟧ │
│    ├ ◍ 2              zsh          2p    ~/src/api/cmd              ⟦hidden 15⟧ │
│    └ ● 3              just         2p !  ~/src/api                    ⟦kaku 15⟧ │
│   ▾ web  1 window  ⟦kaku 16⟧                                                     │
│    └ ● 1              vite         2p    ~/src/web           ⟦kaku 16⟧ <- here  │
│   ▾ scratch  2 windows  ⟦ detached ⟧                                             │
│    ├ ○ 1              zsh          1p    ~                          ⟦ new tab ⟧ │
│    └ ○ 2              htop         1p    ~                          ⟦ new tab ⟧ │
│                                                                                  │
│  enter switch · ^/ show preview · ^t new tab · tab fold (S-tab all) · ^p panes   │
│  ^e hide detached · ^r rename · ^x kill · ^d detach · ^u clear                   │
╰──────────────────────────────────────────────────────────────────────────────────╯
```

`●` visible in a tab · `◍` session has a tab, this window is hidden ·
`○` detached · `!` activity · `z` zoomed

Sessions that have a terminal tab are listed first — those are the ones you
switch between — with detached sessions below.

## Why

tmux stores "which window is displayed" on the **session**, not the client. Two
tabs attached to one session therefore always show the same window: switch one
and the other follows. So "one window per tab" isn't something you can arrange
by hand.

kaku-tab uses **grouped sessions**. When you ask for a second window of an
already-attached session, it creates a satellite (`api~kaku2`) that shares the
session's window list but owns its own current-window. Satellites are created
only when needed, hidden from the picker, and reaped automatically.

See [docs/design.md](docs/design.md) for how the tmux ⇄ terminal join works.

## Requirements

| | |
|---|---|
| tmux | 3.2+ (needs `display-popup`) |
| terminal | Kaku, or WezTerm — auto-detected |
| Go | 1.21+ to build (not needed if you install a prebuilt binary) |

## Install

### TPM

```tmux
set -g @plugin 'dsaad68/kaku-tab'
```

Then <kbd>prefix</kbd>+<kbd>I</kbd>. The plugin builds its binary on first load
if Go is available.

### Prebuilt binary

Grab the archive for your platform from the [releases
page](https://github.com/dsaad68/kaku-tab/releases), unpack it, and put
`kaku-tab` on your `PATH`:

```sh
tar xzf kaku-tab_<version>_darwin_arm64.tar.gz
install kaku-tab /usr/local/bin/
```

That installs the binary only; tmux still needs the plugin entry point, so pair
it with the TPM line above or a checkout. Nothing gets built — the plugin finds
`kaku-tab` on `PATH`.

The binary is not notarized, so macOS quarantines it on first run. Clear the
attribute with `xattr -d com.apple.quarantine /usr/local/bin/kaku-tab`.

### Manual

```sh
git clone https://github.com/dsaad68/kaku-tab ~/.tmux/plugins/kaku-tab
cd ~/.tmux/plugins/kaku-tab && make build
```

```tmux
# ~/.tmux.conf — keep near the bottom
run-shell '~/.tmux/plugins/kaku-tab/kaku-tab.tmux'
```

### Go install

```sh
go install github.com/dsaad68/kaku-tab/cmd/kaku-tab@latest
```

The plugin picks up `kaku-tab` from `PATH` when there's no local build, so this
works with a bare checkout of `kaku-tab.tmux`.

Reload tmux (<kbd>prefix</kbd>+<kbd>r</kbd>, or `tmux source-file ~/.tmux.conf`)
and press <kbd>Alt</kbd>+<kbd>L</kbd>.

## Keys

| Key | Action |
|---|---|
| <kbd>↑</kbd> <kbd>↓</kbd> | move (<kbd>Ctrl</kbd>+<kbd>K</kbd> / <kbd>Ctrl</kbd>+<kbd>J</kbd> too) |
| <kbd>PgUp</kbd> <kbd>PgDn</kbd> <kbd>Home</kbd> <kbd>End</kbd> | move by a screenful, or to either end |
| <kbd>Enter</kbd> | switch to that window, reusing the session's existing tab |
| <kbd>Ctrl</kbd>+<kbd>T</kbd> | force a **new** tab, so two windows of one session show at once |
| <kbd>Tab</kbd> | fold/unfold a session (works from a child row too) |
| <kbd>Shift</kbd>+<kbd>Tab</kbd> | fold or unfold every session |
| <kbd>Ctrl</kbd>+<kbd>P</kbd> | toggle window ⇄ pane rows |
| <kbd>Ctrl</kbd>+<kbd>E</kbd> | hide/show detached sessions — leaves only what's on screen |
| <kbd>Ctrl</kbd>+<kbd>A</kbd> | show only windows where an agent is waiting on you |
| <kbd>Ctrl</kbd>+<kbd>/</kbd> | show/hide the preview — the popup resizes with it |
| <kbd>Ctrl</kbd>+<kbd>R</kbd> | rename: the **window** on a child row, the **session** on a header |
| <kbd>Ctrl</kbd>+<kbd>X</kbd> | kill window |
| <kbd>Ctrl</kbd>+<kbd>D</kbd> | detach the tab showing this window |
| <kbd>Ctrl</kbd>+<kbd>U</kbd> | clear the query |
| <kbd>Esc</kbd> | cancel |

Typing filters. A session header matches on behalf of its windows, so `api`
shows the session *and* everything under it.

When there are more rows than fit, a scrollbar appears down the right edge —
otherwise a list that continues below the frame looks exactly like one that
ends there. <kbd>Shift</kbd>+<kbd>Tab</kbd> folds every session, and
<kbd>Ctrl</kbd>+<kbd>E</kbd> drops the detached ones, which are usually the
faster ways to get a long list back onto one screen.

<kbd>Ctrl</kbd>+<kbd>E</kbd> is a per-invocation toggle: it resets each time you
open the picker, because a filter that quietly persisted would one day hide half
your sessions with nothing on screen to say why. Set
`@kaku-tab-detached 'off'` if you want it on by default.

### Enter vs Ctrl-T

For a window that is hidden — its session has a tab, but that tab is showing a
different window:

```
before             Enter (reuse)       Ctrl-T (new)
tab 5 → api:3      tab 5 → api:4       tab 5 → api:3
                   (:3 now hidden)     tab 7 → api:4   (satellite)
```

`reuse` keeps your tab count flat and is the default. A window with no client
anywhere always opens a new tab — there is nothing to reuse.

## Configuration

Every option, with defaults, is in
[docs/configuration.md](docs/configuration.md). The common ones:

```tmux
set -g @kaku-tab-key        'M-l'    # picker binding
set -g @kaku-tab-search-key 'M-p'    # optional: scrollback search
set -g @kaku-tab-preview    'off'    # ^/ toggles it
set -g @kaku-tab-sort       'tabs'   # or 'mru' / 'name'
set -g @kaku-tab-detached   'on'     # 'off' starts with detached hidden; ^e toggles
set -g @kaku-tab-ignore     'popup'  # sessions to hide, comma-separated
```

With `@kaku-tab-sort 'mru'` the list is ordered by what you most recently
switched to, and the window you are in now is pushed one place down — so
<kbd>Alt</kbd>+<kbd>L</kbd> <kbd>Enter</kbd> toggles back to where you just
were, alt-tab style.

## Agents

Claude Code and Devin CLI sessions show up as a column in the picker — one glyph
for which agent, one for what it wants: working, blocked on a permission prompt,
asking you something, finished, or failed. Moving onto one opens a box saying
what it is actually doing. And the tmux status bar gets two counters:

```
 󰂚 5   󰚩 5      5 want you · 5 open
```

The agents report themselves: each CLI's lifecycle hooks run `kaku-tab hook`,
which records the state on the pane it inherited via `$TMUX_PANE`. Nothing is
guessed from the process table — `#{pane_current_command}` says `node` for
Claude Code, and no process name can tell "thinking" from "waiting on you".

### Setting it up

**1. Install the hooks.** One block in `~/.claude/settings.json` serves both
CLIs — Devin CLI reads that file too, and `kaku-tab hook` tells them apart from
the environment. Idempotent, and it leaves every other key and every hook of
your own alone:

```sh
kaku-tab install-hooks          # --dry-run to see the merge first
```

Restart any running agent session to pick it up. **Nothing appears until this is
done** — with no hooks publishing state there is nothing to count.

**2. Turn on the status-bar counter:**

```tmux
set -g @kaku-tab-agents 'on'
set -g status-interval  5
```

**3. Optional — a key to jump to whatever wants you, and a notification:**

```tmux
set -g @kaku-tab-agent-key    'M-a'   # again for the next one
set -g @kaku-tab-agent-notify 'on'    # only on the transition into waiting
```

### The status-bar counter

| Pill | Counts |
|---|---|
| 󰂚 | how many agents **want you** — waiting, finished, or failed |
| 󰚩 | how many agents are **open** at all |

The bell greys out at zero rather than disappearing, so the pill beside it does
not shift every time an agent finishes. The whole segment prints nothing at all
when no agent is running.

**It needs a Nerd Font** for the two glyphs (`nf-md-bell` and `nf-md-robot`).
Without one you will get tofu — swap them for anything, including plain text:

```tmux
set -g @kaku-tab-notify-icon '!'
set -g @kaku-tab-agent-icon  'AI'
```

**Placement.** `@kaku-tab-agents 'on'` appends the pills to the end of
`status-right`, which is the far right of the bar. To put them anywhere else,
leave the option `off` and place the command yourself:

```tmux
set -g  status-right "#(kaku-tab agents --format tmux)"
set -ag status-right "#{E:@catppuccin_status_session}"
set -agF status-right "#{E:@catppuccin_status_battery}"

set -g @kaku-tab-agents 'off'
```

Plain `-g`/`-ag`, never `-F`: `-F` expands formats at load time, which would run
the `#()` once and freeze the count instead of leaving it for the status bar to
re-run.

`#()` runs with the PATH the **tmux server** inherited when it started, which is
often not your shell's — a server launched by launchd or systemd typically has
neither `/usr/local/bin` nor `~/go/bin`. Use an absolute path if the bare name
does not resolve:

```tmux
set -g status-right "#(/full/path/to/kaku-tab agents --format tmux)"
```

**Theming.** The pills are drawn in catppuccin's own status-module shape — a
rounded separator, the icon on its own colour, the value on the shared module
background — resolved from the live `@thm_*` palette, so they sit flush against
whatever modules you already have. Without catppuccin loaded they fall back to
plain terminal colour names and still render. Override the two colours with:

```tmux
set -g @kaku-tab-notify-color '#fab387'   # default: @thm_peach
set -g @kaku-tab-agent-color  '#cba6f7'   # default: @thm_mauve
```

They are deliberately *not* a catppuccin module: a real module always paints its
icon and separators, including around an empty value — which is what this
segment is most of the time.

**Refresh.** `status-interval` is only the ceiling on staleness. The hook calls
`refresh-client -S` on every attached client the moment an agent changes state,
so the count moves as it happens rather than up to five seconds later.

### Elsewhere

A badge in the window list, and the state in the terminal tab title, so a
blocked agent is visible with the picker closed:

```tmux
set -g window-status-format         "#{?@kt_agent_win,#{@kt_agent_win} ,}#I:#W"
set -g window-status-current-format "#{?@kt_agent_win,#{@kt_agent_win} ,}#I:#W"
```

Full reference — every state, every option, and how the state is stored — in
[docs/agents.md](docs/agents.md).

## Scrollback search

Set `@kaku-tab-search-key` to get a live grep over every pane's scrollback in
every session — then jump straight to the hit, opening a tab if that window
isn't visible. Scrollback is captured once, concurrently, so typing filters
instantly rather than re-grepping.

## Other commands

The binary is useful on its own:

```sh
kaku-tab resolve                  # print the window ⇄ tab join (debugging)
kaku-tab restore [--windows]      # open a tab per detached session
kaku-tab prune                    # reap orphaned satellite sessions
kaku-tab titles [--dry-run]       # retitle tabs after their tmux window
kaku-tab agents                   # which pane each Claude Code / Devin session is in
kaku-tab go-agent                 # jump to the agent that wants you
kaku-tab install-hooks            # register the agent hooks with both CLIs
```

`restore` pairs well with
[tmux-continuum](https://github.com/tmux-plugins/tmux-continuum): continuum
brings the sessions back, this brings the tabs back.

## Docs

- [Design](docs/design.md) — how the join works, and why grouped sessions
- [Agents](docs/agents.md) — Claude Code / Devin CLI state in the picker and status bar
- [Configuration](docs/configuration.md) — every option
- [Troubleshooting](docs/troubleshooting.md)
- [llm.txt](llm.txt) — a setup runbook written for an AI agent to execute, with
  a verification after every step and the conflict checks that have actually
  bitten someone

## Development

```sh
make build             # bin/kaku-tab
make test              # go vet + go test
make lint              # golangci-lint, same config as CI
make cover             # tests + the thresholds in .testcoverage.yml
make release-snapshot  # build the release artifacts, publish nothing
```

Tests run against recorded fixtures — no tmux server or terminal required.

## License

[MIT](LICENSE)

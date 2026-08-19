# Configuration

All options are global tmux options, set before the plugin loads.

```tmux
set -g @kaku-tab-key 'M-l'
set -g @plugin 'dsaad68/kaku-tab'
```

## Keys and layout

| Option | Default | Meaning |
|---|---|---|
| `@kaku-tab-key` | `M-l` | picker binding |
| `@kaku-tab-search-key` | *(unset)* | scrollback search binding; unset means no binding |
| `@kaku-tab-popup-size` | `90%,85%` | popup `width,height` with the preview shown |
| `@kaku-tab-popup-size-compact` | `60%,70%` | popup size with the preview hidden |
| `@kaku-tab-preview` | `off` | start with the preview pane; <kbd>Ctrl</kbd>+<kbd>/</kbd> toggles |
| `@kaku-tab-tree` | `on` | group windows under their session; `off` gives a flat list |

### Why `M-l`

It has to be free in three places: the tmux root table, your terminal's own
keymap, and macOS dead-key composition. `M-l` is usually free in all three —
Kaku binds <kbd>Cmd</kbd>+<kbd>L</kbd> (not <kbd>Alt</kbd>) for its AI overlay,
and `l` is not a dead key. `M-p` is a good alternative.

Check before choosing:

```sh
tmux list-keys -T root | grep -E '\bM-l\b'      # tmux
```

If your terminal sends composed characters for Option (e.g. `¬` instead of
`ESC l`), tell it to treat Option as Meta. In Kaku/WezTerm:

```lua
config.send_composed_key_when_left_alt_is_pressed = false
config.send_composed_key_when_right_alt_is_pressed = false
```

## Behaviour

| Option | Default | Meaning |
|---|---|---|
| `@kaku-tab-open-mode` | `reuse` | what <kbd>Enter</kbd> does for a hidden window: `reuse` retargets the session's existing tab, `go` opens a new one |
| `@kaku-tab-sort` | `tabs` | list order: `tabs`, `mru`, or `name` — see below |
| `@kaku-tab-detached` | `on` | `off` starts with detached sessions hidden; <kbd>Ctrl</kbd>+<kbd>E</kbd> toggles |
| `@kaku-tab-scope` | `all` | `all`, `session` (current session only), or `group` (current session group) |
| `@kaku-tab-ignore` | *(empty)* | comma-separated session names to hide, e.g. a throwaway popup session |
| `@kaku-tab-satellite-suffix` | `~kaku` | naming for grouped satellite sessions |
| `@kaku-tab-mux-cli` | *(auto)* | force `kaku` or `wezterm` instead of auto-detecting |
| `@kaku-tab-search-depth` | `2000` | scrollback lines per pane indexed by search |

### Sort order

| Value | Order |
|---|---|
| `tabs` | sessions that have a terminal tab first, then the rest; alphabetical within each. The default. |
| `mru` | whatever you most recently switched to, first — sessions and windows both. |
| `name` | plain alphabetical, ignoring whether a session has a tab. |

`mru` is the one to reach for if you bounce between the same two or three
windows: the window you were in *before* this one sits at the top of the list,
so <kbd>Alt</kbd>+<kbd>L</kbd> <kbd>Enter</kbd> is a straight toggle.

The window you are currently in is deliberately pushed one place down. It heads
the history — switching here is what recorded it — and leaving it at the top
would put the cursor on a row whose <kbd>Enter</kbd> does nothing.

Only windows you have switched to *through kaku-tab* are ranked; everything
else falls in behind them in the `tabs` order. So a freshly started tmux server
looks exactly like the default until the history fills in.

The history is kept in a tmux option rather than a file, because tmux window
ids (`@42`) mean nothing outside the server that issued them and a server
option lives exactly that long:

```sh
tmux show-option -gqv @kaku-tab-mru     # most recent first
tmux set-option  -gu @kaku-tab-mru      # forget it
```

## Tab titles

Off by default, because it takes over the tab title — which you may already be
driving yourself.

| Option | Default | Meaning |
|---|---|---|
| `@kaku-tab-titles` | `off` | retitle tabs after the tmux window their client shows |
| `@kaku-tab-title-format` | `%g` | `%s` session · `%g` base session · `%w` window name · `%i` window index |

`%w` is deliberately absent from the default. With tmux `automatic-rename` on,
every window is named after its foreground process, so `%g · %w` produces a tab
bar reading *api · zsh, web · zsh, db · zsh* — repeated on every tab and
identifying nothing. Use `%g:%i` if you want satellite tabs of one session
distinguishable without that noise.

Preview what it would set, changing nothing:

```sh
kaku-tab titles --dry-run
```

> **Warning**
> If your config already has `set-tab-title` hooks, leave this `off` or remove
> them first. Both fire on the same events and will fight over the title.

## Agents

Two counters at the far right of the status bar — how many Claude Code and Devin
CLI sessions are open, and how many of them are waiting on you — plus an agent
column in the picker. See [agents.md](agents.md).

| Option | Default | Meaning |
|---|---|---|
| `@kaku-tab-agents` | `off` | append the agent counter to `status-right` |
| `@kaku-tab-agent-color` | `@thm_mauve` | pill colour for the "agents open" count |
| `@kaku-tab-notify-color` | `@thm_peach` | pill colour when something wants you |
| `@kaku-tab-agent-icon` | 󰚩 | icon for the "agents open" count |
| `@kaku-tab-notify-icon` | 󰂚 | icon for the notification count |

Off by default because it appends to `status-right`, which most people compose
by hand. Turning it on is two lines, plus installing the hooks that feed it:

```tmux
set -g @kaku-tab-agents  'on'
set -g status-interval   5
```

```sh
kaku-tab install-hooks
```

`status-interval` is only the ceiling on staleness — the hook calls
`refresh-client -S` the moment an agent changes state, so the count moves as it
happens.

## Full example

```tmux
set -g @kaku-tab-key            'M-l'
set -g @kaku-tab-search-key     'M-p'
set -g @kaku-tab-preview        'off'
set -g @kaku-tab-open-mode      'reuse'
set -g @kaku-tab-ignore         'popup,scratch'
set -g @kaku-tab-popup-size     '90%,85%'
set -g @kaku-tab-popup-size-compact '60%,70%'
set -g @kaku-tab-agents         'on'

set -g @plugin 'dsaad68/kaku-tab'
```

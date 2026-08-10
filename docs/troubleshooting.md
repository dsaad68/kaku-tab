# Troubleshooting

Start here:

```sh
kaku-tab resolve
```

That prints the join the whole plugin is built on — one row per tmux window
with its status, the tab it lives in, and the client session behind that tab.
If this is wrong, everything else will be.

```
VISIBLE          @46   api      idx=2  tab=8   gui=0  client=api        nvim
ATTACHED_HIDDEN  @29   api      idx=1  tab=8   gui=0  client=api        zsh
DETACHED         @70   scratch  idx=1  tab=-   gui=-  client=-          htop
```

## Nothing happens on the key

```sh
tmux list-keys -T root | grep -E '\bM-l\b'
```

- **No output** — the plugin did not load. Check that `run-shell` /
  `set -g @plugin` comes *after* anything that resets bindings, and reload.
- **Bound to something else** — pick a free key, see
  [configuration](configuration.md#why-m-l).
- **Bound correctly but nothing opens** — the binary is missing. The plugin
  builds on first load only when Go is present; otherwise install it:
  `go install github.com/dsaad68/kaku-tab/cmd/kaku-tab@latest`.

If your terminal produces a character (e.g. `¬`) instead of triggering the key,
Option is being composed rather than sent as Meta — see
[configuration](configuration.md#why-m-l).

## Every window says `⟦ new tab ⟧`

The join found no terminal tabs. Check the mux CLI is reachable:

```sh
kaku cli list --format json | head    # or: wezterm cli list --format json
```

If that fails, the picker degrades to a plain tmux window switcher —
still usable, but with no tab column. Force a specific CLI with
`@kaku-tab-mux-cli`.

This is also expected for tmux clients that are *not* inside Kaku/WezTerm (an
SSH session, another terminal): they have no tab to point at.

## Jumping goes to the wrong tab

Almost always a stale `WEZTERM_PANE`. kaku-tab deliberately does not use it —
the value is captured into the tmux session environment when the session is
created and then goes stale, so it routinely names a tab that no longer holds
that session. If something in *your* config uses `$WEZTERM_PANE` to target
tabs, that is the likely culprit.

kaku-tab joins on the tty instead (`tty_name` ↔ `client_tty`), which is exact.

## Sessions named `foo~kaku2` are piling up

Those are satellites — grouped sessions that let two windows of one session
show in two tabs. They should be reaped automatically when their tab closes.
Force a sweep:

```sh
kaku-tab prune
```

They only appear when you press <kbd>Ctrl</kbd>+<kbd>T</kbd> on a window whose
session already has a tab. If you never want them, use <kbd>Enter</kbd>
(`reuse`), which retargets the existing tab instead.

Satellites are never listed in the picker; if you see one there, please open an
issue.

## The popup is too small, or the preview is cramped

The popup opens at `@kaku-tab-popup-size-compact` without a preview and
`@kaku-tab-popup-size` with one. <kbd>Ctrl</kbd>+<kbd>/</kbd> switches between
them — tmux cannot resize an open popup, so the picker reopens at the other
size, carrying your query and cursor across.

Below ~120 columns the preview moves under the list instead of beside it.

## Tab titles flicker or fight

Two things are setting them. Either turn off `@kaku-tab-titles`, or remove your
own `set-tab-title` hooks. See
[configuration](configuration.md#tab-titles).

## The picker is empty

`@kaku-tab-scope` may be limiting it to the current session or group, or
`@kaku-tab-ignore` may be hiding everything. Check with:

```sh
tmux show-options -g | grep kaku-tab
```

## Reporting a bug

Include:

```sh
kaku-tab version
tmux -V
kaku cli list --format json | head -20   # or wezterm
kaku-tab resolve
```

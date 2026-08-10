# Design

How kaku-tab knows which terminal tab a tmux window is showing in, and why it
sometimes creates a second tmux session to answer that question.

## The join

Three sources are merged on every invocation:

```
terminal panes            tmux clients              tmux windows
(kaku cli list --json)    (list-clients)            (list-windows -a)
   tty_name  ═══════════════ client_tty
   tab_id                    session ──────────────── session_name
   window_id (GUI)           window_id (current) ──── window_id
```

The key is the **tty**. `kaku cli list --format json` reports `tty_name` for
each pane; `tmux list-clients` reports `client_tty` for each client. For a tmux
client running inside a terminal pane, they are the same string. Join on it and
every tmux session learns which tab it lives in.

Each client also reports the window its session is *currently showing*, which
separates "this window is on screen" from "this window's session is on screen".

That yields three states, which are the whole user-facing model:

| Status | Meaning | <kbd>Enter</kbd> does |
|---|---|---|
| `VISIBLE` | a tab is showing exactly this window | focus that tab |
| `ATTACHED_HIDDEN` | the session has a tab, showing a different window | retarget that tab |
| `DETACHED` | no client anywhere | open a new tab |

`kaku-tab resolve` prints this table.

### Why not `$WEZTERM_PANE`

The obvious approach is to read the `WEZTERM_PANE` environment variable. It is
wrong here.

The value is captured into the tmux **session environment** when the session is
created, and refreshed only on re-attach. A session that outlives a terminal
restart keeps advertising a pane id belonging to a tab that no longer holds it.
Anything keyed on it silently targets the wrong tab, and does so intermittently,
which is worse.

The tty is assigned by the pty that the terminal created for that pane. It
cannot go stale without the client dying.

## The hard part: current-window is per-session

tmux stores "which window is displayed" on the **session**, not on the client.
Every client attached to a session shows the same window; switching one switches
all of them.

So two tabs attached to `api` cannot show `api:1` and `api:3` at the same time.
"One window per tab" is not something a user can arrange by hand, and no amount
of `select-window` fixes it.

### Satellites

The fix is a **grouped session**:

```sh
tmux new-session -d -t api -s 'api~kaku2'
```

A grouped session shares the base session's window *list* — new windows appear
in both — while keeping its **own current-window**. So `api` can sit on window
1 while `api~kaku2` sits on window 3, and each can own a tab.

kaku-tab creates one only when it has to: opening a window whose session has no
client attaches the base session directly. Satellites appear only when you ask
for a *second* tab on an already-attached session (<kbd>Ctrl</kbd>+<kbd>T</kbd>).

Three rules keep them invisible:

- **Never listed.** A satellite's windows *are* the base session's windows, so
  listing both would duplicate every row once per group member.
- **Attributed to the base.** A client sitting on a satellite still means "this
  session has a tab", so it registers under the base name. Otherwise the base
  reads `DETACHED` and its hidden windows spawn new tabs instead of retargeting.
- **Retargeted by client session.** When switching a tab to another window, the
  target must be the session that client is actually attached to — which may be
  the satellite. Aiming at the base focuses the right tab but leaves it showing
  the wrong window, because current-window is per-session.

### Cleanup

Satellites are reaped when their tab closes (`attach` kills its own session on
clean detach) and swept on every picker run, which covers a hard tab close or a
terminal crash.

`destroy-unattached` is deliberately **not** used: a satellite is briefly
detached between creation and attach, so that option can destroy it out from
under us. Pruning on demand has no such race.

## Targeting rules

Two rules that are easy to get wrong:

- **Windows are identified by `window_id` (`@42`), never `session:index`.**
  With `renumber-windows on`, indices shift whenever a window is closed.
- **Every target is session-qualified** (`api:@42`). A bare `@42` is ambiguous
  once a grouped session shares the window, and tmux resolves it to an arbitrary
  member of the group.

## The popup

The picker needs a pty, so it runs under `display-popup -E`; `run-shell` has no
terminal and Bubble Tea cannot draw there.

`#{client_tty}` is expanded by tmux against the *invoking client* and passed in
as an argument. A popup has no client of its own, so this is the only exact way
for the picker to learn which tab it was launched from.

### Resizing

tmux cannot resize an open popup — `display-popup` is the only popup command and
its `-w`/`-h` are fixed at creation. Toggling the preview therefore closes and
reopens the popup at the other size, carrying query, cursor, fold state and pane
mode through a temp file so the round trip is invisible.

This works because `display-popup -E` **blocks** until the popup exits, letting
`kaku-tab popup` loop around it. That wrapper must run detached (`run-shell -b`)
or the blocking call would stall tmux's command queue.

## Rendering

The picker owns its UI rather than driving an external fuzzy finder, which buys
three things:

- **Search text is separate from display text.** A session header matches on
  behalf of its children, so child rows can omit the session name entirely and
  still be findable by session. Tools whose match field *is* the display string
  cannot do this — hiding the name there means typing it matches only the
  header, and the children disappear with it.
- **Real collapsible groups.**
- **Widths measured in display cells**, so nerd-font glyphs and CJK line up.

Two invariants the tests pin, both learned the hard way:

- Column widths are computed once for the whole table. Deriving them from each
  row's own badge gives every row a different layout.
- Anything that may carry ANSI styling is measured with a width function that
  understands escapes. A rune-width function counts escape bytes as printable,
  which silently narrows whichever row is selected.

## Layout

```
cmd/kaku-tab/     subcommands
internal/model/   shared types; satellite naming rules
internal/resolve/ the join — everything else is presentation
internal/tmux/    tmux CLI wrapper
internal/kaku/    mux CLI wrapper (kaku or wezterm)
internal/action/  jump / retarget / spawn, satellite lifecycle, tab titles
internal/ui/      Bubble Tea picker and scrollback search
```

Inter-process records use `\x1f` as the field separator rather than tab. tmux
window names are routinely empty, and with a whitespace separator an empty field
is indistinguishable from padding — which silently shifts every later field.

## Known gaps

- The mux CLI can activate a tab but has no command to raise a different OS
  window, so cross-window jumps fall back to AppleScript on macOS and do
  nothing elsewhere. An upstream `activate-window --window-id` would fix this.
- Tab titles are driven by shelling out to `set-tab-title` on tmux hooks. A
  lower-latency path exists in principle — tmux can push OSC 1337 user vars
  through `allow-passthrough`, which the terminal's Lua config can react to
  in-process — but it is unimplemented here.

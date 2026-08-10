# Contributing

```sh
make build   # bin/kaku-tab
make test    # go vet + go test
make lint    # gofmt + vet
```

Tests use recorded fixtures, so they run without a tmux server or a terminal.

## Layout

```
cmd/kaku-tab/     subcommands: popup, pick, search, attach, resolve, prune,
                  restore, titles
internal/model/   shared types; satellite naming
internal/resolve/ the tmux ⇄ terminal join — the core, ~180 lines
internal/tmux/    tmux CLI wrapper
internal/kaku/    mux CLI wrapper (kaku or wezterm)
internal/action/  jump / retarget / spawn, satellite lifecycle, tab titles
internal/ui/      Bubble Tea picker and scrollback search
```

Start with `internal/resolve`. Everything else is presentation or plumbing
around the table it produces, and [docs/design.md](docs/design.md) explains why
it works the way it does.

## Things that will bite you

These are all mistakes already made and fixed here; the tests pin them.

- **Measure display cells, not bytes or runes.** `↵` is 3 bytes and `⇧⇥` is 6.
  A styled string's ANSI escapes count as printable to a naive width function,
  which silently narrows whichever row is selected. Use `ansi.StringWidth` for
  anything that may carry styling and `runewidth` for plain text.
- **Never let a row's own content decide the column budget.** Sizing columns
  from each row's badge gives every row a different layout.
- **Always session-qualify tmux targets** (`sess:@42`). A bare `@42` is
  ambiguous once a grouped session shares the window.
- **Never `set-hook -g`** — it replaces every hook on that event, including the
  user's. Use `-ga`, and remember it appends a duplicate on each config reload.
- **Don't trust `$WEZTERM_PANE`.** It is captured into the tmux session
  environment at creation and goes stale. Join on the tty.

## Pull requests

Run `make lint test` first. If you fix a rendering bug, add a test that pins
the invariant rather than the output — `internal/ui/ui_test.go` has examples.

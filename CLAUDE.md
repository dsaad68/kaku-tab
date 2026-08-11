# kaku-tab

A tmux plugin that maps one tmux window to one terminal tab (Kaku / WezTerm).
Go binary + a thin `kaku-tab.tmux` plugin entry point.

## Commands

```sh
make build   # bin/kaku-tab
make test    # go vet + go test
make lint    # golangci-lint, same .golangci.yml as CI
make cover   # tests + coverage thresholds from .testcoverage.yml
```

Tests use recorded fixtures — no tmux server or terminal needed.

CI runs test / lint / coverage / goreleaser-check. The coverage thresholds are
a ratchet set just under the current numbers: if a change drops
`internal/resolve`, that is the signal, not a nuisance.

## Where to start

`internal/resolve` is the core: it joins the terminal's pane list to tmux's
clients and windows on the **tty**. Everything else is presentation or plumbing
around the table it produces. Read [docs/design.md](docs/design.md) first.

## Invariants (all previously broken here; tests pin them)

- Measure **display cells**, not bytes or runes. Use `ansi.StringWidth` for
  anything that may carry ANSI styling, `runewidth` for plain text.
- Column widths are computed once per table, never from a single row's content.
- tmux targets are always session-qualified (`sess:@42`); a bare `@42` is
  ambiguous once a grouped session shares the window.
- Never `set-hook -g` — it replaces the user's hooks. Use `-ga`.
- Never key on `$WEZTERM_PANE`; it goes stale. Join on the tty.

## Verifying changes

`kaku-tab resolve` prints the join. To see the TUI without a real popup, run it
inside a detached tmux session and capture the pane:

```sh
tmux new-session -d -s ui -x 150 -y 30 "$PWD/bin/kaku-tab pick '' ui"
sleep 2 && tmux capture-pane -p -t ui
```

## Research

Use the DeepWiki MCP for package/library questions, and ask follow-ups rather
than assuming when docs are thin.

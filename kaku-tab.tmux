#!/usr/bin/env bash
# kaku-tab — tmux plugin entry point.
#
#   set -g @plugin 'dsaad68/kaku-tab'          # via TPM
#   run-shell '/path/to/kaku-tab/kaku-tab.tmux' # local checkout
#
# Builds the binary on first load when a Go toolchain is available, and
# otherwise falls back to one already on PATH (go install / a release build).

set -u

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$CURRENT_DIR/bin/kaku-tab"

opt() { local v; v="$(tmux show-option -gqv "$1")"; [ -n "$v" ] && printf '%s' "$v" || printf '%s' "$2"; }

KEY="$(opt @kaku-tab-key 'M-l')"
SEARCH_KEY="$(opt @kaku-tab-search-key '')"
TITLES="$(opt @kaku-tab-titles 'off')"
SIZE="$(opt @kaku-tab-popup-size '90%,85%')"
W="${SIZE%%,*}"; H="${SIZE##*,}"

# Build once if we can; a checkout without a prebuilt binary is the common case
# for TPM users.
if [ ! -x "$BIN" ] && command -v go >/dev/null 2>&1; then
  ( cd "$CURRENT_DIR" && go build -o "$BIN" ./cmd/kaku-tab ) >/dev/null 2>&1
fi
[ -x "$BIN" ] || BIN="$(command -v kaku-tab 2>/dev/null || true)"

if [ -z "$BIN" ]; then
  tmux display-message "kaku-tab: no binary and no Go toolchain — see README"
  exit 0
fi

# The picker needs a pty, so it runs under display-popup -E. `run-shell` has no
# terminal and Bubble Tea cannot draw there.
#
# The picker is launched through `kaku-tab popup` rather than display-popup
# directly, because toggling the preview has to reopen the popup at a different
# size — tmux cannot resize one in place. That wrapper blocks, so it must run
# detached (-b) or it would stall tmux's command queue.
#
# #{client_tty} is expanded by tmux against the *invoking client*. A popup has
# no client of its own, so this is the only exact way for the picker to learn
# which terminal tab it was launched from.
tmux bind-key -n "$KEY" run-shell -b "$BIN popup '#{client_tty}' '#{session_name}'"

if [ -n "$SEARCH_KEY" ]; then
  tmux bind-key -n "$SEARCH_KEY" display-popup -E -B -w "$W" -h "$H" \
    "$BIN search '#{client_tty}' '#{session_name}'"
fi

# Window-aware tab titles. Opt-in: it takes over the tab title, which may
# already be driven by your own set-tab-title hooks.
#
# NOTE: -ga (append), never plain -g. `set-hook -g` REPLACES every existing hook
# on that event and would silently delete yours.
if [ "$TITLES" = "on" ]; then
  for ev in after-select-window session-renamed client-attached client-session-changed; do
    tmux set-hook -ga "$ev" "run-shell -b '$BIN titles'"
  done
  "$BIN" titles >/dev/null 2>&1 &
fi

# Agent counter on the right of the status bar. Opt-in: it appends to
# status-right, which most people compose by hand.
#
# The command prints nothing when no agent is running, so the status bar shows no
# stray icon or separator the rest of the time — which is why this is a plain
# #() emitting its own #[fg=...] styling rather than a themed status module.
#
# Refresh has two halves. status-interval is only the ceiling on staleness; the
# `hook` subcommand calls refresh-client -S on every attached client the moment
# an agent changes state, which is what makes the counter feel immediate. Set
# `status-interval` to 5 or so for the backstop.
#
# The guard makes a config reload idempotent: appending unconditionally would
# stack a second copy of the segment every time this file is sourced.
if [ "$(opt @kaku-tab-agents 'off')" = "on" ]; then
  case "$(tmux show-option -gv status-right 2>/dev/null)" in
    *"agents --format tmux"*) ;;
    *) tmux set-option -ag status-right "#($BIN agents --format tmux)" ;;
  esac
fi

# No hook is registered for pruning satellite sessions. `set-hook -ga` appends a
# fresh copy on every config reload and tmux offers no way to remove one by
# content, so it would accumulate duplicates. The picker prunes on every
# invocation and `attach` reaps its own satellite on clean detach.

exit 0

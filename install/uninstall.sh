#!/bin/sh
# Lattice uninstaller (macOS + Linux). Completely removes Lattice from this
# machine and nothing else.
#
# What it does:
#   1. Stops + unregisters the Lattice service(s) — hub AND agent, whichever
#      this box ran:
#        - macOS  launchd:  ~/Library/LaunchAgents/sh.lattice.{hub,agent}.plist
#        - Linux  systemd:  ~/.config/systemd/user/lattice-{hub,agent}.service
#        - nohup fallback:  a pid recorded in ~/.lattice/{hub,agent}.pid
#   2. Deletes the entire ~/.lattice data directory (binary, config.json, the
#      SQLite database, the token, and logs).
#
# What it does NOT touch — by design:
#   - It runs entirely as your user. No sudo, no system directories.
#   - It changes nothing outside Lattice's own paths: not your shell config,
#     not Claude / ~/.claude, not your projects or files, not Tailscale / SSH /
#     Syncthing (Lattice only ever *detected* those — it never configured them).
#   The only directory removed is the literal ~/.lattice.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/shleesauce/lattice/master/install/uninstall.sh | sh
#   # preview everything without changing a thing:
#   curl -fsSL https://raw.githubusercontent.com/shleesauce/lattice/master/install/uninstall.sh | sh -s -- --dry-run
#
# Re-running is safe: it is idempotent and reports "already clean" when there is
# nothing left to remove.
set -eu

DRY=0
case "${1:-}" in
  --dry-run) DRY=1 ;;
  "") : ;;
  *) echo "usage: uninstall.sh [--dry-run]" >&2; exit 2 ;;
esac

say() { echo "lattice-uninstall: $*"; }
# do_run: echo the action under --dry-run, otherwise execute it.
do_run() { if [ "$DRY" = 1 ]; then echo "  would: $*"; else sh -c "$*"; fi; }

# Hard guard: PREFIX must be exactly $HOME/.lattice. We never rm anything else.
PREFIX="$HOME/.lattice"
if [ -z "${HOME:-}" ] || [ "$PREFIX" != "$HOME/.lattice" ]; then
  echo "lattice-uninstall: refusing to run — HOME unset or unexpected data path" >&2
  exit 1
fi

[ "$DRY" = 1 ] && say "DRY RUN — nothing will be changed"
removed_any=0

stop_launchd() { # <label>
  label="$1"
  plist="$HOME/Library/LaunchAgents/$label.plist"
  gui="gui/$(id -u)"
  if [ -f "$plist" ] || launchctl print "$gui/$label" >/dev/null 2>&1; then
    say "stopping launchd service $label"
    do_run "launchctl bootout '$gui/$label' >/dev/null 2>&1 || true"
    do_run "launchctl unload '$plist' >/dev/null 2>&1 || true"
    if [ -f "$plist" ]; then do_run "rm -f '$plist'"; fi
    removed_any=1
  fi
}

stop_systemd() { # <unit-base>
  unit="$1"
  unitfile="$HOME/.config/systemd/user/$unit.service"
  have_systemctl=0
  if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    have_systemctl=1
  fi
  if [ "$have_systemctl" = 1 ] && systemctl --user status "$unit.service" >/dev/null 2>&1; then
    say "stopping systemd --user service $unit"
    do_run "systemctl --user disable --now '$unit.service' >/dev/null 2>&1 || true"
    removed_any=1
  fi
  if [ -f "$unitfile" ]; then
    say "removing unit file $unitfile"
    do_run "rm -f '$unitfile'"
    removed_any=1
  fi
}

case "$(uname -s)" in
  Darwin)
    stop_launchd sh.lattice.hub
    stop_launchd sh.lattice.agent
    ;;
  *)
    stop_systemd lattice-hub
    stop_systemd lattice-agent
    if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
      do_run "systemctl --user daemon-reload >/dev/null 2>&1 || true"
    fi
    ;;
esac

# nohup fallback: stop any process recorded in a pidfile under ~/.lattice.
for pf in "$PREFIX/hub.pid" "$PREFIX/agent.pid"; do
  if [ -f "$pf" ]; then
    pid="$(cat "$pf" 2>/dev/null || true)"
    if [ -n "$pid" ]; then
      say "stopping background process $pid (from $(basename "$pf"))"
      do_run "kill '$pid' >/dev/null 2>&1 || true"
      removed_any=1
    fi
  fi
done

# Remove the entire data directory — the one and only thing we delete.
if [ -d "$PREFIX" ]; then
  say "removing data directory $PREFIX"
  do_run "rm -rf '$PREFIX'"
  removed_any=1
else
  say "no data directory at $PREFIX"
fi

if [ "$removed_any" = 0 ]; then
  say "nothing to remove — Lattice was not installed for this user (already clean)"
else
  say "done — Lattice has been completely removed from this machine."
fi
[ "$DRY" = 1 ] && say "(dry run — re-run without --dry-run to apply)"
exit 0

#!/usr/bin/env bash
set -euo pipefail

# clean-logs.sh — reclaim disk from pm2 log rotation, safely and repeatably.
#
# What it does:
#   - DELETES rotated pm2-logrotate files (hub.err__*.log, hub.out__*.log,
#     watchdog.out__*.log, watchdog.err__*.log) at the repo root.
#   - TRUNCATES (never deletes) the LIVE logs (hub.err.log, hub.out.log,
#     watchdog.out.log, watchdog.err.log) with `: > file`, so a running pm2
#     hub keeps writing to the same open file descriptor — no restart needed.
#
# Safety guarantees:
#   - Refuses to run unless the resolved git root's go.mod is this project
#     (module github.com/shleesauce/lattice), so it can never run in the wrong dir.
#   - NEVER touches lattice.db / -wal / -shm, .lattice-token, scripts/fleet.env,
#     .env*, or any git-tracked file. The only writes are to the explicit log
#     globs/names below. DB sizes are printed read-only as an FYI.
#   - Idempotent and safe to run repeatedly while the hub is live.
#   - --dry-run prints what it would do and changes nothing.

DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
elif [[ $# -gt 0 ]]; then
  echo "usage: $(basename "$0") [--dry-run]" >&2
  exit 2
fi

# Resolve repo root.
ROOT="$(git rev-parse --show-toplevel)"

# SAFETY: only ever operate inside this project.
if [[ ! -f "$ROOT/go.mod" ]] || ! grep -q 'shleesauce/lattice' "$ROOT/go.mod"; then
  echo "refusing to run: $ROOT is not the lattice repo (go.mod missing or wrong module)" >&2
  exit 1
fi

cd "$ROOT"

# Rotated logs to DELETE (explicit globs only).
ROTATED_GLOBS=(hub.err__*.log hub.out__*.log watchdog.out__*.log watchdog.err__*.log)
# Live logs to TRUNCATE (never delete — preserve the hub's open fd).
LIVE_LOGS=(hub.err.log hub.out.log watchdog.out.log watchdog.err.log)

human() { # bytes -> human-readable
  awk -v b="$1" 'BEGIN{
    split("B KB MB GB TB",u," "); i=1;
    while (b>=1024 && i<5){b/=1024;i++}
    printf (i==1 ? "%d %s" : "%.1f %s"), b, u[i]
  }'
}

filesize() { # 0 if missing
  if [[ -f "$1" ]]; then stat -f%z "$1" 2>/dev/null || stat -c%s "$1" 2>/dev/null || echo 0; else echo 0; fi
}

freed=0

echo "lattice clean-logs — root: $ROOT"
if [[ $DRY_RUN -eq 1 ]]; then echo "(dry-run: no changes will be made)"; fi
echo

echo "Rotated logs (DELETE):"
shopt -s nullglob
matched=0
for glob in "${ROTATED_GLOBS[@]}"; do
  for f in $glob; do
    matched=1
    sz="$(filesize "$f")"
    freed=$((freed + sz))
    if [[ $DRY_RUN -eq 1 ]]; then
      echo "  would delete  $f ($(human "$sz"))"
    else
      rm -f -- "$f"
      echo "  deleted       $f ($(human "$sz"))"
    fi
  done
done
shopt -u nullglob
[[ $matched -eq 0 ]] && echo "  (none)"
echo

echo "Live logs (TRUNCATE — preserves the hub's open fd):"
for f in "${LIVE_LOGS[@]}"; do
  if [[ -f "$f" ]]; then
    sz="$(filesize "$f")"
    freed=$((freed + sz))
    if [[ $DRY_RUN -eq 1 ]]; then
      echo "  would truncate $f ($(human "$sz"))"
    else
      : > "$f"
      echo "  truncated     $f (was $(human "$sz"))"
    fi
  else
    echo "  (skip, absent) $f"
  fi
done
echo

# Read-only FYI: DB sizes. NEVER modified by this script.
echo "Database (FYI only — never touched):"
db_any=0
for f in lattice.db lattice.db-wal lattice.db-shm; do
  if [[ -f "$f" ]]; then
    db_any=1
    echo "  $f  $(human "$(filesize "$f")")"
  fi
done
[[ $db_any -eq 0 ]] && echo "  (no lattice.db in this root)"
echo

if [[ $DRY_RUN -eq 1 ]]; then
  echo "Dry-run: would reclaim ~$(human "$freed")."
else
  echo "Done. Reclaimed ~$(human "$freed")."
fi

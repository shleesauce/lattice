#!/usr/bin/env bash
# Lattice fleet watchdog - "keep everything on; recover or reach out".
#
# Runs on the hub host under pm2. Every CHECK_INTERVAL it asks the hub which
# agent-backed machines are stale/offline, then tries to bring each back using
# that machine's OWN persistence primitive over SSH:
#   macOS : launchctl kickstart -k gui/<uid>/sh.lattice.agent   (re-arms launchd)
#   windows: schtasks /run /tn LatticeAgent                     (re-arms the task)
# If a machine can't be recovered after RECOVER_TRIES consecutive cycles, it
# pushes ONE alert to ntfy (the same topic the phone already subscribes to) and
# goes quiet until the machine recovers - then sends a single "recovered" note.
#
# Why a script on the hub host (not in the hub binary): D2 keeps the hub free of
# any inbound/SSH dependency on leaves; recovery is an operator action, so it
# lives beside deploy-fleet.sh and reuses the same SSH aliases. The agent's own
# reconnect-backoff loop handles transient hub blips; this watchdog handles the
# harder case where the agent PROCESS is gone (crash / headless reboot).
#
# COMPAT: written for bash 3.2 (macOS system bash) - NO `declare -A`, no other
# bash-4 features. Per-machine state lives in files under a state dir, not in
# associative arrays (`declare -A` is a fatal error on bash 3.2).
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
# Fail CLOSED: an empty token would make the watchdog authenticate as nobody and
# silently "guard nothing" (the dangerous direction). Refuse to start instead.
TOKEN="$(cat .lattice-token 2>/dev/null)"
[ -n "$TOKEN" ] || { echo "lattice: no token in .lattice-token" >&2; exit 1; }

# The down-detection sweep pipes hub JSON into inline python3; without it the
# watchdog can't tell who's down and would fail OPEN. Refuse to start.
command -v python3 >/dev/null || { echo "lattice: python3 required" >&2; exit 1; }

# Operator-local fleet config (gitignored). Non-fatal if absent - a daemon must
# not refuse to start; it falls back to env vars / built-in generic defaults.
FLEET_ENV="$(dirname "$0")/fleet.env"
# shellcheck disable=SC1090
[ -f "$FLEET_ENV" ] && . "$FLEET_ENV"
HUB_URL="${LATTICE_HUB_LOCAL:-http://localhost:7400}"
# Fleet map ("ssh_alias:lattice_name:os"); empty if fleet.env is absent - the
# alias/os lookups below then resolve to "" (no recovery path), which is safe.
[ -n "${LATTICE_FLEET+x}" ] || LATTICE_FLEET=()

CHECK_INTERVAL="${LATTICE_WD_INTERVAL:-60}"   # seconds between sweeps
STALE_AFTER="${LATTICE_WD_STALE:-90}"         # secs since lastSeen => treat agent as down
RECOVER_TRIES="${LATTICE_WD_TRIES:-3}"        # failed cycles before alerting

# ntfy push - reuse the topic the phone is already subscribed to.
NTFY_URL="${LATTICE_NTFY_URL:-https://ntfy.sh}"
# Topic comes from scripts/fleet.env (LATTICE_NTFY_TOPIC) or env override; empty
# => notify() no-ops. No personal topic baked into the tracked script.
NTFY_TOPIC="${LATTICE_NTFY_TOPIC:-}"

# Machines we actively guard. A down agent is only recovered/alerted when its
# name is on this list AND maps to an SSH alias (from LATTICE_FLEET). Everything
# else (a desktop that sleeps, phones, unmapped boxes) is ignored - guarding a
# machine we can't (or shouldn't) wake just produces a recovery storm + false
# alerts. Override with LATTICE_WD_WATCH="name1 name2 ...".
#
# Only ALWAYS-ON machines belong here. Laptops sleep/roam and their agent
# connection flaps - guarding them produces a down/recovered push PER flap, and
# you can't SSH-wake a sleeping laptop anyway. Same reason phones are excluded.
#
# A Windows desktop CAN be watched: when its agent dies the hub SSHes
# `schtasks /run /tn LatticeAgent` to revive it. Accepted tradeoff: when the box
# sleeps it reads as down, so each sleep/wake cycle yields one "down" (after
# RECOVER_TRIES failed SSH attempts) + one "recovered" ntfy ping. The
# RECOVER_TRIES dedup keeps it to that, not a per-cycle storm. Drop it from
# LATTICE_WD_WATCH if the pings get noisy.
# Default comes from scripts/fleet.env (LATTICE_WD_WATCH) or env override; empty
# => nothing guarded (safe generic default - no recovery storms on a fresh hub).
WATCH="${LATTICE_WD_WATCH:-}"

# Per-machine counters live as files here (bash-3.2-safe; also survives restart).
STATE_DIR="${LATTICE_WD_STATE:-${TMPDIR:-/tmp}/lattice-watchdog}"
mkdir -p "$STATE_DIR"

# safe filename for a machine name (no slashes/spaces in the state filename).
slug() { printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_'; }
fails_get()  { local f="$STATE_DIR/$(slug "$1").fails";   [ -f "$f" ] && cat "$f" || echo 0; }
fails_set()  { printf '%s' "$2" > "$STATE_DIR/$(slug "$1").fails"; }
alerted_get(){ local f="$STATE_DIR/$(slug "$1").alerted"; [ -f "$f" ] && cat "$f" || echo 0; }
alerted_set(){ printf '%s' "$2" > "$STATE_DIR/$(slug "$1").alerted"; }
state_clear(){ rm -f "$STATE_DIR/$(slug "$1").fails" "$STATE_DIR/$(slug "$1").alerted"; }

# Is this machine on the watch list?
is_watched() {
  local n="$1" w
  for w in $WATCH; do [ "$n" = "$w" ] && return 0; done
  return 1
}

# Map an agent NAME (as the hub reports it) to its SSH alias + OS family by
# looking it up in LATTICE_FLEET ("ssh_alias:lattice_name:os" entries from
# scripts/fleet.env). The hub host itself is not in LATTICE_FLEET (it's
# pm2-managed and can't watchdog itself), so it never resolves a recovery path.
# Match on the reported name against either the lattice_name OR the ssh_alias,
# so a box that enrolls under its hostname still resolves.
ssh_alias() {
  local entry alias name _os
  [ "${#LATTICE_FLEET[@]}" -gt 0 ] || { echo ""; return; }
  for entry in "${LATTICE_FLEET[@]}"; do
    IFS=: read -r alias name _os <<<"$entry"
    if [ "$1" = "$name" ] || [ "$1" = "$alias" ]; then echo "$alias"; return; fi
  done
  echo ""   # unknown => no recovery path
}

# OS family per agent name (drives which recovery command to use). Falls back to
# darwin when the name isn't in LATTICE_FLEET.
os_family() {
  local entry alias name os
  [ "${#LATTICE_FLEET[@]}" -gt 0 ] || { echo darwin; return; }
  for entry in "${LATTICE_FLEET[@]}"; do
    IFS=: read -r alias name os <<<"$entry"
    if [ "$1" = "$name" ] || [ "$1" = "$alias" ]; then echo "$os"; return; fi
  done
  echo darwin
}

notify() {
  local title="$1" msg="$2" priority="${3:-default}" tags="${4:-warning}"
  [ -n "$NTFY_TOPIC" ] || return 0
  curl -fsS -m 10 \
    -H "Title: Lattice · ${title}" \
    -H "Priority: ${priority}" \
    -H "Tags: ${tags}" \
    -d "$msg" \
    "${NTFY_URL}/${NTFY_TOPIC}" >/dev/null 2>&1 || true
}

recover() {
  local name="$1" alias os
  alias="$(ssh_alias "$name")"
  os="$(os_family "$name")"
  [ -n "$alias" ] || { echo "  no SSH alias for '$name' - cannot recover"; return 1; }

  if [ "$os" = windows ]; then
    ssh -o ConnectTimeout=8 -o BatchMode=yes "$alias" \
      "schtasks /run /tn LatticeAgent" >/dev/null 2>&1
  else
    # gui/<uid> domain so the agent relaunches inside the user's Aqua session
    # (Keychain access for claude auth - D22). -k kills any stuck instance first.
    ssh -o ConnectTimeout=8 -o BatchMode=yes "$alias" \
      'launchctl kickstart -k "gui/$(id -u)/sh.lattice.agent"' >/dev/null 2>&1
  fi
}

echo "lattice-watchdog: up (interval=${CHECK_INTERVAL}s stale=${STALE_AFTER}s tries=${RECOVER_TRIES} watch='${WATCH}' ntfy=${NTFY_TOPIC:-none})"

while true; do
  # Re-read the token every sweep. The daemon outlives operator token rotations;
  # caching it once at startup means a rotation silently invalidates our auth and
  # the sweep dies until someone restarts the process. Re-reading self-heals.
  TOKEN="$(cat .lattice-token 2>/dev/null)"
  if [ -z "$TOKEN" ]; then
    echo "$(date '+%H:%M:%S') no token in .lattice-token - skipping sweep"
    sleep "$CHECK_INTERVAL"; continue
  fi

  # Reachability gate uses the UNGATED /api/health so an auth problem can never be
  # misread as "hub down". Only a genuinely unreachable hub skips the sweep.
  if ! curl -fsS -m 10 "${HUB_URL}/api/health" >/dev/null 2>&1; then
    echo "$(date '+%H:%M:%S') hub unreachable on ${HUB_URL} - skipping sweep"
    sleep "$CHECK_INTERVAL"; continue
  fi

  # Fetch the (auth-gated) device roster, capturing the HTTP status so a 401/403
  # surfaces as a LOUD, distinct error instead of masquerading as "hub unreachable".
  # Split on the LAST newline: trailing token is the status, everything before is body.
  RESP="$(curl -sS -m 10 -w $'\n%{http_code}' -H "Authorization: Bearer $TOKEN" "${HUB_URL}/api/devices" 2>/dev/null)"
  CODE="${RESP##*$'\n'}"
  JSON="${RESP%$'\n'*}"
  if [ "$CODE" != 200 ]; then
    if [ "$CODE" = 401 ] || [ "$CODE" = 403 ]; then
      echo "$(date '+%H:%M:%S') /api/devices auth rejected (HTTP $CODE) - token stale/rotated? check .lattice-token. Skipping sweep."
    else
      echo "$(date '+%H:%M:%S') /api/devices returned HTTP ${CODE:-000} - skipping sweep"
    fi
    sleep "$CHECK_INTERVAL"; continue
  fi
  if [ -z "$JSON" ]; then
    echo "$(date '+%H:%M:%S') /api/devices empty body - skipping sweep"
    sleep "$CHECK_INTERVAL"; continue
  fi

  # Emit one line per agent-backed device that looks down: "offline, or lastSeen
  # older than STALE_AFTER". Non-agent devices are ignored here.
  DOWN="$(printf '%s' "$JSON" | STALE="$STALE_AFTER" python3 -c '
import json,sys,os,datetime
stale=int(os.environ["STALE"])
now=datetime.datetime.now(datetime.timezone.utc)
for d in json.load(sys.stdin).get("devices",[]):
    if not d.get("hasAgent"): continue
    name=d.get("name","")
    bad = not d.get("online", False)
    ls=d.get("lastSeen","")
    if not bad and ls:
        try:
            t=datetime.datetime.fromisoformat(ls.replace("Z","+00:00"))
            if (now-t).total_seconds() > stale: bad=True
        except Exception: pass
    if bad and name: print(name)
')"

  # Reconcile recovered machines: any machine with saved state that is NOT down
  # this cycle has come back -> clear its state + send a single recovery note.
  for f in "$STATE_DIR"/*.fails; do
    [ -e "$f" ] || continue
    sname="$(basename "$f" .fails)"
    # Recover the real name from any down line whose slug matches (handles spaces).
    still_down=0
    while IFS= read -r dn; do
      [ -n "$dn" ] || continue
      [ "$(slug "$dn")" = "$sname" ] && { still_down=1; break; }
    done <<<"$DOWN"
    if [ "$still_down" -eq 0 ]; then
      if [ "$(cat "$STATE_DIR/$sname.alerted" 2>/dev/null || echo 0)" = 1 ]; then
        notify "recovered" "✅ ${sname} is back online." default "white_check_mark"
      fi
      rm -f "$STATE_DIR/$sname.fails" "$STATE_DIR/$sname.alerted"
      echo "$(date '+%H:%M:%S') $sname recovered"
    fi
  done

  # Attempt recovery for each watched down machine. Unwatched down agents are
  # noted once at low volume and otherwise left alone (no storm, no false alert).
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    if ! is_watched "$name"; then
      echo "$(date '+%H:%M:%S') $name down but not on watch list - ignoring"
      continue
    fi
    echo "$(date '+%H:%M:%S') $name DOWN - attempting recovery"
    recover "$name" || true
    n="$(( $(fails_get "$name") + 1 ))"
    fails_set "$name" "$n"
    # Alert once after RECOVER_TRIES consecutive failures.
    if [ "$n" -ge "$RECOVER_TRIES" ] && [ "$(alerted_get "$name")" != 1 ]; then
      notify "agent down" "⚠️ ${name} has been unreachable for ${n} cycles and auto-recovery is not sticking. Check the machine." high "rotating_light"
      alerted_set "$name" 1
    fi
  done <<<"$DOWN"

  sleep "$CHECK_INTERVAL"
done

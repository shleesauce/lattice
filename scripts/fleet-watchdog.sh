#!/usr/bin/env bash
# Lattice fleet watchdog — "keep everything on; recover or reach out".
#
# Runs on mini-ops (the hub host) under pm2. Every CHECK_INTERVAL it asks the hub
# which agent-backed machines are stale/offline, then tries to bring each back
# using that machine's OWN persistence primitive over SSH:
#   macOS : launchctl kickstart -k gui/<uid>/sh.lattice.agent   (re-arms launchd)
#   windows: schtasks /run /tn LatticeAgent                     (re-arms the task)
# If a machine can't be recovered after RECOVER_TRIES consecutive cycles, it
# pushes ONE alert to ntfy (the same topic the phone already subscribes to) and
# goes quiet until the machine recovers — then sends a single "recovered" note.
#
# Why a script on the hub host (not in the hub binary): D2 keeps the hub free of
# any inbound/SSH dependency on leaves; recovery is an operator action, so it
# lives beside deploy-fleet.sh and reuses the same SSH aliases. The agent's own
# reconnect-backoff loop handles transient hub blips; this watchdog handles the
# harder case where the agent PROCESS is gone (crash / headless reboot).
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
TOKEN="$(cat .lattice-token 2>/dev/null)"
HUB_URL="http://localhost:7400"

CHECK_INTERVAL="${LATTICE_WD_INTERVAL:-60}"   # seconds between sweeps
STALE_AFTER="${LATTICE_WD_STALE:-90}"         # secs since lastSeen ⇒ treat agent as down
RECOVER_TRIES="${LATTICE_WD_TRIES:-3}"        # failed cycles before alerting

# ntfy push — reuse the homebase topic the phone is already subscribed to.
NTFY_URL="${LATTICE_NTFY_URL:-https://ntfy.sh}"
NTFY_TOPIC="${LATTICE_NTFY_TOPIC:-homebase-mini-ops-1b595c0d11a1}"

# SSH alias per agent NAME as the hub reports it. mini-ops is the hub host itself
# (pm2-managed) so it's intentionally excluded — it can't watchdog itself.
ssh_alias() {
  case "$1" in
    Dylans-Mac-Studio.local|studio)        echo studio ;;
    mbp)                                   echo mbp ;;
    pc)                                     echo pc ;;
    emu|Emulation*|*Mac\ mini*)            echo emu ;;
    *)                                      echo "" ;;   # unknown ⇒ no recovery path
  esac
}

# OS family per agent name (drives which recovery command to use).
os_family() {
  case "$1" in
    pc) echo windows ;;
    *)  echo darwin ;;
  esac
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
  [ -n "$alias" ] || { echo "  no SSH alias for '$name' — cannot recover"; return 1; }

  if [ "$os" = windows ]; then
    ssh -o ConnectTimeout=8 -o BatchMode=yes "$alias" \
      "schtasks /run /tn LatticeAgent" >/dev/null 2>&1
  else
    # gui/<uid> domain so the agent relaunches inside the user's Aqua session
    # (Keychain access for claude auth — D22). -k kills any stuck instance first.
    ssh -o ConnectTimeout=8 -o BatchMode=yes "$alias" \
      'launchctl kickstart -k "gui/$(id -u)/sh.lattice.agent"' >/dev/null 2>&1
  fi
}

declare -A FAILS=()     # name → consecutive failed recovery cycles
declare -A ALERTED=()   # name → 1 once we've alerted (suppresses repeats)

echo "lattice-watchdog: up (interval=${CHECK_INTERVAL}s stale=${STALE_AFTER}s tries=${RECOVER_TRIES} ntfy=${NTFY_TOPIC:-none})"

while true; do
  JSON="$(curl -fsS -m 10 -H "Authorization: Bearer $TOKEN" "${HUB_URL}/api/devices" 2>/dev/null)"
  if [ -z "$JSON" ]; then
    echo "$(date '+%H:%M:%S') hub unreachable on ${HUB_URL} — skipping sweep"
    sleep "$CHECK_INTERVAL"; continue
  fi

  # Emit one line per agent-backed device: "<name>\t<down|up>" where down means
  # the agent process looks dead (offline, or lastSeen older than STALE_AFTER).
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
    if bad: print(name)
')"

  # Reconcile recovered machines first: anything previously failing that is NOT
  # in DOWN this cycle has come back → clear state + send a single recovery note.
  for name in "${!FAILS[@]}"; do
    if ! grep -qxF "$name" <<<"$DOWN"; then
      [ "${ALERTED[$name]:-0}" = 1 ] && notify "recovered" "✅ ${name} is back online." default "white_check_mark"
      unset 'FAILS[$name]'; unset 'ALERTED[$name]'
      echo "$(date '+%H:%M:%S') $name recovered"
    fi
  done

  # Attempt recovery for each down machine.
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    echo "$(date '+%H:%M:%S') $name DOWN — attempting recovery"
    if recover "$name"; then
      FAILS[$name]=$(( ${FAILS[$name]:-0} + 1 ))
    else
      FAILS[$name]=$(( ${FAILS[$name]:-0} + 1 ))
    fi
    # Alert once after RECOVER_TRIES consecutive failures.
    if [ "${FAILS[$name]}" -ge "$RECOVER_TRIES" ] && [ "${ALERTED[$name]:-0}" != 1 ]; then
      notify "agent down" "⚠️ ${name} has been unreachable for ${FAILS[$name]} cycles and auto-recovery is not sticking. Check the machine." high "rotating_light"
      ALERTED[$name]=1
    fi
  done <<<"$DOWN"

  sleep "$CHECK_INTERVAL"
done

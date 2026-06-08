#!/usr/bin/env bash
# Lattice fleet-sync - keep every agent on the SAME current build, persistently.
#
# The fix for version skew (agents drifting onto different -dirty builds with no
# mechanism to keep them in lockstep). Run this after any agent/proto change:
#   1. rebuild the dashboard + cross-compile every target (scripts/build.sh)
#   2. restart the local hub so it serves the new binaries at /dl + the new SPA
#   3. re-run the hub installer ON each agent box over SSH, which downloads the
#      fresh binary AND (re)installs the OS-native persistence (launchd on macOS,
#      scheduled task on Windows) - so a sync never downgrades durability.
#
# Why the installer and not deploy-fleet.sh: deploy-fleet.sh uses nohup (no
# persistence - that's how agents drifted/dropped before). The hub installer is
# the canonical D14 path and lays down launchd/schtasks, matching D33's always-on.
#
# Targets come from scripts/fleet.env (LATTICE_FLEET). The hub host itself is
# rebuilt+restarted directly here, not over SSH.
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
TOKEN="$(cat .lattice-token 2>/dev/null)"
[ -n "$TOKEN" ] || { echo "lattice: no token in .lattice-token" >&2; exit 1; }

# Verify step pipes hub JSON into inline python3; without it the sync can't
# confirm the fleet came back. Fail fast rather than skipping verification.
command -v python3 >/dev/null || { echo "lattice: python3 required" >&2; exit 1; }

# Operator-local fleet config (gitignored). See scripts/fleet.env.example.
FLEET_ENV="$(dirname "$0")/fleet.env"
[ -f "$FLEET_ENV" ] || { echo "missing $FLEET_ENV - copy scripts/fleet.env.example"; exit 1; }
# shellcheck disable=SC1090
. "$FLEET_ENV"
HUB_TS="$LATTICE_HUB_TS"        # tailnet URL agents dial
HUB_LOCAL="$LATTICE_HUB_LOCAL"
# Agent boxes to sync: "ssh_alias:lattice_name:os". The hub host is handled
# separately above (rebuilt+restarted directly, not over SSH).
TARGETS=("${LATTICE_FLEET[@]}")

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

# 1. Rebuild everything (dashboard embed + all cross-compiled binaries).
say "building (dashboard + all targets)"
if ! bash scripts/build.sh; then
  echo "build failed - aborting sync (fleet left untouched)"; exit 1
fi

# 2. Restart the local hub so it serves the fresh binaries + SPA.
say "restarting local hub (pm2 lattice-hub)"
pm2 restart lattice-hub >/dev/null 2>&1 || echo "  WARN: pm2 restart lattice-hub failed - is pm2 up?"
# Give it a moment, then health-check.
for _ in 1 2 3 4 5; do
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$HUB_LOCAL/api/devices" 2>/dev/null)"
  [ "$code" = 200 ] && break
  sleep 1
done
[ "${code:-}" = 200 ] && echo "  hub healthy ($code)" || { echo "  hub not healthy ($code) - aborting"; exit 1; }

# 3. Re-run the hub installer on each agent box (downloads fresh binary +
#    re-lays persistence). The installer restarts the agent itself.
FAILED=()
for entry in "${TARGETS[@]}"; do
  IFS=: read -r alias name os <<<"$entry"
  say "sync $name ($os) via $alias"
  if [ "$os" = windows ]; then
    # PowerShell installer; LATTICE_TOKEN via env, --name preserved.
    if ssh -o ConnectTimeout=20 -o BatchMode=yes "$alias" \
        "powershell -NoProfile -Command \"\$env:LATTICE_TOKEN='$TOKEN'; irm http://$HUB_TS/install.ps1 | iex\"" 2>&1 | tail -4; then
      :
    else
      FAILED+=("$name"); echo "  $name: install FAILED"
    fi
  else
    # Token via LATTICE_TOKEN env (matches the Windows path) so it never lands in
    # the remote `ps`/argv while the installer runs; --name preserved.
    if ssh -o ConnectTimeout=20 -o BatchMode=yes "$alias" \
        "LATTICE_TOKEN='$TOKEN' sh -c 'curl -fsSL http://$HUB_TS/install.sh | sh -s -- --name $name'" 2>&1 | tail -4; then
      :
    else
      FAILED+=("$name"); echo "  $name: install FAILED"
    fi
  fi
done

# 4. Verify: every agent back online with a fresh heartbeat.
say "verifying fleet"
sleep 5
curl -s -H "Authorization: Bearer $TOKEN" "$HUB_LOCAL/api/devices" | python3 -c '
import json,sys,datetime
d=json.load(sys.stdin)["devices"]; now=datetime.datetime.now(datetime.timezone.utc)
for x in d:
    if not x.get("hasAgent"): continue
    ls=x.get("lastSeen",""); age="?"
    try:
        t=datetime.datetime.fromisoformat(ls.replace("Z","+00:00")); age=str(int((now-t).total_seconds()))+"s"
    except Exception: pass
    name=x.get("name",""); st=x.get("status",""); alive=x.get("agentLive","?")
    flag="" if (x.get("agentLive") if x.get("agentLive") is not None else x.get("online")) else "  <-- NOT live"
    print("  %-26s status=%-9s agentLive=%s age=%s%s" % (name, st, alive, age, flag))
'

if [ "${#FAILED[@]}" -gt 0 ]; then
  echo
  echo "SYNC INCOMPLETE - failed: ${FAILED[*]}"
  exit 1
fi
say "fleet-sync complete - all agents on $(git describe --tags --always --dirty 2>/dev/null || echo current)"

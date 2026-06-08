#!/usr/bin/env bash
# Phase-1 dogfood deploy: push the right cross-compiled binary to each leaf and
# (re)start its agent pointed at the hub over the tailnet.
#
# Persistence here is intentionally light (nohup / detached) - proper per-OS
# services are Phase 4. The agent's own reconnect loop covers hub restarts.
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
TOKEN="$(cat .lattice-token 2>/dev/null)"
[ -n "$TOKEN" ] || { echo "lattice: no token in .lattice-token" >&2; exit 1; }

# Standardize SSH/SCP: fail fast on unknown host keys / unreachable hosts rather
# than hanging a non-interactive deploy (BatchMode), with a bounded timeout.
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10)

# Operator-local fleet config (gitignored). See scripts/fleet.env.example.
FLEET_ENV="$(dirname "$0")/fleet.env"
[ -f "$FLEET_ENV" ] || { echo "missing $FLEET_ENV - copy scripts/fleet.env.example"; exit 1; }
# shellcheck disable=SC1090
. "$FLEET_ENV"
HUB="$LATTICE_HUB_TS"
DARWIN="dist/lattice-darwin-arm64"
WIN="dist/lattice-windows-amd64.exe"

[ -f "$DARWIN" ] || { echo "missing $DARWIN - run scripts/build.sh first"; exit 1; }
[ -f "$WIN" ]    || { echo "missing $WIN - run scripts/build.sh first"; exit 1; }

deploy_mac() {
  local alias="$1" name="$2"
  echo "==> $alias (darwin)"
  # Narrow the kill to the installed binary path so we don't reap an unrelated
  # process (an editor, a tail, or this very ssh) that merely mentions the words.
  ssh "${SSH_OPTS[@]}" "$alias" 'mkdir -p ~/lattice && pkill -f "/lattice/lattice agent" 2>/dev/null; sleep 1'
  # A failed copy must abort THIS machine (was silently swallowed -> stale-binary
  # "success" was the structural cause of the version-skew the audit flagged).
  scp "${SSH_OPTS[@]}" -q "$DARWIN" "$alias:lattice/lattice" || { echo "  $alias: scp FAILED - skipping"; return 1; }
  # Token via LATTICE_TOKEN env (read by the agent) so it never lands in argv /
  # `ps` on the remote box. Set inline before nohup so it's in the agent's env.
  ssh "${SSH_OPTS[@]}" "$alias" "chmod +x ~/lattice/lattice && \
    LATTICE_TOKEN='$TOKEN' nohup ~/lattice/lattice agent --hub $HUB --name $name \
    > ~/lattice/agent.log 2>&1 & sleep 1; echo started; tail -3 ~/lattice/agent.log" \
    || { echo "  $alias: agent start FAILED"; return 1; }
}

deploy_win() {
  local alias="$1" name="$2"
  echo "==> $alias (windows)"
  # Stop any prior, copy the fresh binary.
  ssh "${SSH_OPTS[@]}" "$alias" 'taskkill /IM lattice.exe /F 2>NUL & mkdir C:\\lattice 2>NUL & echo ok' >/dev/null
  scp "${SSH_OPTS[@]}" -q "$WIN" "$alias:C:/lattice/lattice.exe" || { echo "  $alias: scp FAILED - skipping"; return 1; }
  # Spawn FULLY DETACHED via WMI Win32_Process.Create so the agent survives the
  # SSH session closing (a plain Start-Process dies with the ssh logon session).
  # Redirect output to a logfile so it isn't tied to a console.
  #
  # Token goes through the spawned process's ENVIRONMENT, not its command line:
  # we set $env:LATTICE_TOKEN in this PowerShell session first, and the child
  # created by Win32_Process.Create (no ProcessStartupInformation) inherits the
  # creator's environment block - so the master token never appears in the child
  # process command line (Win32_Process.CommandLine / Get-CimInstance).
  local cmdline="C:\\lattice\\lattice.exe agent --hub $HUB --name $name"
  ssh "${SSH_OPTS[@]}" "$alias" "powershell -NoProfile -Command \"\$env:LATTICE_TOKEN='$TOKEN'; Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine='cmd /c \\\"$cmdline > C:\\lattice\\agent.log 2>&1\\\"'} | Select-Object ProcessId,ReturnValue | Format-List\""
}

# Deploy one "ssh_alias:lattice_name:os" entry, routing by os.
deploy_one() {
  local alias name os
  IFS=: read -r alias name os <<<"$1"
  if [ "$os" = windows ]; then deploy_win "$alias" "$name"; else deploy_mac "$alias" "$name"; fi
}

FAILED=()
TARGET="${1:-all}"
matched=0
for entry in "${LATTICE_FLEET[@]}"; do
  IFS=: read -r alias name _os <<<"$entry"
  if [ "$TARGET" = all ] || [ "$TARGET" = "$alias" ] || [ "$TARGET" = "$name" ]; then
    matched=1
    deploy_one "$entry" || FAILED+=("$name")
  fi
done
if [ "$matched" -eq 0 ]; then
  echo "usage: deploy-fleet.sh [<alias-or-name from scripts/fleet.env>|all]"; exit 2
fi

if [ "${#FAILED[@]}" -gt 0 ]; then
  echo "==> deploy INCOMPLETE - failed: ${FAILED[*]}"
  exit 1
fi
echo "==> deploy complete; check the dashboard at http://$HUB"

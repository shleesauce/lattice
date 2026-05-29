#!/usr/bin/env bash
# Phase-1 dogfood deploy: push the right cross-compiled binary to each leaf and
# (re)start its agent pointed at the hub on mini-ops over the tailnet.
#
# Persistence here is intentionally light (nohup / detached) — proper per-OS
# services are Phase 4. The agent's own reconnect loop covers hub restarts.
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
TOKEN="$(cat .lattice-token)"
HUB="mini-ops.tail3c8bee.ts.net:7400"
DARWIN="dist/lattice-darwin-arm64"
WIN="dist/lattice-windows-amd64.exe"

[ -f "$DARWIN" ] || { echo "missing $DARWIN — run scripts/build.sh first"; exit 1; }
[ -f "$WIN" ]    || { echo "missing $WIN — run scripts/build.sh first"; exit 1; }

deploy_mac() {
  local alias="$1" name="$2"
  echo "==> $alias (darwin)"
  ssh "$alias" 'mkdir -p ~/lattice && pkill -f "lattice agent" 2>/dev/null; sleep 1' 2>/dev/null
  scp -q "$DARWIN" "$alias:lattice/lattice"
  ssh "$alias" "chmod +x ~/lattice/lattice && \
    nohup ~/lattice/lattice agent --hub $HUB --token $TOKEN --name $name \
    > ~/lattice/agent.log 2>&1 & sleep 1; echo started; tail -3 ~/lattice/agent.log" 2>/dev/null
}

deploy_win() {
  echo "==> pc (windows)"
  # Stop any prior, copy the fresh binary.
  ssh pc 'taskkill /IM lattice.exe /F 2>NUL & mkdir C:\\lattice 2>NUL & echo ok' >/dev/null 2>&1
  scp -q "$WIN" 'pc:C:/lattice/lattice.exe'
  # Spawn FULLY DETACHED via WMI Win32_Process.Create so the agent survives the
  # SSH session closing (a plain Start-Process dies with the ssh logon session).
  # Redirect output to a logfile so it isn't tied to a console.
  local cmdline="C:\\lattice\\lattice.exe agent --hub $HUB --token $TOKEN --name pc"
  ssh pc "powershell -NoProfile -Command \"Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine='cmd /c \\\"$cmdline > C:\\lattice\\agent.log 2>&1\\\"'} | Select-Object ProcessId,ReturnValue | Format-List\""
}

case "${1:-all}" in
  studio) deploy_mac studio studio ;;
  mbp)    deploy_mac mbp mbp ;;
  pc)     deploy_win ;;
  all)
    deploy_mac studio studio
    deploy_mac mbp mbp
    deploy_win
    ;;
  *) echo "usage: deploy-fleet.sh [studio|mbp|pc|all]"; exit 2 ;;
esac
echo "==> deploy complete; check the dashboard at http://mini-ops.tail3c8bee.ts.net:7400"

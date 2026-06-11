#!/bin/sh
# Lattice HUB bootstrap (macOS + Linux). Installs the controller on a FRESH
# machine - no hub need already be running. The binary is pulled from GitHub
# Releases (not served by a hub), the config + token are initialized, a free
# port is chosen, and the hub is installed as a persistent service.
#
# Usage:  curl -fsSL https://raw.githubusercontent.com/shleesauce/lattice/master/install/get.sh | sh
#
# The download base is overridable for local testing:
#   LATTICE_DOWNLOAD_BASE=http://localhost:8000 curl -fsSL .../get.sh | sh
#
# POSIX sh only: `set -o pipefail` is a bashism that ABORTS under a real /bin/sh
# (dash on Debian/Ubuntu, BusyBox on Alpine) — which is exactly the Linux install
# target — so the advertised `curl … | sh` one-liner died on this line. Use `set -eu`.
set -eu

BASE="${LATTICE_DOWNLOAD_BASE:-https://github.com/shleesauce/lattice/releases/latest/download}"

# --- HTTPS-pin the download transport (mirrors the Go updater's requireSecureBase) ---
# A plain-http BASE lets a MITM feed a binary; the SHA256SUMS check only helps if the
# origin is trusted, which is exactly what pinning the transport guarantees. Allow it
# only when https, OR the host is loopback, OR LATTICE_DOWNLOAD_INSECURE=1 (same opt-out
# as the Go side, for local mock-cascade testing). The default GitHub https BASE passes.
case "$BASE" in
  https://*) ;;
  *)
    # Strip scheme, then take the host portion (up to the first '/' or ':').
    _rest="${BASE#*://}"
    _hostport="${_rest%%/*}"
    _host="${_hostport%%:*}"
    case "$_host" in
      localhost|127.0.0.1|::1|"[::1]") ;;
      *)
        if [ "${LATTICE_DOWNLOAD_INSECURE:-}" = "1" ]; then
          echo "lattice: warning: using INSECURE plain-http download base $BASE (LATTICE_DOWNLOAD_INSECURE=1)" >&2
        else
          echo "lattice: refusing to download over an insecure transport: $BASE" >&2
          echo "lattice: must be https (set LATTICE_DOWNLOAD_INSECURE=1 to override for local testing)" >&2
          exit 1
        fi ;;
    esac ;;
esac

# --- detect platform ---
os="$(uname -s)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) echo "lattice: unsupported OS: $os" >&2; exit 1 ;;
esac
arch="$(uname -m)"
case "$arch" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64)  arch="amd64" ;;
  *) echo "lattice: unsupported arch: $arch" >&2; exit 1 ;;
esac

BIN_NAME="lattice-${os}-${arch}"
PREFIX="$HOME/.lattice"
BIN_DIR="$PREFIX/bin"
BIN="$BIN_DIR/lattice"
mkdir -p "$BIN_DIR"

# enable_linger - best-effort enable systemd user linger so the --user service
# starts at BOOT (not just at interactive login) — the whole point of installing
# a persistent service on a headless box. Never fatal under `set -eu`: every step
# is guarded, a missing loginctl or a polkit denial just falls back to a manual tip.
enable_linger() {
  command -v loginctl >/dev/null 2>&1 || return 1
  # Already on? done.
  if [ "$(loginctl show-user "$USER" --property=Linger 2>/dev/null)" = "Linger=yes" ]; then
    return 0
  fi
  # Modern systemd+polkit often lets a user enable their own linger, no sudo.
  loginctl enable-linger "$USER" >/dev/null 2>&1 || true
  # Otherwise try sudo, but ONLY non-interactively (never block the pipe on a prompt).
  if [ "$(loginctl show-user "$USER" --property=Linger 2>/dev/null)" != "Linger=yes" ] \
     && command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    sudo -n loginctl enable-linger "$USER" >/dev/null 2>&1 || true
  fi
  [ "$(loginctl show-user "$USER" --property=Linger 2>/dev/null)" = "Linger=yes" ]
}

# --- download ---
# fetch <url> <dest> - download a URL to a file via curl or wget (whichever exists).
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    echo "lattice: need curl or wget to download the hub" >&2
    exit 1
  fi
}

URL="$BASE/$BIN_NAME"
TMP="$BIN.download.$$"
SUMS="$BIN.sha256sums.$$"
# Clean up partial downloads on any exit path (error or interrupt).
trap 'rm -f "$TMP" "$SUMS"' EXIT

echo "lattice: downloading $URL"
fetch "$URL" "$TMP"
# A failed/HTML-error download must never masquerade as a binary. The checksum
# below is the real gate, but bail early on an obviously empty file.
if [ ! -s "$TMP" ]; then
  echo "lattice: downloaded binary is empty (download failed?): $URL" >&2
  exit 1
fi

# --- verify against SHA256SUMS (fail closed) ---
# Reach parity with the self-updater: never run an unverified binary.
echo "lattice: verifying checksum"
if ! fetch "$BASE/SHA256SUMS" "$SUMS"; then
  echo "lattice: could not fetch $BASE/SHA256SUMS - refusing to install unverified binary" >&2
  exit 1
fi
# Extract the expected hash for THIS asset. SHA256SUMS lines are:
#   <64-hex><two spaces><filename>
EXPECTED="$(sed -n "s/^\([0-9a-fA-F]\{64\}\)  ${BIN_NAME}\$/\1/p" "$SUMS" | head -n1)"
if [ -z "$EXPECTED" ]; then
  echo "lattice: no checksum entry for $BIN_NAME in SHA256SUMS - refusing to install" >&2
  exit 1
fi
# Compute the local hash. Linux ships sha256sum; macOS ships shasum.
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$TMP" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "$TMP" | awk '{print $1}')"
else
  echo "lattice: need sha256sum or shasum to verify the download" >&2
  exit 1
fi
# Case-insensitive compare (both tools emit lowercase, but be defensive).
if [ "$(printf '%s' "$EXPECTED" | tr 'A-F' 'a-f')" != "$(printf '%s' "$ACTUAL" | tr 'A-F' 'a-f')" ]; then
  echo "lattice: checksum MISMATCH for $BIN_NAME" >&2
  echo "lattice:   expected $EXPECTED" >&2
  echo "lattice:   actual   $ACTUAL" >&2
  echo "lattice: refusing to install a binary that does not match SHA256SUMS" >&2
  exit 1
fi
echo "lattice: checksum verified"

# Only now is the binary trusted - make it executable and move into place.
chmod +x "$TMP"
mv -f "$TMP" "$BIN"
rm -f "$SUMS"
trap - EXIT
echo "lattice: installed binary at $BIN"

# Make `lattice` runnable by name (no sudo, no system dirs): drop a symlink in
# ~/.local/bin. The uninstaller removes it. The binary at $BIN always works by
# full path regardless of PATH.
mkdir -p "$HOME/.local/bin" 2>/dev/null && ln -sf "$BIN" "$HOME/.local/bin/lattice" 2>/dev/null \
  && echo "lattice: linked 'lattice' command → ~/.local/bin/lattice"

# --- initialize config + token + free port ---
"$BIN" hub init

# Parse the chosen addr (e.g. ":7400") from the config the init just wrote.
CONFIG="$PREFIX/config.json"
ADDR="$(sed -n 's/.*"addr"[^"]*"\([^"]*\)".*/\1/p' "$CONFIG" | head -n1)"
[ -n "$ADDR" ] || ADDR=":7400"
PORT="${ADDR#:}"

# --- install + (re)start the persistent hub service ---
if [ "$os" = "darwin" ]; then
  LABEL="sh.lattice.hub"
  PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
  LOG_DIR="$PREFIX/log"
  mkdir -p "$HOME/Library/LaunchAgents" "$LOG_DIR"
  cat > "$PLIST" <<PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN</string>
    <string>hub</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$LOG_DIR/hub.out.log</string>
  <key>StandardErrorPath</key><string>$LOG_DIR/hub.err.log</string>
</dict>
</plist>
PLIST_EOF

  GUI="gui/$(id -u)"
  # Idempotent: tear down any existing instance, then bootstrap fresh.
  launchctl bootout "$GUI/$LABEL" >/dev/null 2>&1 || true
  launchctl unload "$PLIST" >/dev/null 2>&1 || true
  if launchctl bootstrap "$GUI" "$PLIST" >/dev/null 2>&1; then
    launchctl kickstart -k "$GUI/$LABEL" >/dev/null 2>&1 || true
  else
    launchctl load "$PLIST" >/dev/null 2>&1 || true
  fi
  echo "lattice: launchd hub '$LABEL' installed and started"

else
  # Linux: prefer a systemd --user unit; fall back to nohup.
  if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    UNIT_DIR="$HOME/.config/systemd/user"
    UNIT="$UNIT_DIR/lattice-hub.service"
    mkdir -p "$UNIT_DIR"
    cat > "$UNIT" <<UNIT_EOF
[Unit]
Description=Lattice hub
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=$BIN hub
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
UNIT_EOF
    systemctl --user daemon-reload
    systemctl --user enable --now lattice-hub.service
    # Ensure a config change is picked up on re-run.
    systemctl --user restart lattice-hub.service
    echo "lattice: systemd --user service 'lattice-hub' installed and started"
    if enable_linger; then
      echo "lattice: reboot-survival enabled (user linger on — the hub starts at boot, no login needed)"
    else
      echo "lattice: tip - run 'sudo loginctl enable-linger $USER' so it runs without an active login"
    fi
  else
    PIDFILE="$PREFIX/hub.pid"
    LOG="$PREFIX/hub.log"
    if [ -f "$PIDFILE" ]; then kill "$(cat "$PIDFILE")" >/dev/null 2>&1 || true; fi
    nohup "$BIN" hub >"$LOG" 2>&1 &
    echo $! > "$PIDFILE"
    echo "lattice: started hub via nohup (no systemd); logs at $LOG"
    echo "lattice: note - nohup does not survive reboot; install systemd --user for persistence"
  fi
fi

# --- enroll THIS machine as an agent too ---
# So the dashboard is populated and fully working out of the box (live metrics,
# terminal, editor) instead of showing an empty fleet the operator has to figure
# out how to fill. The hub host running its own agent is the normal pattern; it
# reaches the hub over loopback, so this needs NO Tailscale — it's all one box.
# Wrapped so any hiccup here is non-fatal: the hub is already installed above and
# must never be undone by an agent problem.
TOKEN="$(cat "$PREFIX/.lattice-token" 2>/dev/null || true)"
AGENT_NAME="$(hostname 2>/dev/null || echo lattice-node)"
AGENT_HUB="127.0.0.1:$PORT"

enroll_local_agent() {
  if [ "$os" = "darwin" ]; then
    ALABEL="sh.lattice.agent"
    APLIST="$HOME/Library/LaunchAgents/$ALABEL.plist"
    mkdir -p "$HOME/Library/LaunchAgents" "$PREFIX/log"
    cat > "$APLIST" <<APLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$ALABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN</string>
    <string>agent</string>
    <string>--hub</string><string>$AGENT_HUB</string>
    <string>--name</string><string>$AGENT_NAME</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict><key>LATTICE_TOKEN</key><string>$TOKEN</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$PREFIX/log/agent.out.log</string>
  <key>StandardErrorPath</key><string>$PREFIX/log/agent.err.log</string>
</dict>
</plist>
APLIST_EOF
    chmod 600 "$APLIST"  # embeds LATTICE_TOKEN — owner-only
    AGUI="gui/$(id -u)"
    launchctl bootout "$AGUI/$ALABEL" >/dev/null 2>&1 || true
    launchctl unload "$APLIST" >/dev/null 2>&1 || true
    if launchctl bootstrap "$AGUI" "$APLIST" >/dev/null 2>&1; then
      launchctl kickstart -k "$AGUI/$ALABEL" >/dev/null 2>&1 || true
    else
      launchctl load "$APLIST" >/dev/null 2>&1 || true
    fi
  elif command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    AUNIT_DIR="$HOME/.config/systemd/user"
    mkdir -p "$AUNIT_DIR"
    cat > "$AUNIT_DIR/lattice-agent.service" <<AUNIT_EOF
[Unit]
Description=Lattice agent
After=network-online.target
Wants=network-online.target

[Service]
Environment=LATTICE_TOKEN=$TOKEN
ExecStart=$BIN agent --hub $AGENT_HUB --name $AGENT_NAME
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
AUNIT_EOF
    chmod 600 "$AUNIT_DIR/lattice-agent.service"  # embeds LATTICE_TOKEN — owner-only
    systemctl --user daemon-reload
    systemctl --user enable --now lattice-agent.service
    systemctl --user restart lattice-agent.service
  else
    mkdir -p "$PREFIX/log"
    if [ -f "$PREFIX/agent.pid" ]; then kill "$(cat "$PREFIX/agent.pid")" >/dev/null 2>&1 || true; fi
    LATTICE_TOKEN="$TOKEN" nohup "$BIN" agent --hub "$AGENT_HUB" --name "$AGENT_NAME" >"$PREFIX/log/agent.log" 2>&1 &
    echo $! > "$PREFIX/agent.pid"
  fi
}

if [ -n "$TOKEN" ]; then
  if enroll_local_agent; then
    echo "lattice: this machine is enrolled as agent '$AGENT_NAME' — it'll appear in your fleet automatically"
  else
    echo "lattice: local agent enroll hit a snag (non-fatal) — the hub is running; add this machine from the dashboard"
  fi
else
  echo "lattice: (couldn't read the token to self-enroll — the hub is running; add this machine from the dashboard)"
fi

HOSTNAME="$(hostname 2>/dev/null || echo localhost)"
echo ""
echo "lattice: hub + this machine are up and running."
echo "lattice: open  http://$HOSTNAME:$PORT/  to finish setup (your machine is already in the fleet)."
case ":$PATH:" in
  *":$HOME/.local/bin:"*) echo "lattice: manage it anytime with the 'lattice' command (e.g. 'lattice uninstall')." ;;
  *) echo "lattice: for the 'lattice' command, add ~/.local/bin to your PATH — or use $BIN" ;;
esac

# Changelog

All notable changes to Lattice are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [0.1.4] - 2026-06-08

A bugfix release — found in first-run testing.

### Fixed
- **The `lattice` command now works by name.** The installer put the binary at
  `~/.lattice/bin/lattice` but never on `PATH`, so the documented `lattice uninstall` /
  `update` / `doctor` returned "command not found". `get.sh` now symlinks `lattice` into
  `~/.local/bin` (no sudo, no system dirs); `get.ps1` adds the install dir to your user
  `PATH`. Both uninstallers (and `lattice uninstall`) remove what was added. The web
  uninstaller still needs no `PATH` at all.

## [0.1.3] - 2026-06-08

A bugfix release — critical for multi-machine meshes; found in first-run testing.

### Fixed
- **A second machine could not join a self-installed hub.** The hub served agent binaries from a
  `dist/` folder resolved relative to its working directory, but a hub installed by `get.sh` has no
  populated `dist/` and its service runs from `/` — so every `/dl/<binary>` request 404'd and the
  agent download inside the join command failed. The hub now **redirects `/dl/` to the public
  release** when a binary isn't present locally, so a `get.sh`-installed hub can hand out the agent
  for **any** OS/arch (e.g. a Mac hub enrolling a Windows machine) with nothing bundled. Hubs built
  from source with a populated `dist/` still serve locally; honors `LATTICE_DOWNLOAD_BASE`.

## [0.1.2] - 2026-06-08

First public release. Focused on making Lattice trustworthy and dead-simple for someone you hand
the repo to: one-command install that lands on a working dashboard, one-command removal, guided
mesh onboarding, and a security/lifecycle hardening pass.

### Added
- **One-command uninstall.** A built-in `lattice uninstall` subcommand (offline; `--dry-run` / `--yes`)
  plus `install/uninstall.sh` and `install/uninstall.ps1`, published as release assets. Stops and
  unregisters the hub *and* agent service and removes `~/.lattice` — user-level only, idempotent, and
  it touches nothing else (not your files, shell, Claude, or Tailscale/SSH/Syncthing).
- **Install auto-enrolls the local machine.** `get.sh` / `get.ps1` now also enroll the hub's own box
  as an agent over loopback, so a fresh install lands on a populated, working dashboard with zero
  Tailscale needed to start.
- **Tailscale-guided mesh onboarding.** The add-machine flow is now a clear two-step command (get on
  your network → join), and the first-run wizard has a dedicated, optional "add your machines" step.
  Lattice guides Tailscale setup; it never installs it for you.
- **Repeatable log cleanup.** `scripts/clean-logs.sh` reclaims pm2 log-rotation disk safely and
  idempotently (truncates live logs, never touches the database).

### Security / hardening
- Login rate-limiter now caps its per-IP failure map (no unbounded growth); the dashboard WebSocket
  gets a read-deadline + ping/pong keepalive so a half-open browser can't leak a connection.
- The master enrollment token is explicitly non-revocable, and revoking an unknown token now 404s
  instead of reporting a false success.
- The unauthenticated health probe no longer touches the database (can't be used to starve the single
  SQLite connection); the `audit_log` and revoked-token tables are now bounded by reaping.
- Installer endpoints prefer an operator-configured canonical hub URL, closing a Host-header spoof on
  the open `curl | sh` endpoints.

### Changed
- Setup guide rewritten to be dummy-proof: a two-step quickstart with the single-machine payoff up
  front, an explicit requirements section, the exact machine footprint, and a one-command uninstall.

## [0.1.1] - 2026-06-07

A patch release. The headline is an install-blocking fix for fresh hub installs; the rest is
reliability and internal cleanup. The live pm2-managed hub is unaffected (it passes `--db` explicitly).

### Fixed
- **Fresh-install database location.** The hub's `--db` defaulted to a cwd-relative `lattice.db`, but the
  installed services (launchd / systemd `--user` / nohup) run `lattice hub` from an unpredictable working
  directory — so a clean install could try to write its database to the filesystem root and fail to start.
  The default is now `~/.lattice/lattice.db`, alongside the config and token. **Anyone who installed v0.1.0
  should `lattice update` to this release.**
- **Fleet watchdog auto-recovery.** The hub-host watchdog cached its enrollment token at startup and used an
  auth-gated endpoint as its liveness probe, so rotating the token silently disabled recovery (a 401 was
  misread as "hub unreachable"). It now re-reads the token each sweep, probes the unauthenticated
  `/api/health`, and reports an auth failure as a distinct error.

### Changed
- Unified the dashboard's floating dialogs onto one shared `<Modal>` and the fleet/devices/workspace data
  hooks onto a shared `useLiveResource`; added a Go↔TypeScript wire-contract drift guard. Internal refactors,
  no behavior change.
- Earlier audit pass: fixed a WebSocket reconnect race, scrubbed PII, and deduplicated hot paths.

## [0.1.0] - 2026-06-06

The first installable release — built and published by `.github/workflows/release.yml`
(all targets + `SHA256SUMS` + `get.sh` / `get.ps1` as GitHub Release assets).

### Hardening (pre-public)
- **Secure by default**: a configured hub refuses to bind a public address with no admin
  password unless `--insecure-no-auth` (loopback + first-run exempt).
- **Verified installs**: `get.sh` / `get.ps1` verify the release `SHA256SUMS` fail-closed,
  matching the self-updater.
- **Token off the command line**: the master/enroll token flows via the `LATTICE_TOKEN`
  environment everywhere (agent, served installers, deploy scripts, pm2) — not argv/`ps`.
- **Supply chain**: all GitHub Actions pinned to commit SHAs; release fails on missing
  assets; `npm ci` + `go mod verify` + `-mod=readonly` + pinned Go toolchain in the build.
- **Apache-2.0 license**; lint backlog cleared (golangci-lint 0 issues) with CI lint blocking.

### Added — install & lifecycle
- **One-line hub bootstrap** (`get.sh` / `get.ps1`) that installs the hub as a persistent
  service (launchd / systemd / Windows Scheduled Task), picks a free port, generates a stable
  token, starts it, and prints the dashboard URL.
- **First-run web wizard** — admin password → mesh name → projects root — served until the hub
  is configured.
- **`lattice hub init`** (scaffold config + stable token + free port) and
  **`lattice hub set-password`** (set/replace the admin password) subcommands.
- **`lattice update`** — self-update from the latest release, with `SHA256SUMS` verification and
  an optional `--restart` of the installed service.
- **`lattice doctor`** — a one-shot health check (config, hub reachability, capabilities, and
  SSH/Syncthing/Tailscale integrations), with a `--json` mode for scripts.
- **GitHub Releases pipeline** — a `v*` tag builds the 5 cross-compiled targets + checksums and
  publishes the binaries and installers as release assets.

### Added — fleet & mesh
- **Live fleet dashboard** — every machine deduped across the lattice agent, the Tailscale
  tailnet, and the SSH config, with CPU / memory / disk / uptime telemetry and truthful status.
- **Manage Mesh** — rename / remove machines, **per-machine revocable join tokens**, a guided
  **Add machine** flow (mint a join command → live "waiting to join"), and an **Integrations**
  panel that detects and guides SSH / Syncthing / Tailscale per machine (never installs them),
  including a "sync your brain" Syncthing pairing walkthrough.
- **Wake-on-LAN** for sleeping machines; offline machines stay visible and wakeable.

### Added — workspace
- Long-lived **Claude** (interactive `claude` in a PTY) and **terminal** sessions that survive a
  browser refresh *and* a hub restart, with smart capability-aware placement across machines.
- Embedded **VS Code** (code-server) tunnelled through the hub over a yamux stream — full editor
  on any fleet machine from one URL, with a paired Claude chat beside it.
- Tunnelled **preview** of a machine's local dev server, a right-side dock (files / terminal /
  git / preview), a ⌘K command palette, and a new-project onboarding wizard.

### Added — security
- **Single admin password** per hub (bcrypt-hashed), HttpOnly + SameSite=Strict session cookie,
  and a long-lived `Authorization: Bearer` API token for CLI/scripts.
- Auth middleware gating the API, dashboard socket, and editor/preview proxies; same-origin
  WebSocket checks; constant-time token comparison; login rate-limiting. Tailscale (WireGuard)
  is the encryption boundary.
- Auth is enforced only once a password is set, so an existing open hub is never locked out by an
  upgrade.

### Performance & correctness
- Single-connection SQLite with WAL pragmas + startup checkpoint (bounded the WAL); cached agent
  capability probes; coalesced fleet broadcasts; TTL-cached Tailscale/SSH reads; reaped
  command-history; one shared dashboard socket.

### Fixed — audit pass (security, lifecycle, dedup)
- **Process cleanup**: session teardown now kills the whole process group, so code-server's node
  workers and `claude`'s MCP/hook children no longer orphan (unix + windows).
- **Tunnel binding**: `/ws/tunnel` is now bound to the token's agent id — a per-machine token can
  only register its own id (master may act for any), closing an editor-tunnel impersonation.
- **Passwordless hub**: `/api/enroll` requires the master token even when no admin password is
  set, so a passwordless hub no longer hands its master token to unauthenticated callers.
- **Proxy hardening**: hop-by-hop headers are stripped on the websocket proxy path (smuggling),
  and the preview proxy is contained to a configurable dev-server port range (`previewPortMin` /
  `previewPortMax` in `config.json`, default 3000–9999) instead of any loopback port (SSRF).
- **Integrity**: token/id generation fails loudly instead of falling back to a guessable
  timestamp; the self-updater verifies checksums fail-closed (`--insecure` to override).
- **Injection**: project-registry markdown cells are escaped.
- **Resilience**: reconnect backoff gained ±20% jitter (no fleet-wide thundering herd); pty writes
  are serialized; the dashboard refetches REST state on websocket reconnect.
- **Dedup**: editor/preview proxies merged into a shared `tunnelproxy.go`; single constant-time
  token compare; shared websocket-send and `requireMethod` helpers; one shared agent reconnect
  loop; centralized dashboard hub-error parsing.

[0.1.4]: https://github.com/shleesauce/lattice/releases/tag/v0.1.4
[0.1.3]: https://github.com/shleesauce/lattice/releases/tag/v0.1.3
[0.1.2]: https://github.com/shleesauce/lattice/releases/tag/v0.1.2
[0.1.1]: https://github.com/shleesauce/lattice/releases/tag/v0.1.1
[0.1.0]: https://github.com/shleesauce/lattice/releases/tag/v0.1.0

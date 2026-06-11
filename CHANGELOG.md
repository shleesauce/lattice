# Changelog

All notable changes to Lattice are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-06-11

A deliberate, multi-session milestone: a four-pass security/resilience audit, a ground-up fix
for the reconnect-storm class (stable machine identity), and a frontend pass that makes the
console usable on a phone and reachable by keyboard / screen reader — plus cross-OS installer
gaps closed.

### Added
- **The dashboard works on phones and tablets.** It had no responsive layout at all, so the
  default Fleet view overflowed off the side of a small screen. It now reflows: on a phone the
  three-pane control room stacks into one scrollable column, and the top bar collapses to its
  essentials. Desktop is unchanged.
- **You can drive the fleet from the keyboard and with a screen reader.** Machine rows in the
  rail are now real buttons (Tab to them, Enter/Space to select), the mesh map is keyboard-
  navigable with arrow keys and announces the selected machine, and dialogs (the ⌘K palette,
  modals) trap focus so Tab can't wander off behind them.
- **Native Windows-on-ARM build.** Lattice now ships a `windows-arm64` binary, and the Windows
  installers detect your CPU architecture and pull the right one — so an ARM Windows machine no
  longer silently installs the x64 build to run under emulation.

### Changed
- **Your machines now keep a stable identity.** A machine used to be identified by
  `hostname + OS`, which meant two machines with the same hostname collided, and a machine's
  identity was re-derived on every reconnect. Each machine now mints a persistent id
  (`~/.lattice/agent-id`) that the hub keys everything on; hostname and OS are display-only.
  Existing fleets keep their current identity (no re-enrollment, no lost sessions); two same-named
  machines now get distinct identities.
- **Reconnect storms are designed out, not patched.** If two agent processes ever claim one
  machine's identity (a stale orphan plus a fresh restart, say), the hub now detects the duel,
  keeps the newcomer, and banishes the loser for a minute so it stops retrying — with a loud
  warning and a phone notification. This retires the whole class of "superseded by reconnect"
  floods that earlier releases fought one cause at a time.
- **Animations respect your system and your battery.** The live mesh map's animation pauses when
  you've asked for reduced motion and when its browser tab is in the background, instead of
  redrawing 60 times a second forever.
- **Linux installs survive a reboot on their own.** The Linux installer now enables systemd user
  *lingering* automatically (best-effort), so a headless server's hub/agent comes back after a
  reboot without someone logging in first. If it can't, it falls back to telling you the one
  command to run.
- **A stuck machine can't stall a fleet update.** A one-click update now has an overall time
  budget: a wedged machine is marked "will update on next start" and the rest of the fleet
  proceeds, instead of one slow box holding up everyone.
- **Honest update status.** When the hub runs under a process manager it can't restart itself
  (e.g. pm2), the update flow now tells you a manual restart is needed and shows the command,
  instead of a green check followed by a silent reload onto the old version. And the version
  badge can now tell "you're up to date" apart from "couldn't reach the update check."

### Fixed
- **A render error no longer white-screens the whole console.** A crash in one part of the UI is
  now caught and shown as a recoverable error panel rather than a blank page.
- **A background panic no longer takes down the hub.** The hub's long-running internal loops now
  recover from an unexpected panic and restart themselves instead of dropping every agent and
  tunnel.
- **The Linux `curl … | sh` installer runs under real `/bin/sh`.** It used a bash-only option
  that aborts under dash/BusyBox — exactly the default shell on the Linux machines it targets —
  so the advertised one-liner died on the first line. Fixed.
- Multi-row database cleanups now run in single transactions, so an interrupted cleanup can't
  leave a half-finished state.

### Security
- **A hub with no admin password is no longer an open door.** The remote-command, update, and
  power endpoints now require the master enrollment token even when no admin password is set —
  previously, anyone who could reach the port on your tailnet could run commands on every machine
  in the fleet. (Set an admin password for the full browser experience.)
- **The first-run setup wizard can't be hijacked.** Claiming admin on a fresh hub now requires
  being on the same machine (loopback) or holding the master token, so a tailnet peer can't grab
  the admin slot during the setup window.
- **Installers and the self-updater refuse insecure downloads.** Both the bootstrap installers and
  the binary self-updater now require an HTTPS download source (loopback and an explicit opt-out
  aside), so a plain-HTTP source can't feed a tampered binary.
- Per-machine enrollment tokens widened to 128-bit; the two unauthenticated capability endpoints
  are now rate-limited per IP.

## [0.1.9] - 2026-06-10

### Changed
- **The dashboard no longer loads anything from the internet.** It used to pull two fonts from
  the Google Fonts CDN that nothing in the UI actually used — so every dashboard load made a
  render-blocking request to Google. Those are gone; Lattice ships the fonts it actually renders
  (Hanken Grotesk + IBM Plex Mono) locally. A self-hosted fleet console now starts fully offline,
  makes zero third-party requests, and paints a touch faster — no visual change.

## [0.1.8] - 2026-06-10

### Fixed
- **One-click update no longer orphans the Windows agent.** On Windows the agent restarted itself
  with `schtasks /End` + `/Run`, but `/End` didn't reliably kill the process that *initiated* the
  call — so the old and new agents kept running under one identity and flooded the hub with
  "superseded by reconnect" churn. The agent now exits cleanly after starting its replacement
  (macOS/Linux already get killed by their service manager), so an update leaves exactly one agent.
- **No overlapping fleet updates.** A second "Update now" while one is already running now returns a
  409 instead of kicking off a second cascade that double-restarts every agent.

## [0.1.7] - 2026-06-10

### Added
- **Preview Vite / Next dev servers, not just plain ones.** The dock's Preview tab now has a
  **Vite/Next** toggle. Framework dev servers emit root-absolute asset URLs (`/@vite/client`,
  `/_next/…`) that ignored the preview path prefix and failed to render; the toggle switches the
  proxy to a no-strip mode (`/fpreview/…`) that forwards the full path, and the dock shows the
  exact `--base=…` string to launch the dev server with (Next: use it as `basePath` + `assetPrefix`).
  So you can now preview a Vite or Next app on any machine from any device that reaches the hub.

## [0.1.6] - 2026-06-10

### Added
- **`LATTICE_RELEASES_API` override.** Point the release-notes panel and update check at a
  different releases endpoint — so a self-hosted fork tracks its OWN releases instead of being
  pinned to upstream (and the update cascade can be exercised against a local release server).

### Fixed
- **One-click fleet update is more reliable.** Agents now acknowledge the update *before*
  restarting their own service, instead of restarting first and racing the confirmation off
  the wire — the cause of the "agent did not respond in time" message during the 0.1.5 update
  even when the upgrade actually succeeded.
- **Honest per-machine update status.** The update view now shows each machine as **updated**,
  **pending**, or **failed**. A slow or sleeping machine that doesn't confirm in time shows an
  amber "pending — applies on restart" instead of a red failure, and an agent that drops offline
  mid-update is skipped immediately rather than stalling the rest of the fleet.

## [0.1.5] - 2026-06-09

### Added
- **Choose Claude's permission mode per session.** The New Session dialog has a permission-mode
  picker for Claude sessions — Ask / Accept edits / Plan / Auto / Bypass (defaults to Bypass) — and
  the Claude pane has a one-click control to cycle the mode live (sends Shift+Tab to the TUI, so it
  works on a phone where Shift+Tab isn't available). Groundwork for unattended runs: start an agent
  in Bypass/Accept-edits so it doesn't block waiting for approvals.
- **Pick Claude's model when you start a session.** The New Session dialog now has a model dropdown —
  Opus 4.8 (1M context) by default, plus Fable 5, Sonnet 4.6, Haiku 4.5 and the legacy Opus builds —
  with a fast-mode toggle. Before this, every session inherited Claude's stale config default.
- **Richer, accurate session cards.** Claude sessions now show precise live state — working, waiting
  on you (a permission prompt), or done — plus the model, context used, and running cost. It's driven
  by Claude Code's own lifecycle hooks (wired per-session, never touching your `~/.claude` settings)
  instead of guessing from terminal quiet, so "waiting on you" actually means waiting on you. When a
  session opens a pull request, the PR link shows up on its card and pings your phone once.
- **One-click updates — stop SSH-ing in to upgrade.** The header now shows the version you're running.
  When a newer Lattice is published on GitHub, that badge turns into an "Update available" alert with
  an **Update now** button: it updates the hub and then every online agent in lockstep — each binary
  checksum-verified before it's swapped — then reconnects you to the new build. No terminal, no
  per-machine commands.
- **Workflow templates: Implement an issue / Review a PR.** Paste a GitHub issue or pull-request URL
  and Lattice spins up a dedicated worktree session, pre-briefed for the task, placed on the best
  available machine.
- **Fire-and-forget: get pinged on your phone, approve from anywhere.** Flip "Ping my phone" on a
  Claude session and walk away. The moment that run goes quiet waiting on you — a permission prompt
  or the end of its turn — or it finishes, Lattice pushes a notification (ntfy) to your phone. The
  push carries **Approve / Deny** buttons that answer the prompt without opening a laptop, plus
  **Open** to jump straight into the session. Approve sends Enter; Deny sends Esc. The approve link
  is a single-use, expiring capability URL — no admin token rides on the notification — and your
  phone reaches the hub over your existing tailnet. Every idle / active / exit / approval transition
  is recorded to the `audit_log`. Tune the quiet threshold with `LATTICE_IDLE_SECS` (default 45s);
  notifications reuse the watchdog's `LATTICE_NTFY_TOPIC` / `LATTICE_NTFY_URL`, and the Approve/Deny
  buttons require the hub's canonical URL (`hubUrl` in `~/.lattice/config.json`).
- **Release notes in the app.** Settings → **Release notes** shows what each version brought, pulled
  live from GitHub releases through the hub (cached), with the version you're running flagged and an
  honest "up to date / vX available" line. Release notes now come straight from `CHANGELOG.md`: the
  release pipeline publishes each version's changelog section as the GitHub Release body, so the repo
  and the in-app panel always say the same thing. Groundwork for one-click updates.
- **Name your sessions.** Right-click (or double-click) a session in the sidebar to rename it inline —
  the name sticks. Fresh Claude sessions also name themselves: Lattice derives a short 3–5 word title
  from your first message (the way Claude Code titles a session), so the sidebar reads like a task list
  instead of a wall of "claude". Auto-naming is a free local heuristic — never an extra model call —
  and a name you set by hand always wins and is never overwritten.
- **Smarter Wake-on-LAN: the wake routes through a machine that can actually reach the sleeper.**
  Waking a sleeping machine now picks a *live* machine on the **same LAN/subnet** as the target to
  send the magic packet — a broadcast from a machine on a different network would never arrive, so
  this is the difference between "it wakes" and a silent no-op. If no awake machine shares the
  sleeper's network, Lattice says so ("no relay reachable on that subnet") instead of pretending it
  worked. Agents report their private LAN addresses so the hub can match subnets; the target's MAC is
  remembered, so you just click Wake — no addresses to enter.
- **Wake-then-place in one step.** Starting a session on a sleeping (but otherwise eligible) machine
  now wakes it, waits for it to rejoin the fleet, and then starts the session — one action instead of
  "wake it in Fleet, wait, then come back and place."
- **Sleep or shut down a machine from Lattice.** Each agent can suspend or power off its own machine
  on request, closing the unattended loop: wake a box, run work on it, put it back to sleep — all
  without touching it.

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

[0.1.5]: https://github.com/shleesauce/lattice/releases/tag/v0.1.5
[0.1.4]: https://github.com/shleesauce/lattice/releases/tag/v0.1.4
[0.1.3]: https://github.com/shleesauce/lattice/releases/tag/v0.1.3
[0.1.2]: https://github.com/shleesauce/lattice/releases/tag/v0.1.2
[0.1.1]: https://github.com/shleesauce/lattice/releases/tag/v0.1.1
[0.1.0]: https://github.com/shleesauce/lattice/releases/tag/v0.1.0

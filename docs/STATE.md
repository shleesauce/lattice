# Lattice — Live State / Handoff

**Read this first every session.** Update it at the end of every working session: what's
done, what's in flight, what's next, what's blocked. This is the source of truth for resuming.

---

## Current phase
**Phases 1, 2, 3 (Workspace) & 4 COMPLETE & verified on the real fleet (2026-05-29).** The SUCCESS
CRITERION (packageable) is MET and the workspace is built + verified (D15–D25).

**IDE Milestone (M2) — P1 BUILT & VERIFIED END-TO-END on mini-ops (2026-05-30).** The embedded editor
works: real VS Code (code-server 4.112.0) renders inside Lattice through the hub, proxied over the
yamux tunnel (D27), with file tree + git + extension-host WebSocket all live. Every DONE-WHEN met
(see "### P1 — DONE & VERIFIED" below). **NEXT: P2** (Monaco-rail retirement D31 in the workspace UI +
multi-machine code-server install/verify on studio/mbp/pc + the Cursor-grade AI chrome D29).

**IDE Milestone (M2) — architecture DISCUSSED & DECIDED with Dylan (2026-05-29).**
Turn the workspace into a real IDE (compete with Cursor / VS Code / Claude Code desktop / Codex /
T3 Code): deep editor abilities + a Cursor-grade AI experience, all over the mesh, as a distributable
product. **Decisions D26–D31 recorded** (docs/DECISIONS.md); roadmap **P1–P4** in docs/ROADMAP.md;
full spec/plan at `~/.claude/plans/rippling-wishing-candy.md`.

### IDE architecture (ratified D26–D31)
- **D26** EMBED **code-server** as the editor core; do NOT fork VS Code (supersedes D16). Keep the
  Claude chat + sessions + placement + onboarding as OUR chrome around it.
- **D27** Expose the per-agent editor via a **second dial-out WS tunnel multiplexed with yamux** (hub
  reverse-proxies `/editor/{sessionId}/*` → `yamux.OpenStream` → agent → local code-server) —
  preserves D2 (zero inbound on leaves). **Biggest technical piece; build/spike first.**
- **D28** Distribute code-server via **hub-as-distribution** (extends D14); a new on-demand **`editor`**
  session kind reusing the D18 lifecycle + D19 placement; fetched+cached on first use, torn down with
  the session.
- **D29** AI **chrome-first** (reuse the Phase-3 Claude runner beside the editor) → a **Lattice VS Code
  extension** (Cmd-K, autocomplete, in-workbench chat) later.
- **D30** Editor on **all four machines**; Windows runs **code-server inside WSL2** (`/mnt/c`).
- **D31** The embedded VS Code **replaces the read-only Monaco rail**; the sidebar drives it.
- **Mobile / same-URL (D7/D15):** every layer is reachable from a phone browser at the SAME hub URL
  (`http://mini-ops.tail3c8bee.ts.net:7400`) over the tailnet — no app install, Tailscale is the only
  prerequisite. The Lattice chrome (fleet, Claude chat, terminal, sessions, file nav) is already
  mobile-responsive (drawer sidebar + `md:` breakpoints). The embedded editor (D27) renders through the
  hub too (phone → hub only, never direct to the agent → works on CGNAT/mobile data), BUT code-server's
  workbench is desktop-grade — usable on a phone, not optimized. Realistic split: phone = chat/terminal/
  quick peeks; laptop = heavy editing. A mobile-friendly editor affordance is a P3/P4 nice-to-have.

### P1 spike — code-server subpath proxy (2026-05-30): ✅ PASS (visually verified)
**code-server 4.112.0** (Code 1.112.0) on mini-ops. Stood up a stdlib Go reverse proxy
(`/tmp/cs-spike/proxy.go`, v3 ~180 lines) mapping `/editor/test/*` → 127.0.0.1:9444. **Playwright
loaded the full VS Code workbench through the subpath** — file tree showed `hello.js`, extension host
connected, WS stayed alive, no blocking console errors. Screenshot `/tmp/cs-spike-shot.png` (101 KB,
1280×720, valid PNG — I viewed it). code-server emits **relative** asset URLs, so a trailing slash
makes everything resolve under the prefix.

**Verified recipe the hub MUST implement** (more than the trailing-slash redirect — two extra pieces):
1. **Director:** strip the `/editor/{id}` prefix; set `X-Forwarded-Host`=hub host, `X-Forwarded-Proto`,
   `X-Forwarded-Prefix=/editor/{id}/`; set the upstream `Host` to code-server.
2. **302 redirect** `/editor/{id}` → `/editor/{id}/` (trailing slash MANDATORY).
3. **ModifyResponse:** rewrite `Location` headers — code-server 302s with a RELATIVE `./?folder=…`;
   resolve it against the prefix so the browser stays under `/editor/{id}/` (else infinite redirect loop).
4. **WebSocket tunnel:** `httputil.ReverseProxy` strips hop-by-hop `Upgrade`/`Connection`, so the
   extension-host WS never upgrades. Detect `Upgrade: websocket` → bypass ReverseProxy → `net.Dial` the
   backend + `http.Hijack` the client + bidirectional `io.Copy`.
5. **code-server flag `--trusted-origins`** = the hub host (use `*` for dev) — its `authenticateOrigin()`
   compares the browser `Origin` to the backend `Host` and **403s the WS** without this. Plus `--auth
   none` (tailnet + hub already gate access, D2/D3) + `--disable-telemetry`.
Harmless non-blockers: a 404 on optional `vsda.js` (code-signing WASM, absent in this build) + an
ERR_ABORTED on the webview iframe (browser sandbox) — workbench fully functional.

**Footprint / D28 note to settle in P1:** the brew cellar is ~384 MB (full VS Code + Node runtime). The
standalone release tarball is ~95 MB compressed — viable for hub-as-distribution (D28), but the simpler
alternative is **per-node install** (brew / official `install.sh`) with the hub just spawning+proxying.
Decide in P1: hub-served tarball (keeps D28's "no per-machine install") vs per-node install (simpler,
heavier per box). The WSL2 Windows path (D30) likely wants the Linux tarball inside WSL regardless.

Reference impl + evidence: `/tmp/cs-spike/proxy.go`, `/tmp/cs-spike-shot.png`, `/tmp/cs-server.log`.

### P1 — DONE & VERIFIED END-TO-END on mini-ops (2026-05-30) ✅
The embedded editor is built and proven on the real fleet. **D28 distribution decision (settled):
per-node install** for P1 — the agent detects an existing `code-server` (`resolveCodeServer`: PATH →
brew/`/usr/local` → `~/.local/bin`) and just spawns+proxies it; placement excludes machines without it.
Hub-served tarball stays a clean future add (the resolver is the only seam that'd grow a fetch path).

**Architecture as built (Arch-1: hub terminates HTTP, agent is a dumb per-session pipe):**
- **Shared** `internal/tunnel/` — `WSConn` (gorilla `*websocket.Conn` → `net.Conn`, binary frames),
  `Config()` (yamux keepalive 30s / write-timeout 10s), and the stream handshake (`WriteStreamHeader`/
  `ReadStreamHeader`: the hub writes `sessionId\n` as the first bytes of every stream so the agent
  routes it). yamux = `github.com/hashicorp/yamux v0.1.2`.
- **Hub** — `tunnel.go` (`/ws/tunnel?token&agent`: token-gated, `yamux.Server`, per-agent session in a
  registry that closes a stale prior session on reconnect). `editorproxy.go` (`/editor/{sessionId}/*`:
  the proven spike recipe verbatim — prefix strip + `X-Forwarded-*`, relative-`Location` rewrite, the
  trailing-slash 302, manual WS hijack — but `Transport.DialContext` opens a yamux stream + writes the
  handshake instead of a TCP dial). Routes in http.go; `kind=editor` accepted in sessionapi.go +
  `code-server not installed` exclusion in placement.go.
- **Agent** — `editor.go` (`editorSessions` registry mirroring claude/terminal: spawns code-server on a
  free `127.0.0.1:PORT`, per-session `--user-data-dir` + shared `--extensions-dir`, waits for the port,
  supervises, tears down on close). `codeserver.go` (resolver + WSL probe). `tunnel.go` (2nd dial-out to
  `/ws/tunnel` with its own reconnect/backoff; `yamux.Client`; Accept loop reads the handshake →
  `addrFor(sessionId)` → `net.Dial` loopback → bidirectional `io.Copy`). `state.go` 3rd registry +
  `setSink`; `capabilities.go` probes code-server/WSL; `SessionEditor` cases in agent.go.
- **code-server argv:** `--bind-addr 127.0.0.1:PORT --auth none --trusted-origins * --disable-telemetry
  --disable-update-check --disable-workspace-trust --user-data-dir <base>/<sid> [--extensions-dir
  <shared>] <cwd>`.
- **Frontend** — `SessionEditor.tsx` (iframe `src=/editor/{id}/`, loading overlay), `editor` tab in
  SessionPane (TAB_ORDER), `editor` kind in NewSessionDialog (gated on `codeServerInstalled` for device
  sessions), types.ts `SessionKind`+`Capabilities`. *(Monaco rail NOT yet retired — that's P2/D31.)*
- **Proto** — `SessionEditor` kind + `Capabilities.CodeServerInstalled/Version` + `WSLAvailable`.

**DONE-WHEN — all verified (real browser via Playwright + lsof + on-disk):**
1. ✅ open a project on mini-ops → full VS Code workbench renders through the hub (screenshot: lattice
   file tree, `master*`, 21-change SCM badge). 2. ✅ edit+save → marker written to disk via the editor.
   3. ✅ git works (M markers / SCM badge); search panel present. 4. ✅ **no new inbound listener on the
   agent** — agent has ZERO listening sockets (`lsof -a -p AGENT -iTCP -sTCP:LISTEN` empty), only two
   outbound loopback conns to :7400 (`/ws/agent`+`/ws/tunnel`); code-server bound `127.0.0.1` only.
   5. ✅ **hub restart re-adopts** — session stays `live`, tunnel re-establishes, `/editor/{id}/` serves
   200, SAME code-server pid survives. Editor close tears code-server down cleanly.

### P2 — IN PROGRESS
- ✅ **One-click "Open Editor" per project (2026-05-30).** Each project row in the Sidebar has an
  Open-Editor (`</>`) action (shown only when an online machine has code-server — `editorAvailable`).
  It **creates-or-reuses a single editor session per project** (never spawns a 2nd code-server for the
  same project) and opens it embedded in the Lattice shell. Verified on mini-ops via Playwright: click →
  VS Code workbench mounts INSIDE the Lattice iframe (2 editor WS), 2nd click reuses (session count
  stays 1), editor tab/glyph/labels render. (Workspace.tsx `onOpenEditor`/`editorAvailable`, Sidebar.tsx
  CodeIcon button + editor KindGlyph.)
- **Monaco-rail retirement (D31) — DEFERRED until code-server is fleet-wide.** Fully removing
  ProjectFilesPanel/FileViewer/MonacoPanel/useFileBrowser now would regress file browsing on
  studio/mbp/devices (no code-server there yet). Keep the read-only rail as a graceful fallback; retire
  it once the editor is on every machine. Sidebar click-file → open-in-embedded-editor is the follow-on.
- Install code-server on studio/mbp/pc and verify the editor cross-machine (Windows = code-server inside
  WSL2, D30 — `codeserver.go`/`detectWSL` already gate this; the spawn path needs a WSL `wsl.exe -e`
  wrapper).
- Cursor-grade AI chrome beside the editor (D29): reuse the Phase-3 Claude runner; later a Lattice VS
  Code extension (Cmd-K / autocomplete / in-workbench chat).

### Build approach (unchanged from prior phases)
Parallel subagents per phase, **adversarially verified against the real fleet** (mini-ops/studio/mbp/pc)
— never trust an implementer agent's self-report. Rebuild `bash scripts/build.sh` → `pm2 restart
lattice-hub`; deploy agents via the hub installer (launchd LaunchAgents — D22 / Windows Scheduled Task);
token in `.lattice-token`; push to `origin/master`.

---

## Prior milestone (Phase 3 Workspace) — DONE & VERIFIED, kept for reference below
The reframed Phase 3 shipped: Projects→Sessions + Devices sidebar, long-lived Terminal + Claude
(stream-json, Max subscription) sessions surviving browser detach + hub restart, smart placement, the
onboarding wizard, and the (now-to-be-retired) read-only Monaco file rail. Full detail follows.

## Phase 3 (Workspace) — DECIDED (D15–D25), BUILT & VERIFIED  [D16 superseded by D26 for the IDE milestone]
- **D15** shell: browser-first SPA now → **Tauri** wrapper later (bundles the Go agent as a
  sidecar). **D16** editor: lean (file tree + Monaco + the two tabs), code-server dropped.
  **D17** Claude tab: the LOCAL `claude` binary headless in stream-json (subscription; verified
  flags) — NOT the pay-per-token Managed Agents API. **D18** persistence: first-class Session
  entity, processes outlive the browser (the core fix — today PTYs die with the browser WS).
  **D19** placement: capability filter (Claude needs `claude` present — mbp lacks it) + headroom +
  locality boost, visible/overridable. **D20** portability: placed + resumable (not live-migrated).
  **D21** trust: skip-perms default + audit log + per-machine approval kill switch.
- **Build = parallel workstreams (WS-1 proto gates):** see ROADMAP.md Phase 3 for WS-1…WS-9.
- **Fleet facts that drive the build:** claude installed on mini-ops/studio/pc, NOT mbp; node
  everywhere; studio already aliases `claude --dangerously-skip-permissions --permission-mode
  bypassPermissions`; AI-Hub + ~/.claude are Syncthing-synced fleet-wide.

## Phase 4 — what shipped and is VERIFIED (the success criterion)
Self-hosted, no-cloud: **the hub is the distribution + enrollment point**.
- Hub serves: `GET /dl/{name}` (agent binaries from `--dist`, default `dist/`; strict
  basename allowlist + path.Base → traversal-safe), `GET /install.sh` + `GET /install.ps1`
  (rendered from go:embed templates with the hub URL baked from the request `Host`),
  `GET /api/enroll` → {hubUrl, token, unix one-liner, windows one-liner}.
- Installers: detect OS/arch, download the agent from the hub, install a **persistent service**
  — launchd LaunchAgent (macOS), systemd --user unit (Linux), per-user logon Scheduled Task
  (Windows) — and enroll via token. Idempotent. Token passed at runtime, never baked in.
- Dashboard "Add device" modal: copy-paste join command (macOS/Linux + Windows tabs).
- **VERIFIED on real hardware:** re-provisioned mbp via `curl …/install.sh | sh -s -- --token …`
  → launchd service installed, KeepAlive-respawn confirmed, rejoined mesh. Provisioned pc via
  `install.ps1` → Scheduled Task `LatticeAgent` Running, rejoined mesh. One command per machine.
- NOT done (deferred): GitHub Releases / Homebrew tap / winget manifest (the public-distribution
  channel). The private-mesh story — hub-as-distribution — is complete and is the stronger
  packageability proof. Windows-on-ARM build + detection deferred (amd64 runs under emulation).

## Workspace UX — DECIDED (D15–D21) + Phase 3 BACKEND/FRONTEND BUILT & VERIFIED on the fleet (2026-05-29)
The 7 questions resolved as D15–D21. Backend (proto + sessions + claude runner + placement +
re-discovery + audit) and frontend (workspace shell: Projects→Sessions sidebar, Terminal + Claude
tabs, machine chip, Monaco) are BUILT, building green (`go build/vet/test`, `npm run build`), and
**verified against the real fleet**. Build plan: ROADMAP Phase 3 + `~/.claude/plans/stateful-
watching-ripple.md`. New decisions from verification: **D22** (Claude needs a launchd LaunchAgent for
Keychain auth), **D23** (projectPath portability gap — follow-up).

### VERIFIED working (exercised the running system, not just code)
- **Placement (D19):** `/api/placement` + `/api/sessions` score correctly — mini-ops/studio eligible
  with RAM/load/cores breakdown + locality; **mbp hard-excluded "claude not installed"**; pc "offline";
  pin override works. `/api/projects` returns the 24 synced projects.
- **Claude tab (D17) — END TO END:** a Claude session on studio's launchd agent returned a real
  **"PONG"** over `/ws/session` with real token usage (in 13700 / out 5 / cache 30028); full
  stream-json (`system→stream_event→user→assistant→result`) replayed + streamed live. Uses the **Max
  subscription** (no API key). Hub-assigned `--session-id` ⇒ Lattice sessionId == Claude sessionId.
- **Persistence (D18) — END TO END:** terminal process outlives browser detach; **scrollback replays
  on reattach**; session + process **survive a HUB RESTART**, are re-adopted (status live), scrollback
  intact (1053 B), and **live I/O still works** after the restart.
- **Frontend:** workspace shell renders (WORKSPACE|FLEET toggle, Projects→Sessions sidebar, empty-state),
  matches the dark emerald/zinc aesthetic. Screenshot: /tmp/lattice-workspace.png.

### Device projects (D24) — ADDED & VERIFIED (2026-05-29, same session)
Sessions now have a **`scope`**: `project` (synced, auto-placed) or **`device`** (pinned to one machine,
cwd = that machine's **home**) — for machine-local work (set up programs, organize files, admin a box).
UI: a **DEVICES** section under PROJECTS (fleet, online-first, CLAUDE capability chip, offline dimmed),
each device → its device sessions + "+ new session"; static machine chip for device sessions. Strict
placement (runs on the device or fails; capability filter still applies). Agent resolves empty/`~` cwd to
home (the hub can't — home paths differ per box; partially addresses D23). **Verified on the fleet:**
device terminal on mini-ops → `pwd`=`/Users/mini-ops`; device Claude on mbp → 400 "claude not installed";
device Claude on studio → real "PONG" in its home. Schema: idempotent `sessions.scope` ALTER migration.

### Right-side file explorer (IDE-style) — ADDED & VERIFIED (2026-05-29)
3-column workspace layout: left Projects/Devices sidebar | center session panes | **right file rail**.
Clicking a PROJECT label opens its file tree on the right (browsed via the hub's **local agent** —
`Agent.local` — at the project's hub-host path, so no sync needed), with breadcrumb nav + a lazy Monaco
read-only viewer (click a file → syntax-highlighted content; size-capped + download for big/binary).
Clicking a DEVICE label opens that machine's home files via its own agent (bonus). The in-session Monaco
panel was removed (the right rail is now the sole browser). Below md the rail overlays as a drawer.
Verified: clicked `lattice` → tree rendered; clicked `.gitignore` → viewer showed it. Shared engine:
`useFileBrowser.ts` + `FileViewer.tsx` + `ProjectFilesPanel.tsx`. Follow-up: write-back is not wired
(read-only viewer); file edits still go through a Claude/terminal session.

### "Begin new project" onboarding wizard (D25) — ADDED & VERIFIED (2026-05-29)
A `POST /api/projects` endpoint + a 4-step workspace wizard ("+ new project" in the PROJECTS header):
name → folder (live kebab + collision check) → stack/port → connectors/MCPs/agents/related/envs →
review (register + auto-launch toggles). The hub scaffolds the AI-Hub skeleton (README, CLAUDE.md,
docs/PROJECT_CONTEXT, **docs/ONBOARDING.md brief**, .env, .gitignore, .claude, git init) directly in
projectsRoot (syncs everywhere), registers it (row in **~/AI-Hub/UNIVERSAL_RULES.md** — NOT CLAUDE.md —
+ regenerate PROJECT_INDEX.md + KB stub), and launches a Claude session on the **local agent** (loopback
detection → `Agent.Local`, so files are co-located) **seeded** with the onboarding brief. Connectors/etc.
are captured as intent for Claude to wire. **Verified end-to-end** (scaffold/register/launch/seed) on the
fleet; test project fully reverted. Caveat: the onboarding session lands on mini-ops which can't auth
claude WHILE this Claude-desktop session monopolizes it (the known local-agent contamination; works on a
clean box). Follow-ups: auto-link the KB stub into wiki `_index`/`_map`; optional `/gsd:new-project` seed.

### Bugs found & fixed THIS session (via adversarial fleet verification)
1. `claude --print --output-format=stream-json` **requires `--verbose`** — launcher omitted it (instant exit). FIXED.
2. Launcher now **scrubs `ANTHROPIC_API_KEY`/`CLAUDECODE`/`CLAUDE_CODE_*`** from the child env → forces subscription auth (also the cost rule). FIXED.
3. **D18 core bug:** long-lived PTY/claude were spawned from the **per-connection context**, so a hub
   restart (agent↔hub link drop) KILLED every session. Now rooted at the **process-global base context**
   (newAgentState(ctx) → registries' baseCtx). FIXED + re-verified across a hub restart.
4. **D22 (operational):** macOS Claude auth needs a **launchd LaunchAgent** (GUI-session Keychain), not
   a nohup daemon — studio reprovisioned via the hub installer; "Not logged in" → real "PONG".

### Fleet state after this session
- mini-ops: hub + local agent under PM2 (new binary, claude=true). studio: **launchd LaunchAgent**
  (`sh.lattice.agent`, new binary, claude=true) — reprovisioned from nohup. mbp: launchd (new binary,
  claude=false — correct, no claude installed). pc: OFFLINE (asleep) — still on old binary, redeploy when awake.
- Cosmetic: a stale offline `studio` agent record (old `--name studio`) coexists with the launchd
  `Dylans-Mac-Studio.local` record — agentID = hostname+os, and the installer uses the real hostname
  while deploy-fleet used `studio`. Harmless dup; clean up by removing the offline row.

### NOT YET DONE (follow-ups)
- **D23 projectPath portability** (name-relative resolution) — needed for true cross-machine resume.
- **Cross-machine Claude resume** (orphan on A → `--resume` on B) not yet exercised end-to-end (needs D23 + Syncthing-timing retry).
- **Audit log + approval kill switch** wired in backend but not yet exercised/verified.
- **Interactive frontend QA** (click into a session, drive the Claude chat UI in-browser) — only the empty-state was screenshotted.
- **pc (Windows)** redeploy + Claude-session auth on Windows (credential store) untested.
- **WS-9 Tauri** desktop packaging — not started (deferred sub-phase).
- Monaco write-back, projects served per-machine vs hub-only listing.

## Phase 2 — what shipped and is VERIFIED
- **Interactive terminal:** per-agent PTY via `github.com/aymanbagabas/go-pty` (unix + Windows
  ConPTY, CGO-free). Dashboard xterm.js terminal over `/ws/terminal?agent=<id>`. Verified: real
  zsh session on mbp through the browser (independent Playwright agent) + WS-level echo test.
- **File browser:** scoped list (`GET /api/agents/{id}/files?path=`) + download
  (`GET /api/agents/{id}/download?path=`), dirs-first, breadcrumb + up-nav. Verified: listed
  mini-ops /tmp + PC `C:\` (cross-OS), downloaded a seeded file, browser UI renders 30+ entries.
- **Wake-on-LAN:** agent broadcasts a magic packet (SO_BROADCAST, unconnected socket +
  per-interface directed broadcasts — macOS needs this). **Verified end-to-end on real hardware:**
  slept the PC → it dropped offline (still visible via offline-retention) → POST wake from the
  dashboard (mini-ops agent as LAN sender) → PC woke + agent reconnected in ~35s.
- **Offline-machine retention:** hub `fleet()` merges persisted (offline, last-known metrics+MACs)
  with the live registry, so a sleeping machine stays visible and wakeable. Agents report their
  MACs in the heartbeat → Wake is turnkey (no manual MAC entry).
- Reviewer majors fixed: WoL SO_BROADCAST + macOS broadcast path; PTY `explicitClose` data race
  (atomic.Bool).

## Phase 1 — what shipped and is RUNNING
- Single Go binary (`github.com/dylanstoryyy/lattice`), roles `hub`/`agent`. Go 1.26.3 installed.
- **Hub LIVE under PM2 on mini-ops** (`pm2 ls` → `lattice-hub`, port 7400), reachable at
  `http://mini-ops.tail3c8bee.ts.net:7400`. SQLite at `lattice.db` (WAL). PM2 list saved.
- **4/4 agents online:** mini-ops (local agent, PM2 `lattice-agent`), studio, mbp (darwin/arm64),
  pc (windows/amd64). Macs started via `scripts/deploy-fleet.sh` (nohup); PC via WMI
  `Win32_Process.Create` (session 0, survives SSH logout).
- Dashboard (React/Vite/Tailwind dark "fleet console") embedded in the hub binary, served at `/`.
- **Verified E2E:** WS-level test ran `uname -a` on 3 Macs + `ver` on PC, all exit 0 with real
  output; independent Playwright agent drove the browser UI (select machine → type → Run →
  streamed output + exit code rendered). Screenshots: /tmp/lattice-dash.png, /tmp/lattice-ui-after.png.

## Key implementation decisions (this session)
- **Pure-Go SQLite (`modernc.org/sqlite`)** so the whole mesh cross-compiles `CGO_ENABLED=0`
  from mini-ops → 5 targets (darwin arm64/amd64, windows amd64, linux amd64/arm64). See D10.
- gorilla/websocket; gopsutil/v4 for heartbeat metrics; single-writer per conn.
- Enrollment token in `.lattice-token` (gitignored): `latt-631134d87f44`.
- Hardened after adversarial review: hub read+write deadlines (half-open socket cleanup),
  agent backoff reset after healthy session, exec rejected to swept-offline agents, stable
  fleet ordering.

## Build / deploy / run (operational)
- Build all: `bash scripts/build.sh` (dashboard → embed → cross-compile to dist/).
- Deploy agents: `bash scripts/deploy-fleet.sh [all|studio|mbp|pc]`.
- Hub: `pm2 start scripts/pm2-hub.config.cjs` / `pm2 restart lattice-hub`.
- Open: `http://mini-ops.tail3c8bee.ts.net:7400`.

## Environment facts (verified 2026-05-29)
- Hub host = mini-ops; port 7400; build host = mini-ops (cross-compile all targets).
- Full SSH mesh + passwordless sudo across the fleet — use it to deploy/verify.
- PC live-adapter MAC for WoL (Phase 2): `30-56-0F-4C-4D-3A` (Ethernet 3, Realtek, on LAN).
- Dogfood fleet + per-OS quirks: see FLEET.md.

## Known minor items (non-blocking, deferred)
- Dashboard pulls Google Fonts over CDN — vendor them for true offline/privacy before release.
- Agent persistence is light (nohup / Win32 detached) — proper per-OS services are Phase 4.
- agentID = hostname+os; add a hardware-id/nonce before shipping (two same-hostname boxes collide).

## Build/verify loop (use every phase)
After implementing, VERIFY by exercising the running system, not by reading code:
- SSH into each target, confirm the agent process/service is up.
- `curl` the hub API; assert fleet membership + status.
- Playwright screenshot the dashboard; assert machines render live.
- Run a command through the full UI→hub→agent→back loop; assert real output.
Spawn verification subagents for this; don't trust an implementer agent's self-report.

## Open questions for Dylan (don't block on these; pick sane defaults and note them)
- Final product name (Lattice is provisional).
- Whether to run the hub under PM2 (recommended, matches homebase) or launchd.

## Changelog
- 2026-05-29: Phase 0 scaffold created (originating session, ~50% context, before handoff).

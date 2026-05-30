# Lattice — Live State / Handoff

**Read this first every session.** Update it at the end of every working session: what's
done, what's in flight, what's next, what's blocked. This is the source of truth for resuming.

---

## Current phase
**Phases 1, 2 & 4 COMPLETE & verified on the real fleet (2026-05-29).** The SUCCESS CRITERION
(packageable) is MET. **Phase 3 (Workspace) is now BUILDING** — reframed from "proxy code-server"
into the Claude-Code/VS-Code-style mesh workspace. UX/architecture DISCUSSED & DECIDED with Dylan
(2026-05-29): decisions **D15–D21** recorded; reframed plan in ROADMAP.md Phase 3; full spec/plan
at `~/.claude/plans/stateful-watching-ripple.md`.

## Phase 3 — DECIDED (D15–D21), now building
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

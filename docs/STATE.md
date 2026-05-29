# Lattice — Live State / Handoff

**Read this first every session.** Update it at the end of every working session: what's
done, what's in flight, what's next, what's blocked. This is the source of truth for resuming.

---

## Current phase
**Phase 1 COMPLETE & verified (2026-05-29).** → Next: Phase 2 (xterm.js terminal +
file browser + wake-the-PC). See ROADMAP.

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

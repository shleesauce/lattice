# Lattice — Roadmap

Build order is chosen so each phase produces something **testable in a browser** and
dogfoodable on Dylan's fleet. The success criterion (packageable) is Phase 4 — but every
earlier phase is structured so it doesn't have to be redone to get there.

## Phase 0 — Scaffold ✅ (done in the originating session)
Repo, docs, fleet reference, kickoff prompt. No code yet.

## Phase 1 — Heartbeat + Registry + Remote Exec  ✅ DONE & VERIFIED (2026-05-29)
The proof the model works.
- Go module; `lattice` binary with `hub` and `agent` subcommands.
- Agent: dial hub over WebSocket, register, heartbeat {hostname, os, arch, uptime, disk, online}.
- Hub: accept agent WS, persist registry in SQLite, expose REST `/api/fleet`, WS for live updates.
- Hub: dispatch a one-shot command to a chosen agent; stream stdout/stderr back.
- Dashboard: fleet grid (live status dots) + a command panel with streaming output.
- Cross-compile agent for darwin-arm64 (mini-ops/studio/mbp), windows-amd64 (pc).
- **Dogfood:** hub on mini-ops, agents on studio + mbp + pc.
- **DONE WHEN:** open `http://mini-ops.<tailnet>:7400` in a browser, see 4 machines live,
  run `uname -a` on the Macs and `ver` on the PC from the UI and watch output stream back.

## Phase 2 — Interactive terminal + file browser + Wake-on-LAN  ✅ DONE & VERIFIED (2026-05-29)
- Per-agent PTY ↔ xterm.js full interactive terminal in the dashboard.
- Scoped file-tree browse + download per agent.
- `wake` action: agent on a LAN sends a magic packet to a sleeping peer (PC is the live
  case — Ethernet, NIC already wake-armed; see FLEET.md).
- **DONE WHEN:** real shell session to any machine in the browser; browse a remote tree;
  wake the PC from sleep via the dashboard.

## Phase 3 — Workspace  🔨 BUILDING (reframed 2026-05-29; the real product)
**Reframed** from "proxy code-server" (old D8) into a Claude-Code/VS-Code-style mesh workspace —
see docs/VISION-WORKSPACE.md + decisions D15–D21. A Projects→Sessions sidebar over the synced
`~/AI-Hub/projects/*`; per session a **Terminal** tab and an already-live **Claude** tab (local
`claude` headless stream-json on Dylan's subscription); cross-machine sessions with smart-but-
visible/overridable hub placement; sessions that outlive the browser; later a Tauri desktop app.

Build order (parallel workstreams; proto gates):
- **WS-1 proto** (gate): session lifecycle + claude channel + capabilities + re-discovery messages.
- **WS-2 store**: `sessions` / `audit_log` / `settings` tables + methods.
- **WS-3 terminal decoupling**: process-global agent registry, scrollback ring, swappable sink,
  `handleSessionWS` (attach/detach/replay) — sever PTY lifetime from the browser WS.
- **WS-4 claude runner** (`internal/agent/claude.go`): spawn + supervise the local `claude` binary
  in stream-json; frame stdin/stdout over the agent WS; resume-by-session-id.
- **WS-5 placement** (`internal/hub/placement.go`): capability filter + headroom + locality scorer;
  `/api/placement` (preview) + `/api/sessions` (create).
- **WS-6 re-discovery + audit**: register/heartbeat capabilities + live-session re-adoption; orphan
  reconciliation; audit-log writes + approval kill switch.
- **WS-7 capabilities probe** (`internal/agent/capabilities.go`).
- **WS-8 frontend**: sidebar, tabs, Claude chat renderer + token-usage HUD, machine chip, Monaco.
- **WS-9 Tauri** packaging (after the SPA is verified): wrap the SPA + bundle the agent sidecar.

- **DONE WHEN:** from a browser on mini-ops, open a project, start an already-live Claude session
  (auto-placed on a capable machine, mbp excluded; machine visible + overridable), chat with tool
  calls + live token usage; open a Terminal tab; refresh the browser and the sessions are still
  live with replayed scrollback; restart the hub and sessions are re-adopted. Tauri app installs as
  a single artifact carrying its own agent.

## Phase 4 — Packaging + onboarding  ✅ DONE & VERIFIED (2026-05-29) — THE SUCCESS CRITERION
Done via hub-as-distribution (no GitHub needed for a private mesh): hub serves binaries +
rendered installers + /api/enroll; installers install a persistent OS service; dashboard
"Add device" onboarding. Verified on real mbp (launchd) + pc (scheduled task). Public-channel
distribution (GitHub Releases / Homebrew / winget) is the only deferred sub-item. Original text:
- `install.sh` (mac/linux), `install.ps1` (windows), termux installer: detect OS/arch,
  fetch binary, install as a service, enroll via one-time token.
- "Create your mesh" onboarding: run `lattice hub init` → get a join command → paste on each
  device. A wizard in the dashboard to add/name/group devices.
- GitHub Releases with prebuilt binaries; Homebrew tap; winget manifest.
- **DONE WHEN:** on a fresh machine that has never seen this project, a single documented
  command installs the agent and it appears in the dashboard — no manual per-OS steps.

## Explicitly deferred / non-goals (for now)
- iOS agent (no good sideload-free path) — phone story is Android/Termux only, last.
- Multi-user / teams / sharing someone else's mesh — single-owner first.
- Auth beyond tailnet + enrollment token — don't gold-plate before Phase 4.

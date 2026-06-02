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

## Phase 3 — Workspace  ✅ DONE & VERIFIED (reframed 2026-05-29; the real product)
**Reframed** from "proxy code-server" (old D8) into a Claude-Code/VS-Code-style mesh workspace —
see docs/VISION-WORKSPACE.md + decisions D15–D21. A Projects→Sessions sidebar over the synced
`~/AI-Hub/projects/*`; per session a **Terminal** tab and an already-live **Claude** tab (interactive
local `claude` in a PTY on Dylan's subscription, D35 — was headless stream-json); cross-machine sessions with smart-but-
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

---

# IDE Milestone (M2) — compete with Cursor / VS Code / Claude Code desktop  🔨 NEXT (decided 2026-05-29)

Turn the workspace into a real IDE: deep editor abilities (edit/save, search, git, LSP/intellisense,
debugging, extensions) + a Cursor-grade AI experience, all over the mesh, as a distributable product.
**Architecture (ratified, D26–D31):** embed **code-server** as the editor core (no VS Code fork);
expose it via a **second dial-out WS tunnel multiplexed with yamux** (preserves D2 — zero inbound on
leaves); ship it via **hub-as-distribution** as an on-demand **`editor`** session kind reusing the D18
lifecycle + D19 placement; AI stays in OUR chrome first, then a Lattice VS Code extension; editor on
all four machines (Windows via **code-server in WSL2**); the embedded VS Code replaces the read-only
Monaco rail. Spec/plan: `~/.claude/plans/rippling-wishing-candy.md`.

## P1 — Embed a real editor (local agent, mini-ops)
code-server `editor` session kind + lifecycle; hub serves the binary; the yamux tunnel + `/editor`
reverse proxy; rendered embedded in the shell scoped to a project; real edit/save/search/git. Retire
the Monaco rail. **Spike the `/editor/{id}/` subpath concern FIRST.**
- **DONE WHEN:** open a project on mini-ops → code-server loads in the Lattice shell → edit + save
  (verify on disk via SSH) → search + git work → the agent shows **no new inbound listener** (netstat)
  → restart the hub → the editor session is re-adopted.

## P2 — Mesh editor
Open code-server on ANY placed agent: studio + mbp natively, **pc via WSL2** (`/mnt/c`); placement
visible/overridable; sessions persist across browser refresh + hub restart (extend D18 to editor).
- **DONE WHEN:** editor verified on studio, mbp, and pc(WSL2); machine chip shows placement + override;
  survives refresh + hub restart on each.

## P3 — AI-native
Weave the Claude runner beside the editor (chat aware of the open file/project); then the **Lattice VS
Code extension**: Cmd-K inline edits, tab autocomplete, in-workbench chat → the Claude runner on the
Max subscription.
- **DONE WHEN:** Cursor-grade in-editor AI works over the mesh (a Cmd-K edit applies; a completion
  appears) on the subscription.

## P4 — Package
Tauri desktop app (D15) bundling the agent sidecar → the installable IDE; finalize a product name (D9);
optional public distribution channel (GitHub Releases / Homebrew / winget).

---

## Explicitly deferred / non-goals (for now)
- iOS agent (no good sideload-free path) — phone story is Android/Termux only, last.
- Multi-user / teams / sharing someone else's mesh — single-owner first.
- Auth beyond tailnet + enrollment token — don't gold-plate before Phase 4.
- Fork VS Code — only if a must-have interaction provably can't be done via the extension APIs (D26/D29).

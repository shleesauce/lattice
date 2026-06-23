# Lattice — Roadmap

Build order is chosen so each phase produces something **testable in a browser** and usable
on a real fleet. **Phases 1–4 and the M3 "distributable" milestone are shipped — Lattice is
publicly released (v0.1.0 → v0.1.9) as an installable, auth-gated, self-updating self-hosted
product.** The active milestone is **v0.2.0** (a deliberate multi-session pass: the IDE M2-P2
mesh editor, agentID/storm hardening, an audit-driven quality pass — see `docs/V0.2.0-MILESTONE.md`).
The IDE milestone (M2) is the headline feature direction within it.

---

## Shipped

### Phase 1 — Heartbeat + registry + remote exec
- One `lattice` binary with `hub` and `agent` subcommands.
- Agent dials the hub over WebSocket, registers, and heartbeats host metrics (hostname, OS,
  arch, uptime, disk, online).
- Hub accepts agent connections, persists the registry in SQLite, and exposes a REST
  `/api/fleet` plus a WebSocket for live updates.
- Hub dispatches one-shot commands to a chosen agent and streams stdout/stderr back.
- Dashboard: live fleet grid + a command panel with streaming output.
- Agent cross-compiled for macOS (arm64/amd64), Windows (amd64), and Linux (amd64/arm64).

### Phase 2 — Interactive terminal + file browser + Wake-on-LAN
- Per-agent PTY ↔ xterm.js interactive terminal in the dashboard.
- File-tree browse + download per agent.
- `wake` action: an agent on a LAN sends a magic packet to a sleeping peer.

### Phase 3 — Workspace (the core product)
A Claude-Code / VS-Code-style mesh workspace (see `DECISIONS.md` D15–D21): a Projects→Sessions
sidebar over a synced projects directory; per session a **Terminal** tab and a live **Claude**
tab (an interactive local `claude` in a PTY, using your own Claude subscription); cross-machine
sessions with smart-but-overridable hub placement; sessions that survive a browser refresh and a
hub restart with replayed scrollback.

### Phase 4 — Packaging + onboarding
Self-hosted distribution: the hub serves the binaries + rendered installers + `/api/enroll`; the
installers register a persistent OS service (launchd / systemd / Windows Scheduled Task); a
dashboard "Add machine" onboarding flow with per-machine revocable tokens. Public release
channels (GitHub Releases; Homebrew / winget later) build on this.

### M3 — Distributable product (shipped, v0.1.0 → v0.1.9)
Cold-install a hub (`curl … | sh`), first-run wizard, single admin password + Tailscale boundary,
Manage-Mesh area with revocable per-machine tokens, SHA256-verified self-update, and a **one-click
fleet update cascade** (production-proven: ack-before-restart, Windows self-exit, tri-state outcomes).
Plus the v0.1.x feature drops: per-session permission mode + model picker, fire-and-forget phone
notify/approve, Claude Code hook-driven session state, workflow templates, framework (Vite/Next)
dev-server preview, and a fully-offline dashboard (no third-party requests).

---

## In progress — IDE milestone (M2)

Turn the workspace into a real IDE: deep editor abilities (edit/save, search, git, LSP, debugging,
extensions) plus an inline AI experience, all over the mesh, as a distributable product.

**Architecture (ratified, `DECISIONS.md` D26–D31):** embed **code-server** as the editor core (no
VS Code fork); expose it via a **second dial-out WebSocket tunnel multiplexed with yamux** (keeps
the zero-inbound-on-leaves property, D2); ship it via hub-as-distribution as an on-demand
**`editor`** session kind reusing the Phase-3 session lifecycle + placement; AI lives in Lattice's
own chrome first, then a Lattice VS Code extension; the editor runs on every machine (Windows via
code-server in WSL2).

- **P1 — Embed a real editor.** `editor` session kind + lifecycle; the hub serves the binary; the
  yamux tunnel + `/editor` reverse proxy; rendered in the shell scoped to a project; real
  edit/save/search/git, with no new inbound listener on the agent. *(shipped)*
- **P2 — Mesh editor.** Open the editor on any placed agent (macOS/Linux natively, Windows via
  WSL2); placement visible/overridable; sessions persist across refresh + hub restart.
- **P3 — AI-native.** Weave the Claude runner beside the editor (aware of the open file/project),
  then a Lattice VS Code extension: inline edits, completions, and in-workbench chat.
- **P4 — Package.** A desktop app bundling the agent sidecar → an installable IDE; optional public
  distribution channels.

---

## Explicitly deferred / non-goals (for now)
- iOS agent (no good sideload-free path) — the phone story is Android/Termux only, and last.
- Multi-user / teams / sharing someone else's mesh — single-owner first.
- Auth beyond tailnet + admin password + enrollment token.
- Forking VS Code — only if a must-have interaction provably can't be done via the extension APIs.

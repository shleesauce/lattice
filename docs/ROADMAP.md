# Lattice — Roadmap

Build order is chosen so each phase produces something **testable in a browser** and
dogfoodable on Dylan's fleet. The success criterion (packageable) is Phase 4 — but every
earlier phase is structured so it doesn't have to be redone to get there.

## Phase 0 — Scaffold ✅ (done in the originating session)
Repo, docs, fleet reference, kickoff prompt. No code yet.

## Phase 1 — Heartbeat + Registry + Remote Exec  ← START HERE
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

## Phase 2 — Interactive terminal + file browser + Wake-on-LAN
- Per-agent PTY ↔ xterm.js full interactive terminal in the dashboard.
- Scoped file-tree browse + download per agent.
- `wake` action: agent on a LAN sends a magic packet to a sleeping peer (PC is the live
  case — Ethernet, NIC already wake-armed; see FLEET.md).
- **DONE WHEN:** real shell session to any machine in the browser; browse a remote tree;
  wake the PC from sleep via the dashboard.

## Phase 3 — Workspace
- Proxy code-server through the hub; open any machine's filesystem in a browser VS Code.
- **DONE WHEN:** edit a file on studio from a browser tab on mini-ops, with file explorer +
  integrated terminal + doc preview.

## Phase 4 — Packaging + onboarding  ← THE SUCCESS CRITERION
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

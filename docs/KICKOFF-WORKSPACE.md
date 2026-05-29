# Kickoff — Lattice Workspace (next session)

Paste the block below to start the next session. This session is a **design discussion first**,
not a build. Talk through the UX with Dylan, lock the decisions, THEN plan.

---

You are continuing **Lattice**, a packageable cross-platform mesh command center at
`~/AI-Hub/projects/lattice/`. Phases 1, 2, and 4 are **done, running, and verified on the real
fleet** — a Go hub (PM2 on mini-ops, port 7400) + agents on 4 machines (mini-ops/studio/mbp =
macOS·arm64, pc = Windows·amd64), a dark React/Tailwind dashboard served by the hub with live
fleet, one-shot commands, an xterm.js PTY terminal, a file browser, Wake-on-LAN, and hub-served
installers + token enrollment (the packaging success criterion). Open the live dashboard at
http://mini-ops.tail3c8bee.ts.net:7400.

**Read first, in order:** `docs/STATE.md`, `docs/VISION-WORKSPACE.md` (the new north star),
`docs/ARCHITECTURE.md`, `docs/DECISIONS.md` (D1–D14 — don't re-litigate without reason),
`docs/FLEET.md`, and global memories `lattice-phase1`, `mesh-command-center-product`,
`reference_ssh_command_center`, `file-sync-mesh`.

**This session is about the WORKSPACE UX — and it is a DISCUSSION before a build.** The vision
(see docs/VISION-WORKSPACE.md): Lattice should feel like the **Claude Code / VS Code desktop
app**. Left sidebar = **Projects → Sessions** (projects are the synced `~/AI-Hub/projects/*`).
Two tabs: a **Terminal tab** (raw terminal; new session = new tab) and a **Claude tab** (a Claude
Code session that's already live — auto-launched, no typing `claude --dangerously-skip-permissions`
— behaving exactly like the Claude Code desktop app: project, context, usage, chat over the
terminal it drives). Sessions can run on **any machine** (AI-Hub is synced everywhere); the **hub
auto-picks the best machine** (speed/power/confidence/consistency) but the user can always **see
and manually override** the machine. We can reuse open-source (VS Code / code-server / Monaco /
Claude Code SDK) and this is likely where Lattice **becomes a distributable desktop app**.

**Do this, in order:**
1. Read the docs/memories above and the existing dashboard code (`dashboard/src/`) so you know
   what's already built (PTY/xterm, file browser, fleet WS, enrollment).
2. **Enter plan mode and DISCUSS** the 7 open questions in `docs/VISION-WORKSPACE.md` with Dylan
   — app shell (desktop Electron/Tauri vs browser-first), editor (full VS Code vs lean custom),
   Claude-tab mechanics (PTY passthrough vs Claude Agent/Code SDK headless `stream-json`), the
   smart machine-placement scoring model, session portability vs placement, the project/session
   persistence model, and the trust model for auto-skipping permissions. Use AskUserQuestion for
   the real forks. Recommend a default for each, with the tradeoff, but let Dylan decide.
3. Only after the UX/architecture is agreed: write it up (a VISION→spec doc + reframed Phase 3
   plan in ROADMAP.md), record decisions in DECISIONS.md (D15+), update STATE.md, then build —
   parallel subagents per track, adversarially verified against the real fleet, exactly like
   Phases 1/2/4 (use the Workflow tool; dogfood on mini-ops/studio/mbp/pc).

Operational notes: rebuild with `scripts/build.sh` (embeds dashboard + installer templates), then
`pm2 restart lattice-hub`. Enrollment token in `.lattice-token`. mbp + pc are now managed by their
installed OS service (launchd / Scheduled Task); mini-ops hub + local agent run under PM2. Full
SSH mesh + passwordless sudo on the Macs, admin on pc — deploy/verify autonomously; don't touch
Kinzie's machines. Keep docs/STATE.md current.

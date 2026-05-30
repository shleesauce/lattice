# Lattice — Workspace Vision (the product Lattice is becoming)

Captured 2026-05-29 from Dylan. This is the north star for the next major arc. It **expands
Phase 3** from "proxy code-server" into the real product: a Claude-Code / VS-Code-style
workspace over the mesh. Distributable as a product Dylan can share.

> This is a VISION capture, not a settled spec. The next session's first job is to **talk
> through and pin down the functionality** (see docs/KICKOFF-WORKSPACE.md), then plan it.
> Don't start building until the open questions below are decided with Dylan.

## The feel
It should feel like the **Claude Code desktop app / VS Code desktop app** — a real workspace,
not a dashboard. Something Dylan would install, live in daily, and hand to others.

## Core layout
- **Left sidebar = Projects → Sessions.** Projects are the synced `~/AI-Hub/projects/*` (every
  machine has them via Syncthing). Each project expands (dropdown) to its **sessions**.
- **Two tabs** (per session/project context):
  1. **Terminal tab** — a raw, open terminal. Opening a new session = a new tab, exactly like
     tabs in a terminal window. (We already have the PTY + xterm.js plumbing from Phase 2.)
  2. **Claude tab** — a **Claude Code session, already live**. When you submit into this tab for
     a project, Claude Code is auto-launched for you (no typing `claude --dangerously-skip-permissions`
     or any setup). It behaves **exactly like the Claude Code desktop app**: see the project,
     context, token usage, etc. — a chat interface layered over the terminal it's driving.

## Cross-machine, auto-placed, always visible
- Sessions can run on **any machine** in the mesh — because all the AI-Hub files are synced
  everywhere ([[file-sync-mesh]]), the work isn't tied to one box.
- The **hub is smart enough to pick the best machine** for a given task — by speed, power,
  confidence, consistency, etc. The user should **not have to** think about which machine.
- But the user must **always be able to SEE** which machine a session is on, and **manually
  override / pin** it to a specific machine.

## Implementation latitude (Dylan's steer)
- Reuse open-source where it helps (VS Code / code-server / OpenVSCode Server, Monaco, etc.).
- This is likely where Lattice **becomes a downloadable desktop app** (so it can be shared and
  distributed as a product), rather than only a hub-served browser page. Decide the shell.

## ✅ RESOLVED 2026-05-29 — see DECISIONS.md D15–D21
The 7 questions below were worked through with Dylan in a decision-first discussion and are now
locked: D15 (browser-first SPA → Tauri shell w/ agent sidecar) · D16 (lean editor: tree+Monaco+two
tabs, code-server dropped) · D17 (Claude tab = local `claude` headless stream-json on the
subscription, NOT the pay-per-token Managed Agents API) · D18 (first-class persisted Session;
processes outlive the browser) · D19 (placement: capability filter + headroom + locality) · D20
(placed + resumable, not live-migrated) · D21 (skip-perms default + audit + approval kill switch).
Build plan: ROADMAP.md Phase 3 + `~/.claude/plans/stateful-watching-ripple.md`. The questions are
kept below for the rationale trail.

## Open design questions to resolve WITH Dylan (next session)
1. **App shell:** desktop app (Electron / Tauri) as the client you sit at, with agents staying
   headless per machine? Or keep browser-first (hub-served + PWA)? Distribution-as-a-product
   pushes toward a desktop client. Decide the shell and how it relates to the existing
   hub-served dashboard.
2. **Editor:** embed full VS Code (code-server / OpenVSCode) for the file-tree + editor, or build
   a lean custom workspace (Monaco + our Terminal/Claude tabs)? The vision emphasizes the two
   tabs + project/session sidebar, which is narrower than full VS Code.
3. **Claude tab mechanics:** drive a PTY running `claude` and render its TUI as chat (passthrough),
   OR integrate the **Claude Code / Agent SDK headless mode** (`stream-json`) to render a true
   native chat with structured messages, tool calls, usage? The latter is almost certainly the
   right path for "exactly like the desktop app." Confirm.
4. **Smart machine placement (the scheduler):** what inputs feed "best machine"? Live metrics
   (CPU/RAM/load — we already heartbeat these) + declared capability (GPU/RAM tier) + soft
   signals (confidence/consistency from per-machine success history?). Define the scoring model,
   the per-machine capability profile, and the UI to show + override the choice.
5. **Session portability vs placement:** a running PTY/Claude session lives on ONE machine, but
   files + `~/.claude` transcripts are synced. Do sessions **migrate**, or are they **placed once
   and resumable elsewhere** from synced transcripts? Define "a session lives across machines."
6. **Project/session model + persistence:** how sessions are enumerated, named, persisted, and
   reattached across hub restarts and across machines (terminal tabs vs Claude Code sessions).
7. **Auth/permissions for `--dangerously-skip-permissions`:** auto-launching Claude with skipped
   permissions across the mesh is powerful and risky — confirm the trust model (tailnet + token
   is the current gate) and any guardrails.

## What already exists to build on (don't rebuild)
- Single Go binary hub+agent, agents dial out over WS, SQLite, dark React/Tailwind dashboard
  served by the hub (Phases 1/2/4 done & verified). PTY/xterm.js terminal, file browser, WoL,
  hub-served installers + token enrollment. See docs/STATE.md + docs/DECISIONS.md (D1–D14).

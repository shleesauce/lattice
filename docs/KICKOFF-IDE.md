# Lattice — IDE Milestone Kickoff & Handoff

Paste the block at the bottom to start the new session. This session is a **DISCUSS-FIRST**
architecture session (like the workspace kickoff): agree the IDE approach, record decisions, THEN
build. The strategic recommendation below is the answer to "what's the best approach" — it is a
recommendation to ratify or revise, not a fait accompli.

---

## The goal
Turn Lattice from a "workspace" into a real **IDE that competes with Cursor, VS Code, the Claude
Code desktop app, Codex desktop, and T3 Code** — a distributable product. Deep editor abilities:
real multi-file editing + save, search, git UI, LSP/intellisense, debugging, extensions, AND an
AI experience at least as good as Cursor/Claude Code — all over the Lattice mesh.

## RECOMMENDED ARCHITECTURE ("the best approach")
**Embed a VS Code server as the editor core; do NOT fork VS Code. Keep your AI + mesh as YOUR
chrome around it; ship it in the Tauri shell.** Concretely, three layers:

1. **Editor core = a VS Code *server* (code-server by Coder, or OpenVSCode Server by Gitpod), run
   per-agent and proxied through the hub.** This gives you the FULL VS Code workbench — editing,
   multi-tab, search, git, LSP/intellisense, debugging, integrated terminal, and extensions (via
   Open VSX) — for ~free, with **zero fork-maintenance**. You do NOT rebuild the editor.
2. **Differentiation = your AI + mesh, as your own chrome.** Embed the code-server surface inside
   the existing Lattice React shell and keep the **Claude stream-json chat, sessions, placement,
   project onboarding, device switching** as YOUR UI around the editor. Reuse everything Phase 3
   built; just swap the lean file-rail/Monaco viewer for a real VS Code editor surface. Later,
   deepen with a **Lattice VS Code extension** for in-editor AI (inline Cmd-K edits, tab
   autocomplete, chat woven into the workbench) using VS Code's now-mature **Chat / Language-Model
   / inline-completion extension APIs** — these let you build the Cursor experience WITHOUT forking.
3. **Shell = the Tauri app (D15).** Wrap the same frontend + bundle the agent sidecar; the editor
   surface is the proxied code-server. Browser-first still works for everything.

### Why not fork VS Code (Cursor/Windsurf's path)?
Cursor forks `microsoft/vscode` (MIT source; the *branded* build + MS marketplace are proprietary,
so forks use Open VSX). Forking buys deep editor-core control — but costs a **large team just to
rebase on upstream forever**. For a solo dev that's a trap. As of 2025/2026 you don't need it: the
VS Code **extension APIs** (Chat participants, Language Model API, inline completions, inline chat)
deliver the AI-IDE experience natively — proven by Continue.dev, Cline, etc. **Rule: start at
extension level; only fork if a specific must-have interaction provably can't be done via the API.**

### Alternatives considered (and why they lose)
| Approach | Verdict |
|---|---|
| **Embed code-server / OpenVSCode + your chrome + extension** (RECOMMENDED) | Full IDE, ~no fork maintenance, reuses all Phase-3 work, fits the agent/hub model. |
| **Fork VS Code (Cursor-style)** | Maximum control, but a perpetual upstream-rebase tax — unrealistic solo. Reserve for later if an API gap forces it. |
| **Keep extending the lean custom workspace (Monaco + tabs)** | You'd re-invent LSP, debugging, git UI, extensions — years of work to "compete with VS Code." Supersedes the wrong goal. |
| **Be a VS Code/Cursor extension only (Continue/Cline-style)** | Least work, but you're a *plugin*, not "the IDE" — abandons the mesh + distribution story. |

### Lattice's moat (lean into it)
Nobody else gives you: **a full VS Code that opens on ANY machine in your fleet, with an AI placed
on the best box, sessions that survive disconnects/restarts, one Syncthing-synced project set, and
self-hosted bring-your-own-Claude-Max — no per-seat AI subscription, no cloud.** That mesh +
self-host + subscription-Claude combination is the differentiator vs Cursor (cloud, per-seat AI,
single machine), Claude Code desktop (chat-over-terminal, single machine), and VS Code (no AI/mesh).

## Key decisions to settle next session (discuss-first)
1. **code-server vs OpenVSCode Server** — code-server (Coder) is more turnkey ("run a binary → VS
   Code on a port", auth, mature); OpenVSCode Server (Gitpod) is leaner/closer to upstream.
   Recommend code-server for v1.
2. **How to expose the per-agent editor** — agents dial OUT only (D2, no inbound listener). Either
   (a) **tunnel code-server's HTTP/WS over the existing agent WS** (multiplexed, e.g. yamux —
   preserves the zero-inbound-config promise; more plumbing) or (b) **bind code-server to the
   agent's tailnet interface and proxy directly** (simpler; the tailnet/WireGuard gates it; slight
   break of D2's purity). Recommend **tailnet-direct for the dogfood, WS-tunnel for the shipped
   product**. This is the biggest technical decision.
3. **AI placement: your chrome vs in-editor extension** — start with **AI in your React chrome,
   code-server embedded as the editor surface** (reuses everything, fastest). Add the Lattice VS
   Code extension for in-editor AI (Cmd-K, autocomplete) as a follow-on. Confirm the order.
4. **Supersede D16?** — D16 chose "lean custom editor, drop code-server." That was right for a
   *workspace*; the *IDE-competitor* goal flips it back to embedding VS Code. Record the reversal
   (new decision) with the rationale, scoped to the IDE milestone.
5. **Per-machine code-server lifecycle** — install/launch code-server on demand per project on the
   chosen agent (reuse the session/placement machinery + the hub-as-distribution installer model to
   ship the code-server binary), scoped to the project dir, torn down with the session.
6. **Editing/save + the file rail** — the embedded VS Code replaces the read-only Monaco rail; keep
   the Lattice file/project sidebar as navigation that drives the editor (open file → VS Code tab).

## Phased plan sketch (ratify/refine in-session)
- **P1 — Embed a real editor:** run code-server on the local agent (mini-ops), proxy through the
  hub, render it inside the Lattice shell scoped to a selected project; real edit + save + search +
  git. Replaces the read-only file rail.
- **P2 — Mesh editor:** open code-server on ANY chosen agent (placement), over the tunnel/tailnet;
  switch machines; sessions persist (extend D18). Verify across mini-ops/studio/mbp/pc.
- **P3 — AI-native:** weave the Claude stream-json runner into the editor — chat with file context,
  then a Lattice VS Code extension for inline Cmd-K edits + completions. The "Cursor experience."
- **P4 — Package:** the Tauri desktop app (D15) bundling the agent + pointing at the hub; the
  installable IDE. Then a real name (D9) + a public distribution channel.

## What exists today — build on, don't rebuild
Phases 1/2/4 + the Phase-3 workspace are DONE & VERIFIED (see docs/STATE.md, docs/DECISIONS.md
D1–D25): Go hub+agent (single binary, agents dial out over WS, SQLite, tailnet), hub-served
installers + token enrollment, and the **workspace**: Projects→Sessions + Devices sidebar, long-
lived sessions (terminal PTY + **Claude stream-json on the Max subscription**) that survive browser
detach AND hub restart, smart capability+headroom+locality placement, the new-project onboarding
wizard, and an IDE-style right-side file explorer. The Claude runner, session model, placement,
proxy patterns, and hub-as-distribution are the load-bearing pieces the IDE builds on.

Operational: build `bash scripts/build.sh` → `pm2 restart lattice-hub`; deploy agents via the hub
installer (`curl …/install.sh | sh -s -- --token …`) — agents MUST run as launchd LaunchAgents for
Claude auth (D22). Token in `.lattice-token`. Repo now on GitHub: **dylanstoryyy/lattice (private)**.
Full SSH mesh + sudo on the Macs. Don't touch Kinzie's machines. Keep docs/STATE.md current.

---

## PASTE THIS TO START THE NEW SESSION

You are continuing **Lattice**, a packageable cross-platform mesh command center at
`~/AI-Hub/projects/lattice/` (Go hub on PM2/mini-ops:7400 + agents on mini-ops/studio/mbp =
macOS·arm64, pc = Windows·amd64; dark React/Tailwind workspace served by the hub; repo on GitHub at
**dylanstoryyy/lattice**, private). Phases 1/2/4 and the **Phase-3 workspace** are done, running,
and verified on the real fleet. Open the dashboard at http://mini-ops.tail3c8bee.ts.net:7400.

**Read first, in order:** `docs/STATE.md`, `docs/KICKOFF-IDE.md` (this file — the IDE milestone +
the recommended architecture), `docs/DECISIONS.md` (D1–D25), `docs/ARCHITECTURE.md`, `docs/FLEET.md`,
and global memory `lattice-phase1`.

**This milestone:** turn Lattice into a real **IDE that competes with Cursor, VS Code, the Claude
Code desktop app, Codex desktop, and T3 Code** — deep editor abilities (edit/save, search, git,
LSP/intellisense, debugging, extensions) + a Cursor-grade AI experience, all over the mesh, as a
distributable product. **It is a DESIGN DISCUSSION before any build.**

**The recommended approach (in docs/KICKOFF-IDE.md — discuss + ratify or revise):** EMBED a VS Code
*server* (code-server / OpenVSCode Server) as the editor core, run per-agent and proxied through the
hub — **do NOT fork VS Code** (the upstream-rebase tax is unrealistic solo; VS Code's Chat /
Language-Model / inline-completion extension APIs deliver the AI-IDE experience without forking).
Keep the **Claude stream-json chat + sessions + placement + onboarding** as YOUR chrome around the
embedded editor; ship it in the **Tauri** shell. Lattice's moat is the mesh + self-host +
bring-your-own-Claude-Max — a full VS Code on ANY fleet machine, AI placed on the best box, sessions
that persist, one synced project set, no per-seat AI sub, no cloud.

**Do this, in order:** (1) Read the docs/memory above + skim the existing `dashboard/src` and
`internal/` so you know what's built (Claude runner, sessions, placement, proxy patterns, file
endpoints). (2) **Enter plan mode and DISCUSS** the 6 "Key decisions to settle" in docs/KICKOFF-IDE.md
with Dylan via AskUserQuestion — code-server vs OpenVSCode; how to expose the per-agent editor (WS
tunnel vs tailnet-direct); AI-in-your-chrome vs in-editor-extension + ordering; whether/how to
supersede D16; per-machine code-server lifecycle; editing/save + the file rail. Recommend a default
for each with the tradeoff; let Dylan decide. (3) Only after the architecture is agreed: write the
spec + a reframed roadmap (IDE phases P1–P4), record decisions (D26+), update STATE.md, then build
with parallel subagents adversarially verified against the real fleet (mini-ops/studio/mbp/pc),
exactly like the prior phases.

Operational: rebuild `bash scripts/build.sh` then `pm2 restart lattice-hub`; agents run as launchd
LaunchAgents (Claude auth needs the GUI-session Keychain — D22) / Windows Scheduled Task; token in
`.lattice-token`; push to `origin/master`. Full SSH mesh + sudo on the Macs; don't touch Kinzie's
machines. Keep docs/STATE.md current.

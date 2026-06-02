# Lattice — Decision Log

Append-only. Each entry: decision, why, alternatives rejected. Revisit only with reason.

## D1 — Single Go binary, two roles (`hub` / `agent`)
**Why:** one artifact per (OS,arch) → trivial distribution → packageable. Go = static
binaries, no runtime deps, first-class cross-compilation.
**Rejected:** Node/Electron agent (heavy, runtime deps, per-OS pain); Rust (slower iteration
for this team); separate hub+agent codebases (2× release surface).

## D2 — Agents dial OUT to the hub (no inbound listener on leaves)
**Why:** zero inbound firewall/port-forward config on every device → works behind NAT/CGNAT/
mobile → packageable. Hub holds persistent WebSockets; commands flow over the existing socket.
**Rejected:** hub SSHes into agents (re-introduces every per-OS SSH quirk we're trying to kill,
needs inbound config on each leaf).

## D3 — Tailnet for transport + network auth; enrollment token for app identity
**Why:** WireGuard gives encryption + "only my devices can even reach the hub" for free. Token
enrollment binds an agent to a device. No passwords/API keys in the happy path.
**Rejected:** rolling our own TLS/PKI + auth (slow, error-prone, not the value-add).

## D4 — Embedded SQLite for hub state
**Why:** zero-config, single-file, ships inside the binary's footprint → packageable.
**Rejected:** Postgres/Redis (external services to install — anti-packageable).

## D5 — Phase 1 piggybacks on system Tailscale; tsnet embed deferred
**Why:** don't block the proof-of-concept on embedding the tailnet. Dylan's fleet already runs
Tailscale. Migrate to `tsnet` for the shipped product so users don't install Tailscale separately.
**Rejected:** building tsnet embedding first (premature; slows the proof).

## D6 — Build all binaries by cross-compiling from mini-ops
**Why:** no Go toolchain on leaves (esp. Windows/Termux). Go cross-compiles trivially. Mirrors
how the real product will ship prebuilt binaries.
**Rejected:** installing Go per machine (toolchain drift, Windows pain).

## D7 — Dashboard = React + TS + Vite + Tailwind, served by hub
**Why:** Dylan's house stack; dark-first; static bundle embeds cleanly into the hub; browser-
first means the phone and every OS get the UI with zero install.
**Rejected:** native UI per OS first (huge surface before proving value); TUI (not the vision).

## D8 — code-server for the workspace (phase 3), not Theia (yet)  — SUPERSEDED by D16
**Why:** faster to embed/proxy; mature; delivers the file-explorer + editor + terminal + doc
view Dylan wants. Theia is the heavier "build a branded IDE" path — revisit if/when branding matters.
**SUPERSEDED (2026-05-29):** the Phase-3 vision narrowed to a Claude-Code/VS-Code *feel* centered on
Terminal + Claude tabs over a Projects→Sessions sidebar — narrower than a full IDE. code-server is a
heavyweight per-agent server to install + proxy (anti-packageable) and duplicates Claude Code. See D16.

## D10 — Pure-Go SQLite driver (`modernc.org/sqlite`), not `mattn/go-sqlite3`
**Why:** the packageability promise is "build every target from one machine." `mattn/go-sqlite3`
needs CGO → a C cross-toolchain per OS/arch (the exact Windows/Termux pain we're killing).
`modernc.org/sqlite` is pure Go → `CGO_ENABLED=0` cross-compiles darwin/windows/linux × arm64/amd64
from mini-ops with zero toolchain. Verified: 5 targets build clean. DSN sets WAL + busy_timeout.
**Rejected:** mattn/go-sqlite3 (CGO), bolt/badger (SQL ergonomics + history queries wanted).

## D11 — Phase-1 transport is plain `ws://` over the tailnet (not `wss://`)
**Why:** WireGuard already encrypts everything on the tailnet, so app-level TLS is redundant for
the dogfood and would add cert provisioning the agent can't self-serve yet. Agents dial
`ws://mini-ops.tail3c8bee.ts.net:7400/ws/agent`. Revisit when `tsnet` embedding lands (D5) /
for any non-tailnet exposure.
**Rejected:** self-signed TLS now (cert distribution burden, no security gain inside WireGuard).

## D12 — Cross-platform PTY via `github.com/aymanbagabas/go-pty`
**Why:** one terminal codepath for unix (creack/pty) AND Windows (ConPTY), still CGO-free →
keeps the single-binary cross-compile-from-one-host story intact. Verified building for
darwin/windows/linux with `CGO_ENABLED=0`.
**Rejected:** creack/pty alone (no Windows); shelling without a PTY (breaks interactive TUIs,
no colors/resize).

## D13 — WoL: agent broadcasts via an unconnected UDP socket with SO_BROADCAST
**Why:** the hub never reaches a sleeping leaf; a peer agent on the target's LAN sends the magic
packet. Agents report MACs in the heartbeat so the hub knows an offline machine's address →
turnkey "Wake" (no manual MAC entry). Connecting a UDP socket to 255.255.255.255 fails on macOS,
so we use an unconnected socket + WriteTo, hitting the limited broadcast AND each interface's
directed broadcast. Hub retains offline machines (persisted∪live) so a sleeping host stays
visible/wakeable. **Verified:** slept + woke the real PC from the dashboard.

## D14 — Hub-as-distribution: the hub serves binaries + rendered installers + enrollment
**Why:** for a self-hosted, no-cloud, single-owner mesh the most packageable path is NOT GitHub
Releases — it's the hub itself. Stand up `lattice hub`, then on each device run one command
pointing at YOUR hub (`curl http://<hub>/install.sh | sh -s -- --token …`). No GitHub account,
no third party. The hub renders installers from embedded templates with its own URL baked in
(from the request Host, so tailnet names work) and serves the cross-compiled binaries from
`--dist`. Installers install a persistent OS service (launchd / systemd --user / Scheduled Task).
**Verified** on real mbp + pc. **Rejected (for now):** GitHub Releases/Homebrew/winget as the
PRIMARY channel (adds a public dependency the private-mesh use case doesn't need) — keep as a
later public-distribution add-on. Token is passed at install time, never baked into the served
script (the tailnet is the outer gate; the token binds the device).

## D9 — Provisional name "Lattice"
**Why:** mesh imagery, reasonably clear of big collisions (`helm`=k8s, `fleet`=fleetdm).
**Status:** placeholder; finalize before any public/GitHub release.

---

# Phase 3 — Workspace (decided 2026-05-29 in a decision-first discussion with Dylan)

The reframed Phase 3 (docs/VISION-WORKSPACE.md): a workspace that feels like the Claude Code / VS
Code desktop app — Projects→Sessions sidebar, a Terminal tab + an already-live Claude tab,
cross-machine sessions with smart-but-visible machine placement. Two findings reshaped it:
Claude Code is NOT installed uniformly on the fleet (mbp lacks it), and today's PTY sessions die
with the browser WebSocket (the core thing to fix).

## D15 — App shell: browser-first SPA now, Tauri desktop wrapper later
**Why:** the workspace is all web tech (sidebar, tabs, xterm, Monaco, chat) and the hub already
serves the SPA over the tailnet — build it browser-first so it works everywhere day one and never
blocks the mesh. Package later as a **Tauri** app (Rust core + system webview, ~10MB) wrapping the
SAME frontend and **bundling the Go agent as a Tauri sidecar**, so the machine you sit at is a
first-class, fully-privileged node (Dylan's steer: max local control = max mesh power). Holds the
single-small-artifact north star (D1).
**Rejected:** Electron (100MB+ Chromium runtime per install — anti-packageable; kept only as a
fallback if the system webview misbehaves); building the desktop shell first (spends effort on the
shell before the workspace function exists, and you can't iterate in a plain browser).

## D16 — Editor: lean custom (file tree + Monaco + the two tabs), not code-server  (supersedes D8)
**Why:** the vision centers on Terminal + Claude tabs over a Projects→Sessions sidebar — narrower
than a full IDE. Reuse the existing Phase-2 file-browser plumbing for the tree; add **Monaco** (one
npm dep, already in the React bundle) for quick file view/edit. Nothing heavyweight to install per
agent.
**Rejected:** full VS Code via code-server/OpenVSCode (a heavyweight server to install + proxy on
every agent — anti-packageable — and it duplicates what Claude Code already does); no-editor-at-all
(Monaco is cheap and worth having for quick edits).

## D17 — Claude tab = the LOCAL `claude` binary headless in stream-json (subscription auth)  [SUPERSEDED by D35]
> **SUPERSEDED 2026-06-01 by D35.** Headless `-p`/stream-json moves to a separate, capped Agent SDK
> credit pool on June 15 2026 — defeating this decision's whole cost premise. The Claude tab is now an
> **interactive `claude` in a PTY** (which stays on normal subscription limits). The text below is kept
> for history; the headless argv it describes is forbidden — see D35.

**Why:** "exactly like the Claude Code desktop app" means driving Dylan's real Claude Max
subscription against local synced files. Verified the local binary does it all:
`claude -p --input-format stream-json --output-format stream-json --include-partial-messages
--replay-user-messages --permission-mode bypassPermissions --session-id <uuid> [--resume <id>]`
is a true long-lived BIDIRECTIONAL chat (realtime input + structured output: assistant text,
tool_use/tool_result, usage). The Go agent spawns it with `cwd=projectPath` and frames stdin/stdout
over the existing agent WS. `--session-id` lets the HUB assign the id, so the Lattice sessionId IS
the Claude sessionId (no init-event scraping); transcripts land in the synced `~/.claude/projects/`.
**Rejected:** the **Managed Agents API** (`@anthropic-ai/sdk`, Anthropic-hosted) — it's
**pay-per-token** (violates the subscription-only cost rule) and runs in a remote sandbox with no
local files. A research agent recommended it; rejected on cost + premise. The TS "Agent SDK" merely
wraps the same local binary, so spawning the binary directly avoids a Node dependency too. Also
rejected: PTY passthrough of the `claude` TUI (an ANSI box, no structured usage/tool/permission UI).

## D18 — Persistence: a first-class Session entity; processes outlive the browser
**Why:** "a session that's already live" is impossible while the PTY is born/killed with the browser
WS (today: `internal/hub/terminal.go`, and the agent's `defer terms.closeAll()` on socket drop). So:
a persisted hub-SQLite `sessions` row {id, projectPath, kind, agentId, claudeSessionId, title,
status, pinned, …}; the AGENT owns long-lived PTY/Claude processes keyed by sessionId + a scrollback
ring buffer; the browser attaches/detaches without killing them; on hub restart / agent reconnect
the agent reports its live sessions and the hub re-adopts them. This is the core engineering work.
**Rejected:** ephemeral sessions (current model — a refresh loses your session); metadata-only +
rebuild-from-transcript (loses live terminal scrollback and true background-running terminals).

## D19 — Smart placement: capability filter + headroom score + locality boost
**Why:** the hub should pick the best machine without the user thinking about it, but visibly and
overridably. Hard-filter to online agents that CAN run the kind (a Claude session requires `claude`
present — mbp currently fails this), then rank by free RAM + inverse load + cores, with a boost for
the machine the user is on; a manual pin always wins; the score breakdown is shown in the UI.
Agents report capabilities (claude/node + versions) in register + heartbeat.
**Rejected:** pure headroom with no locality (may push a session to a remote box when local is
snappier + more directly controllable); manual-first (loses the "it just works" feel). Success-
history "confidence/consistency" scoring deferred to v2 — no data yet.

## D20 — Session portability = placed + resumable, NOT live-migrated
**Why:** a running PTY/process can't be teleported across machines (let alone across OSes); live
process migration is a research rabbit hole. But because files + `~/.claude` transcripts are
Syncthing-synced fleet-wide, a session's IDENTITY (projectPath + claudeSessionId + transcript) is
portable: a session is pinned to one agent while running, and if that agent drops the SAME logical
Claude conversation resumes on another capable agent via `--resume`.
**Rejected:** live migration (infeasible cross-OS, would sink the timeline); pinned-only with no
resume (throws away the synced-mesh payoff).

## D21 — Trust: skip-permissions default + audit log + per-machine approval kill switch  [SUPERSEDED by D35]
> **SUPERSEDED 2026-06-01 by D35.** The structured audit log and the in-UI approve/deny prompts both
> fed on stream-json tool events, which no longer exist once the Claude tab is interactive (D35). The
> interactive `claude` runs with `--permission-mode bypassPermissions` and surfaces its OWN y/n prompts
> in the terminal. The `audit_log` table is retained (unused) for a future revival. Kept for history.

**Why:** auto-launching `claude --dangerously-skip-permissions` across the mesh adds NO new risk
floor — the fleet already has a full SSH mesh + passwordless sudo, so "tailnet access = full fleet
control" is the existing bar (and Dylan already aliases claude to bypass on studio). Keep the gate =
tailnet (WireGuard) + enrollment token. Guardrails: log every Claude session + its tool calls in
hub SQLite (`audit_log`); a global + per-machine toggle forces approval mode
(`--permission-mode default`), surfacing permission prompts to the UI.
**Rejected:** approval-mode-by-default (contradicts the "already live, no setup" feel and Dylan's
real posture); skip-perms with no audit (nothing to review after a destructive action).

## D22 — Claude sessions require the agent to run as a GUI-session launchd LaunchAgent (not nohup)
**Why (discovered during fleet verification 2026-05-29):** claude's subscription OAuth credentials
live in the macOS **login Keychain** ("Claude Code-credentials"). A process can only read them when
it runs inside the user's GUI/Aqua security session. The Phase-1 dogfood deploy (`deploy-fleet.sh`)
starts agents via **nohup from an SSH session that then closes** → the orphaned daemon loses Keychain
access → spawned `claude` returns **"Not logged in · Please run /login"**. The Phase-4 installer's
**launchd LaunchAgent** (`sh.lattice.agent`, `launchctl bootstrap gui/<uid>`) runs in the GUI session
and HAS Keychain access. **Verified:** the same Claude session that returned "Not logged in" under the
nohup agent returned a real **"PONG"** (with real token usage: in 13700 / out 5 / cache 30028) once
studio was reprovisioned as a launchd LaunchAgent. **Consequence:** the Phase-4 service install is a
PREREQUISITE for the Claude tab on macOS, not just a persistence nicety — agents must be launchd-managed
(not nohup) for Claude sessions to authenticate. Two related launcher fixes shipped the same session:
(a) `claude --print --output-format=stream-json` **requires `--verbose`** (omitting it exits instantly);
(b) the launcher **scrubs `ANTHROPIC_API_KEY` + `CLAUDECODE`/`CLAUDE_CODE_*`** from the child env so a
claude spawned by an agent that itself runs inside a Claude session uses the subscription, not a
poisoned/empty API key (also enforces the subscription-only cost rule). **Open:** Windows/Linux
credential-store equivalents to verify; pc (Windows) Claude-session auth untested.

## D23 — projectPath is currently an ABSOLUTE path — known cross-machine portability gap
**Why noted:** sessions store `projectPath` as an absolute path, but home dirs differ across the fleet
(`/Users/mini-ops/…` vs `/Users/dylanstory/…`), so a session placed/resumed on a different machine can
`chdir`-fail even though the project is Syncthing-synced under `~/AI-Hub/projects/<name>` everywhere.
**Decision (to implement next):** model projects by **name relative to `~/AI-Hub/projects`** and have the
agent resolve `<home>/AI-Hub/projects/<name>` locally; keep the absolute path only as a display hint.
This makes D20 portability (placed + resumable on any machine) actually hold. Tracked as a follow-up;
the `/api/projects` endpoint already returns `{name, path}`, so the name is available.
**Partially addressed by D24:** the agent now resolves an empty/`~` cwd to its own home, so the home-path
divergence is handled for device sessions and the `~/...` form; named-project resolution is still TODO.

## D24 — "Device projects": sessions scoped to a specific machine (machine-local work)
**Why (Dylan, 2026-05-29):** besides the synced AI-Hub projects, Dylan wants to open a Claude/terminal
session bound to a SPECIFIC box to do machine-local work — set up programs, organize files, admin that
device. **Model:** a session has a `scope` of `project` (synced worktree, auto-placeable) or `device`
(pinned to one machine, cwd = that machine's **home**). A device session sets `scope:"device"` +
`pinAgentId:<device>`, omits projectPath; the **agent resolves empty/`~` cwd to its own home** (the hub
can't — home paths differ per box). Device sessions are **strict**: they run on their device or fail —
NEVER fall back to another machine (defeats the point). The capability filter still applies, so a Claude
device session on a box without claude returns a clear `"this device can't host the session: claude not
installed"`. UI: a **DEVICES** section under PROJECTS listing the fleet (online-first, CLAUDE capability
chip, offline dimmed), each device expanding to its device sessions + "+ new session"; the machine chip
is static (no override) for device sessions. **Verified on the fleet:** device terminal on mini-ops →
`pwd` = `/Users/mini-ops` (home); device Claude on mbp → 400 "claude not installed"; device Claude on
studio → real "PONG" in its home. Schema: added `sessions.scope` (idempotent `ALTER … ADD COLUMN`
migration, default `project`).

## D25 — "Begin new project" onboarding wizard (hybrid scaffold → register → seeded Claude session)
**Why (Dylan, 2026-05-29):** a guided way to spin up a brand-new project from the hub — name it,
pick connectors/MCPs/agents/related-projects/envs, and have it ported to the right machine, scaffolded,
Claude-configured, and ingested into the synced AI-Hub. **Design (chosen):** `POST /api/projects`
(GET still lists). The HUB scaffolds the standard skeleton DIRECTLY in `projectsRoot/<folder>` (it lives
on the hub host; Syncthing propagates everywhere): README, CLAUDE.md, docs/PROJECT_CONTEXT.md,
**docs/ONBOARDING.md** (the brief — every wizard answer + a "Setup tasks for Claude" checklist),
.env/.env.example, .gitignore, .claude/settings.json, + `git init` + initial commit. Connectors/MCPs/
agents/related are captured as **intent in the brief** (the hub doesn't assemble MCP configs — Claude
wires them). **Register** (best-effort, warnings not failures): append a row to the Project Registry
table — which lives in **`~/AI-Hub/UNIVERSAL_RULES.md`** (CLAUDE.md only references it; the create logic
tries UNIVERSAL_RULES.md first), run `build-project-index.sh` to regenerate PROJECT_INDEX.md, write a
`knowledge-base/wiki/projects/<folder>.md` stub (manual `_index`/`_map` linking left as a warning).
**Launch:** create a Claude session in the new project and **seed** it with a first turn pointing at
docs/ONBOARDING.md. The session is **placed on the hub's LOCAL agent** (detected via the agent's
**loopback WS RemoteAddr** → `Agent.Local`) so the just-written files are on disk at the exact path
(dodges Syncthing delay + the D23 home-path divergence); falls back to placement with a "files may need
to sync" warning. **Verified on the fleet:** scaffold (all files + git), register (UNIVERSAL_RULES row +
PROJECT_INDEX regenerated + KB stub), launch on mini-ops (local detection), and the seed turn reached
claude (the 401 was only this machine's local-agent contamination — proven to auth on studio). Test
artifacts fully reverted afterward.

---

# IDE Milestone (M2) — turn the workspace into a real IDE (discussed & decided 2026-05-29 with Dylan)

Goal: an IDE that competes with Cursor / VS Code / Claude Code desktop / Codex desktop / T3 Code —
deep editor abilities (edit/save, search, git, LSP/intellisense, debugging, extensions) + a
Cursor-grade AI experience, all over the mesh, as a distributable product. Strategy: **EMBED a VS Code
server as the editor core; do NOT fork VS Code.** Keep the Claude chat + sessions + placement +
onboarding as OUR chrome around the embedded editor. Spec/plan: `~/.claude/plans/rippling-wishing-candy.md`.
In-repo next milestone (`dylanstoryyy/lattice`, master); browser-first now, Tauri shell at P4 (D15).

## D26 — Embed code-server (Coder) as the editor core; do NOT fork VS Code  (supersedes D16)
**Why:** the full VS Code workbench (multi-file edit/save, search, git, LSP/intellisense, debugging,
integrated terminal, extensions via Open VSX) for ~free, with **zero fork-maintenance** — the
upstream-rebase tax of a fork (Cursor/Windsurf's path) is unrealistic solo. code-server is the most
turnkey server build ("run a binary → VS Code on a port", built-in auth, mature). The AI-IDE
experience comes from VS Code's mature **Chat / Language-Model / inline-completion extension APIs**
(D29), not a fork. D16 ("lean Monaco editor, drop code-server") was right for a *workspace*; the
*IDE-competitor* goal flips it back — this reversal is scoped to the IDE milestone.
**Rejected:** fork VS Code (perpetual rebase tax — reserve only if an API gap provably forces it);
keep extending the lean Monaco workspace (re-invents LSP/debug/git/extensions — years of work);
be a VS Code/Cursor extension only (you're a *plugin*, not "the IDE" — abandons the mesh + distribution).

## D27 — Expose the per-agent editor via a SECOND dial-out WS tunnel, multiplexed with yamux
**Why:** preserves **D2** fully (agents dial OUT only, **zero inbound listener on leaves**) — Dylan
chose the pure path over tailnet-direct. The agent opens a second outbound WS to the hub `/ws/tunnel`
(token + agentID auth, mirrors the `/ws/agent` handshake), wrapped as an `io.ReadWriteCloser` running
`yamux.Server`; the hub runs `yamux.Client` on it (tracked per-agent in the registry). The hub
reverse-proxies `/editor/{sessionId}/*` → `yamuxSession.OpenStream()` (via a `httputil.ReverseProxy`
whose `Transport.DialContext` returns the stream; Go ≥1.21 forwards `Connection: Upgrade` so WS
upgrades work) → agent `AcceptStream` reads a sessionId prefix → `net.Dial 127.0.0.1:<code-server
port>` → `io.Copy`. **Why yamux:** a VS Code workbench opens many concurrent HTTP/WS connections, so a
single un-muxed pipe won't do; one tunnel per agent, each editor session = a prefixed stream.
**Rejected:** tailnet-direct (binds an inbound listener on the leaf — breaks D2's purity; Dylan
declined the shortcut); a separate outbound WS per editor session (no multiplexing → can't carry the
workbench's parallel conns). **#1 build risk:** code-server must serve correctly under the
`/editor/{id}/` subpath (asset base-path) — spike + prove FIRST in P1.

## D28 — Distribute code-server via hub-as-distribution; an on-demand "editor" session kind  (extends D14)
**Why:** no per-machine manual install — fits the mesh + single-owner model. The hub serves the
code-server release (reuse `handleDownloadBinary`'s `path.Base` allowlist + `h.distDir`); the agent
fetches + **caches on first editor use**, then launches code-server per-session scoped to the project
dir as a new **`editor`** session kind that **reuses the D18 lifecycle** (process-global registry,
survives browser detach + hub restart, re-adopted on reconnect) and **D19 placement** (capability
filter now also requires `codeServerInstalled`). Torn down with the session.
**Rejected:** pre-bundle code-server in the agent (bloats every agent build per OS/arch); npm/script
install per machine (reintroduces a node/runtime + per-OS install step — the packageability pain
D1/D10 kill). Cost: ~100–200 MB binary per platform → fetch only on first editor use, cache locally.
**P1 REFINEMENT (2026-05-30, Dylan):** ship **per-node install / detect-local** FIRST — the agent
resolves an already-installed `code-server` (`resolveCodeServer`) and spawns+proxies it; placement
excludes machines without it. This unblocked P1 verification immediately (code-server was brew-installed
on mini-ops) and keeps the agent tiny. The hub-served-tarball path above is NOT abandoned — it's a clean
future add: the resolver is the single seam that would grow a "fetch from `/dl/code-server-…` + cache"
fallback when `resolveCodeServer()` returns empty. So D28's "no per-machine install" goal is deferred,
not dropped.

## D29 — AI experience: chrome-first, then a Lattice VS Code extension
**Why:** fastest path to a working IDE that reuses ALL of Phase 3. P1–P2 keep the Phase-3 Claude
stream-json chat (Max subscription, D17) in OUR React chrome beside the embedded editor — runner,
sessions, placement, token HUD all reused. P3 adds a **Lattice VS Code extension** for in-editor Cmd-K
edits + tab autocomplete + in-workbench chat (VS Code Chat / Language-Model / inline-completion APIs),
talking to the same Claude runner → the "Cursor experience" WITHOUT a fork.
**Rejected:** extension-first (wires the extension↔runner bridge before the editor is even embedded —
slower, re-treads less of Phase 3); both in parallel (widest surface at once, higher half-finished
risk). **Rule:** start at extension-API level; only fork if a must-have interaction provably can't be
done via the API.

## D30 — Editor on all four machines; Windows serves code-server inside WSL2
**Why (Dylan):** the moat is "a full VS Code on ANY fleet machine" — so pc (Windows) is in scope NOW,
not later. code-server ships no native Windows build, so keep ONE editor core everywhere by launching
code-server **inside WSL2** on the Windows agent, reaching the Syncthing'd project via `/mnt/c`. The
capability probe reports `codeServerInstalled`/version + `wslAvailable`.
**Rejected:** OpenVSCode Server / `code serve-web` on Windows only (two server flavors to manage);
Macs-first/Windows-later (Dylan wants the full "any machine" proof in this milestone). Cost: a WSL2
dependency on pc (agent bootstrap may need to install it) + cross-filesystem (`/mnt/c`) perf — spike
in P2.

## D31 — Retire the read-only Monaco file rail; code-server becomes the editor surface  (with D26)
**Why:** one editor, no split-brain. The embedded VS Code is THE editor; the Lattice Projects/Devices
sidebar stays as navigation that drives it (click a file → opens a VS Code tab). Remove the workspace
rail trio `ProjectFilesPanel.tsx` / `FileViewer.tsx` / `MonacoPanel.tsx` / `useFileBrowser.ts`. The
Phase-2 `/api/agents/{id}/files` endpoints + the FLEET-tab `FileBrowser` stay (still used there).
**Rejected:** keep Monaco as a quick-peek fallback (UX overlap + extra surface to maintain).

## D32 — A session is pinned to its creation device FOR LIFE; project sessions default to the Studio  (amends D20)
**Why (Dylan, 2026-05-31):** D20 made a project/Claude session re-placeable on resume — the scorer
could land it on whatever machine ranked best, so a session could silently jump hosts between runs.
That breaks the mental model Dylan wants: "once it runs on the Studio, it IS on the Studio." His fleet
has one true coding box (the Mac Studio) where ~90% of sessions belong, and a wandering host is a
footgun, not a feature. **Decision:** every session is `pinned=true` at creation (the `pinned` column
existed since D18 but was never written — now it is), and **resume always returns to the session's
original `agent_id`** — it never re-places. The scorer still runs on resume, but only to confirm that
host is currently eligible; the chosen host is forced to the original. If that box is offline/ineligible,
resume **fails** with `"can't resume on its device (…): <reason> — wake it in Fleet"` instead of
migrating the work. This loses no liveness: per D18 the process already survives sleep/disconnect
(orphaned → re-adopted on reconnect), and the browser still attaches/detaches from ANY device — only
the *host* is frozen, never the viewer. A new `primary_agent` setting holds the default coding machine
(set to `dylans-mac-studio.local-darwin`); a **project** session created with no explicit device pick
**soft-pins** there — honoured by `ScorePlacement` only when the Studio is actually eligible, otherwise
normal placement fallback — so the 90%-on-Studio case is one click while device sessions (already
strict per D24) are unchanged. Settings GET now returns `primaryAgent`; the whitelisted settable keys
grew `primary_agent`.
**Rejected:** keep D20's re-placement as default with pin-on-demand (closest to old code, but leaves
the wandering-host footgun as the default path — Dylan explicitly chose always-pin); a hard pin that
fails creation when the Studio is offline (too brittle for the default — a soft pin that falls back is
the right ergonomic). **Tradeoff accepted:** the synced-mesh "resume the same conversation on another
box" payoff from D20 is intentionally given up for predictability; the transcript is still synced, so a
*new* session could be started elsewhere against the same project if ever needed — it just won't happen
automatically.

## D33 — Fleet "always-on": OS-native persistence per leaf + a hub-host watchdog that recovers-then-alerts
**Why (Dylan, 2026-05-31):** "bring everything on, keep it running, keep things in check if they crash or
disconnect, reach out to them." Resilience is layered, cheapest-first:
1. **Agent reconnect loop** (already built, `internal/agent/agent.go`): exponential backoff redial so a hub
   blip or network drop self-heals without restarting the session processes.
2. **OS-native process persistence** = the real "always on". macOS leaves run a launchd LaunchAgent
   `sh.lattice.agent` with `RunAtLoad`+`KeepAlive` (start on login, auto-restart on crash). The Windows
   leaf (`pc`) was the weak link — its task was **logon-only / InteractiveToken**, so a headless reboot
   left it down (found it dead with a 24h-stale heartbeat while Tailscale still showed the box "online").
   Fixed: re-registered `LatticeAgent` with **both** a LogonTrigger AND a BootTrigger, `RunLevel Highest`,
   restart-on-failure 999×@1min — KeepAlive parity. Kept it as the **interactive user** (bound by the
   machine-local `COMPUTERNAME\user` SID, NOT SYSTEM) because the agent needs the user session context for
   claude auth (D22); SYSTEM would reintroduce the D22 "Not logged in" failure. The install template
   (`installtmpl/install.ps1.tmpl`) was updated to match so fresh installs are hardened.
3. **Hub-host watchdog** (`scripts/fleet-watchdog.sh`, pm2 `lattice-watchdog`): for the case the lower
   layers can't catch (agent process wedged but box up, or total unreachability). Every 60s it polls
   `/api/devices`, flags any agent-backed machine that's offline or whose `lastSeen` is >90s stale, and
   recovers it over SSH using that machine's OWN primitive — macOS `launchctl kickstart -k gui/<uid>/
   sh.lattice.agent`, Windows `schtasks /run /tn LatticeAgent`. After 3 consecutive failed cycles it
   pushes ONE alert to ntfy (reusing the homebase `homebase-mini-ops-…` topic the phone already
   subscribes to) and stays quiet until recovery, then sends a single "recovered" note.
**Why the watchdog is a hub-HOST script, not in the hub binary:** D2 keeps the hub free of any inbound/SSH
dependency on leaves. Recovery is an operator action, so it lives beside `deploy-fleet.sh` and reuses the
same SSH aliases; the hub binary stays pure. mini-ops (the hub host) is excluded from watchdog targets —
it's pm2-managed with a launchd startup item and can't watchdog itself.
**Verified:** all 4 agents online with fresh heartbeats; killed mbp's agent → launchd KeepAlive revived it
in ~2s (before the watchdog's sweep — proves the layering); ntfy test push delivered; `pm2 save` persists
hub+agent+watchdog across reboot. **Rejected:** SYSTEM-context Windows task (breaks D22 claude auth);
putting recovery in the hub (violates D2); alert-only with no auto-recovery and auto-recovery with no alert
(Dylan chose recover-THEN-alert — self-heal silently, escalate only real failures). **Deferred:** the
device view still shows a non-agent machine "online" purely from Tailscale reachability even when its agent
is dead — cosmetic; the watchdog keys off the agent's own `lastSeen`, not that flag, so recovery is correct
regardless. Phone/tablet leaves (S26, etc.) have no agent yet and are out of scope here.

## D34 — Fleet hygiene: exclude other-people's machines, truthful agent status, one-command fleet-sync
**Why (Dylan, 2026-06-01, from a network audit):** three issues surfaced. (a) Two of Kinzie's machines
("Kinz's MacBook Air", "M"/`pc-kinzie`) sit on Dylan's tailnet — fine, he occasionally runs local sessions
on them — but they cluttered the Lattice fleet view and he doesn't manage them here. (b) The device view
showed a box as a healthy "online" agent node even when its agent process was DEAD, as long as the host
still answered Tailscale (the `foldGroup` merge flipped `Online=true` from tailnet reachability, and both
hub `deviceStatus` and the dashboard adapter keyed "is this a runnable node" off `Online`+`hasAgent`). So a
dead agent rendered green — the dashboard lied about what you could run on. (c) The four agents had drifted
onto THREE different builds (studio 563dbc5, mbp 7582584, emu+mini-ops f37a25b) because there was no
mechanism to keep them in lockstep — only `deploy-fleet.sh` (nohup, no persistence) and ad-hoc installs.
**Decisions:**
- **Exclude list** (`internal/hub/devices.go`, `excludedDevices`/`isExcludedDevice`): machines whose
  identity tokens match kinzie/kinz/pckinzie fold OUT of `/api/devices`. They stay on the tailnet and SSH
  still works; they're just not part of OUR fleet. Token-based so it's robust to DNS suffixes/punctuation.
- **Truthful status** — new `Device.AgentLive` field, set from the agent's OWN check-in (`best.Online`)
  BEFORE the tailscale-reachability merge can flip `Online`. `deviceStatus` now returns "online" only when
  `HasAgent && AgentLive`; a reachable-but-dead-agent box is "reachable" (blue), never false-green. The
  dashboard adapter (`adapt.ts`) mirrors this: `agentLive = hasAgent && (d.agentLive ?? true)` gates the
  idle/detached (green/teal) states; sessions only attach to a live agent. The `?? true` keeps back-compat
  if an older hub omits the field. Net: green/teal ⇒ a live agent you can run on; blue ⇒ visible only.
- **fleet-sync** (`scripts/fleet-sync.sh`): the durable cure for skew. Rebuilds (dashboard embed + all
  targets), restarts the local hub, then re-runs the HUB INSTALLER on each agent over SSH — which downloads
  the fresh binary AND re-lays OS-native persistence (launchd / scheduled task), so a sync never downgrades
  durability the way `deploy-fleet.sh`'s nohup would. Verifies every agent is back with a fresh heartbeat.
**Verified:** fleet dropped 8→6 devices (Kinzie ×2 gone, S26 stays reachable); `agentLive` true on all 5
agents; ran fleet-sync → all 5 agents identical at fdd2dd7 (skew gone); hub tests pass; dashboard rebuilds.
**Rejected:** Tailscale ACLs to fence Kinzie's boxes (heavier, and Dylan wants tailnet access retained —
just not in this UI); a one-off manual binary push (doesn't prevent the next drift — fleet-sync is the
repeatable mechanism). **Note:** run `scripts/fleet-sync.sh` after any agent/proto change to keep the fleet
in lockstep; it's the companion to D33's always-on persistence.

## D35 — NO headless `claude -p`; the Claude tab is an interactive `claude` in a PTY  (supersedes D17, D21)
**Why (Dylan, 2026-06-01):** Starting **June 15 2026**, Anthropic moves Agent SDK + `claude -p` (headless)
usage on subscription plans into a **separate monthly Agent SDK credit pool** — roughly the plan fee, no
rollover, overage billed pay-per-token — *separate from interactive usage limits*
(code.claude.com/docs/en/headless). D17 chose headless `-p` precisely to ride the Max subscription and avoid
metered cost; this change defeats that premise. The billing line is drawn at `-p`/SDK: an **interactive
`claude`** (a human typing into a real TTY/PTY) stays on normal subscription interactive limits. Interactive
mode supports everything the tab needs — `--session-id`, `--resume`, `--model`, and the `/model` command
(code.claude.com/docs/en/cli-reference). Dylan's actual goal for the tab is "the Claude Code desktop
experience" = a Claude session with surrounding panels (files, changes, preview); those panels are built from
the agent's filesystem/git access and never needed structured chat JSON. The only thing `-p` bought was
bubble-rendering + a tool-parsed audit + an approve/deny UI — all explicitly traded away.
**Decision:**
- The `claude` session kind launches an **interactive `claude` in a PTY**, reusing the Phase-2 terminal
  engine (`ptySession`, D12). Argv: `claude --session-id <id> --permission-mode bypassPermissions [--resume
  <id>]`. **Forbidden in that argv: `-p`, `--print`, `--input-format`/`--output-format stream-json`,
  `--include-partial-messages`, `--replay-user-messages`.** A guardrail comment lives at the launch seam
  (`claudeCommand` in `internal/agent/terminal.go`).
- Claude sessions now speak the **same WS frames as terminal sessions** (`output`/`replay` bytes, `input`
  keystrokes, `resize`). Removed: the stream-json engine (`internal/agent/claude.go`), `claude_event` /
  `claude_input` / `claude_permission` (proto + hub + dashboard), the structured chat renderer
  (`SessionClaude` chat body, `claudeModel.ts`), and the tool-parsed audit path (`auditClaudeEvent`).
  **Preserved:** `claudeChildEnv()` (still scrubs `ANTHROPIC_API_KEY`/`CLAUDECODE`/`CLAUDE_CODE_*` to force
  subscription OAuth), the `ClaudeInstalled` placement gate (D19), `--resume` portability (D20/D32), and the
  `audit_log` table (left in place, unused, recoverable).
- The onboarding wizard (D25) seeds its interactive claude by sending the brief as **PTY keystrokes**, not a
  structured `claude_input` message.
- **HARD RULE:** do not reintroduce headless `-p`/stream-json anywhere in Lattice until Dylan **explicitly**
  authorizes it. Deferred to a later pass (NOT this one): model-selector dropdown, changes tab, preview tab.
**Rejected:** keeping `-p` and absorbing the new credit-pool cost (reintroduces the metered billing D17 set
out to avoid); a degraded raw-scrollback audit (low-fidelity plumbing for little value — table mothballed
instead); pay-per-token API-key auth for the tab (violates the subscription-only cost rule).

## Open / deferred for the IDE milestone
- **D9** product name — still provisional ("Lattice"); finalize before any public release (target P4).
- **Tauri packaging** (D15) — P4: wrap the SPA + bundle the agent sidecar; then a public distribution channel.
- Cross-machine editor resume parity with the Claude `--resume` story (D20) — editor is placed+restartable,
  not live-migrated; the project tree is synced so re-opening on another box is cheap.

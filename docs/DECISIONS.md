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

## D17 — Claude tab = the LOCAL `claude` binary headless in stream-json (subscription auth)
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

## D21 — Trust: skip-permissions default + audit log + per-machine approval kill switch
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

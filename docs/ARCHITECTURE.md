# Lattice — Architecture

## Design north star
Everything bends toward **packageable**: single artifact per platform, zero-config install,
no per-OS manual steps exposed to the user. If a design choice makes the build easier on
Dylan's fleet but harder for a stranger to install, it's the wrong choice.

## Components

### 1. The agent (Go, single static binary)
- One binary, two roles: `lattice agent` (leaf) and `lattice hub` (controller). Same
  executable — role chosen by flag/subcommand. This keeps distribution to ONE artifact
  per (OS, arch).
- Cross-compiles to darwin/windows/linux × amd64/arm64 with `GOOS`/`GOARCH`. **Build all
  targets from one machine** (mini-ops) — never require a per-machine Go toolchain. This
  is itself a packageability win and sidesteps Windows/Termux toolchain pain.
- Responsibilities (leaf): register with hub, heartbeat (hostname, OS, arch, uptime, disk,
  load, online), execute commands the hub sends, stream stdout/stderr back, host a PTY for
  interactive terminal, expose a scoped file browser, send wake-on-LAN magic packets to
  LAN peers.

### 2. The hub (same binary, role=hub)
- Runs on an always-on machine (Dylan's fleet: **mini-ops**, failover **studio**).
- Serves: REST API + WebSocket endpoint for agents + WebSocket/SSE for the dashboard +
  the static dashboard bundle + (phase 3) a reverse proxy to code-server.
- Stores fleet registry + history in **embedded SQLite** (zero external dependency).
- Default port **7400** on Dylan's fleet (free; avoid 3001/4000/5173/5678/5679/8222/8384/22000).

### 3. The dashboard (React + TS + Vite + Tailwind, dark-first)
- Matches Dylan's house stack. Built to static assets, embedded into / served by the hub.
- Talks to the hub via REST (fleet list, actions) + WebSocket (live status, terminal,
  command output streaming). Terminal UI = xterm.js.

### 4. Networking — THE key decision
**Agents dial OUT to the hub over the tailnet. The hub never initiates to agents.**
- Why: leaf machines need ZERO inbound firewall/port-forward config. Works behind NAT,
  CGNAT, mobile networks (the phone "just works"). Critical for packageability.
- Mechanism: each agent opens a persistent authenticated WebSocket to the hub. Commands
  flow hub→agent over that existing socket; results/streams flow back. No inbound listener
  on leaves.
- Transport security + identity: the tailnet (WireGuard) provides encryption + network-level
  auth — only enrolled devices on the user's tailnet can reach the hub at all.

### 5. Tailscale integration — phased
- **Phase 1 (move fast):** piggyback on the system Tailscale already installed on Dylan's
  fleet. Hub reachable at its `*.ts.net` MagicDNS name; agents connect there.
- **Product (later):** embed Tailscale via **`tsnet`** so the agent brings its own tailnet
  identity and the user doesn't separately install/login Tailscale. Note the migration as a
  deliberate step — don't block Phase 1 on it.

### 6. Auth / enrollment
- Network layer: tailnet membership (WireGuard) — the outer gate.
- App layer: hub mints a one-time **enrollment token**; `lattice join --hub <url> --token <code>`
  registers the agent. Device identity bound to its Tailscale node identity.
- No passwords, no API keys in the happy path.

### 7. Workspace (phase 3)
- Embed/proxy **code-server** (VS Code in the browser) behind the hub — simpler to ship than
  Theia for v1. Gives file explorer, editor, panels, integrated terminal, doc preview — the
  "Cursor-like workspace" Dylan wants — reachable from any device's browser over the tailnet.

## End-to-end flow (Phase 1 proof)
1. `lattice hub` starts on mini-ops, listens on tailnet :7400, opens SQLite, serves dashboard.
2. `lattice agent --hub mini-ops.<tailnet>.ts.net:7400 --token <code>` on studio/mbp/pc.
3. Agent opens WS to hub, registers, heartbeats every Ns.
4. Dashboard (browser → hub) shows 4 live machines with OS/disk/uptime/online.
5. User clicks a machine, types `uname -a` (or `ver` on Windows), hits run → hub relays over
   the agent's WS → agent executes → streams output back → dashboard renders it live.

## Cross-platform landmines (already learned the hard way — see global memory)
- Windows over SSH: `cmd` vs PowerShell; `&`-chaining silently no-ops; `findstr` mishandles
  `/`. **The Go agent exists precisely to delete this class of problem** — but the *installer*
  bootstrap still touches it, so mind it there.
- Build Windows/Linux/ARM binaries by cross-compiling from mini-ops, not on each box.
- Termux/phone is a weak target — schedule last; never block a phase on it.
- macOS service = launchd; Linux = systemd; Windows = service via `golang.org/x/sys/windows/svc`;
  Termux = termux-services. The installer abstracts these.

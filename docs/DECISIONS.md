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

## D8 — code-server for the workspace (phase 3), not Theia (yet)
**Why:** faster to embed/proxy; mature; delivers the file-explorer + editor + terminal + doc
view Dylan wants. Theia is the heavier "build a branded IDE" path — revisit if/when branding matters.

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

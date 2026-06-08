# Lattice

**A self-hosted, cross-platform mesh command center you can install.**

Lattice turns a pile of personal machines — Macs, Windows PCs, Linux boxes, even a
phone on the tailnet — into one private mesh with a single web dashboard. Install one
small agent on each device, pick one machine to be the **hub**, and you get live fleet
status, an interactive terminal for any machine, a file browser, wake-on-LAN, long-lived
Claude + terminal sessions, and a browser-based VS Code workspace — all over **your own
Tailscale tailnet**. No cloud, no account, no per-OS fiddling.

> The dashboard is just a web page the hub serves, so it works from any device that can
> reach the hub — including your phone. Tailscale (WireGuard) is the encryption layer; a
> single admin password sits on top. Your code and machines never touch anyone else's server.

---

## Requirements

- **One machine to be the hub** — any reasonably-always-on Mac, Windows PC, or Linux box. That is
  the only hard requirement. Prebuilt binaries need nothing else installed; building from source
  needs Go + Node.
- **Tailscale (free) — to connect machines across different networks.** Lattice serves plain HTTP
  and uses your [Tailscale](https://tailscale.com/download) tailnet (WireGuard) as both the
  reachability *and* the encryption layer, so every device — including your phone — can reach the
  hub from anywhere. Install it on each machine and join them to one tailnet.
  **Lattice detects this and walks you through it in the dashboard, but it never installs Tailscale
  for you** — you set that up yourself (it's a 2-minute install per machine).
  - *Just want to try it?* You **don't** need Tailscale to evaluate Lattice on a **single machine**
    (or machines already on the same Wi-Fi): one install command gives you a fully working
    dashboard — terminal, file browser, editor, sessions — with that machine already in it.
    Tailscale is only what turns it into a real cross-network mesh.
- **SSH and Syncthing are optional.** They unlock extra features (SSH-only reachability in the fleet
  view, the "sync your brain" Syncthing folder-pairing walkthrough) and are likewise detect-and-guide
  — never auto-installed.

## Quickstart

Two steps to a working dashboard. You don't need Tailscale or anything else to start.

### Step 1 — Run one command

On the machine you want as your dashboard (any Mac, PC, or Linux box), paste this into a
terminal:

**macOS / Linux:**
```sh
curl -fsSL https://github.com/shleesauce/lattice/releases/latest/download/get.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://github.com/shleesauce/lattice/releases/latest/download/get.ps1 | iex
```

That's the whole install. It downloads one small binary, starts the hub as a background
service that survives reboot, **adds this machine to your fleet automatically**, and prints a
link like `http://your-machine:7400`. *(It verifies the download's checksum first and refuses
to run anything that doesn't match.)*

### Step 2 — Open the link and set a password

Open the printed link in any browser. A short first-run wizard asks for three things:

1. **A password** — protects your dashboard.
2. **A name** for this fleet (anything you like).
3. **A folder** to look for your projects in.

Click through it and **you're done** — you land on your dashboard with this machine already
showing live: status, CPU/memory/disk, a terminal, a file browser, and a built-in VS Code
editor. On one machine, that's the whole setup. 🎉

### Add more machines (optional)

Want your other computers in the same dashboard? Two things:

1. **Put them on the same network.** For machines on different networks (home + laptop +
   phone), install [**Tailscale**](https://tailscale.com/download) (free) on each and sign them
   into one account — that's what lets them reach each other securely. Machines already on the
   same Wi-Fi don't need it. *(Lattice shows you exactly how in **Manage mesh → Integrations** —
   but you install Tailscale yourself; Lattice never installs it for you.)*
2. **Add the machine.** In the dashboard, open **Manage mesh → Add machine**, name it, and copy
   the one-line command it gives you. Run that line on the other machine — it joins in seconds.
   Each machine gets its own **revocable** token (remove the machine, the token dies).

**Manage mesh → Integrations** also detects **SSH** and **Syncthing** and walks you through the
optional extras (like a "sync your brain" folder-pairing flow) — again, detect-and-guide only.

### Changed your mind? Remove it completely

One command, removes everything, touches nothing else — see [Uninstall](#uninstall) below.

---

## What you get

- **Live fleet** — every machine, its status, CPU / memory / disk, deduped across the lattice
  agent, your Tailscale tailnet, and your SSH config.
- **Workspace** — long-lived **Claude** and **terminal** sessions that survive a browser refresh
  *and* a hub restart, smart placement across machines, and an embedded **VS Code** editor
  (code-server) tunnelled through the hub — reachable from any device at one URL.
- **Remote ops** — interactive terminal, file browser, and **wake-on-LAN** for any machine.
- **Manage mesh** — rename / remove machines, per-machine revocable join tokens, and the
  integrations detect-and-guide panel.
- **Single binary** — the same artifact is the hub or an agent depending on the subcommand;
  cross-compiled for darwin (arm64/amd64), windows (amd64), and linux (amd64/arm64).

## Security model

- **One admin password per hub** (single-tenant — each person runs their own hub for their own
  fleet). Set at first-run, stored bcrypt-hashed. A login session is an HttpOnly, SameSite=Strict
  cookie; a long-lived `Authorization: Bearer <token>` works for CLI/scripts.
- **Tailscale is the encryption boundary.** Lattice serves plain HTTP and relies on your tailnet
  (WireGuard) for transport security — no certificate management. Internet-exposed TLS is a future
  add, not required for the private-mesh use case.
- **Secure by default.** A fully-configured hub refuses to listen on a public address with no admin
  password — set one with `lattice hub set-password`, or pass `--insecure-no-auth` to intentionally
  run open on a trusted tailnet. Loopback binds and the first-run wizard are exempt. Auth is enforced
  as soon as a password is set.
- The master/enrollment token is passed to agents via the `LATTICE_TOKEN` environment variable, not
  the command line, so it doesn't show up in `ps` / process listings.

## Updating

```sh
lattice update            # download + swap in the latest release binary
lattice update --restart  # ...and restart the hub/agent service
```

`lattice update` **and** the install one-liners (`get.sh` / `get.ps1`) verify the release checksum
(`SHA256SUMS`) before installing or replacing the binary — a mismatch aborts, nothing is installed.

## Uninstall

Lattice is **fully self-contained and reversible.** One command removes it completely — the
service *and* the data directory — and touches nothing else on your machine.

**Easiest — if Lattice is installed, just run:**
```sh
lattice uninstall
```
It lists exactly what it will remove, asks you to confirm, then stops the service(s) and deletes
`~/.lattice`. Works offline. Add `--dry-run` to preview, or `--yes` to skip the prompt.

**Don't have the `lattice` command handy?** Run the same thing from the web:

```sh
# macOS / Linux
curl -fsSL https://github.com/shleesauce/lattice/releases/latest/download/uninstall.sh | sh
```
```powershell
# Windows (PowerShell)
irm https://github.com/shleesauce/lattice/releases/latest/download/uninstall.ps1 | iex
```

Either way it's idempotent — safe to run twice — and removes **both** the hub and the agent if
the machine ran either.

### Exactly what Lattice puts on your machine — and what the uninstaller removes

Everything Lattice installs lives in **one data directory plus one service file**, all at the
**user level — no `sudo`, no system directories, no admin:**

| | macOS | Linux | Windows |
|---|---|---|---|
| Binary, config, database, token, logs | `~/.lattice/` | `~/.lattice/` | `%USERPROFILE%\.lattice\` + `%LOCALAPPDATA%\Lattice\` |
| Persistent service | `~/Library/LaunchAgents/sh.lattice.{hub,agent}.plist` | `~/.config/systemd/user/lattice-{hub,agent}.service` | Scheduled task `LatticeHub` / `LatticeAgent` |

It does **not** edit your shell config or `PATH`, install anything system-wide, require root, or
touch your projects, your files, Claude / `~/.claude`, or your Tailscale / SSH / Syncthing setup —
Lattice only ever *detects and guides* those, never configures them. Removing Lattice leaves your
machine exactly as it was before.

> Prefer to remove it by hand?
> **macOS:** `for s in hub agent; do launchctl bootout gui/$(id -u)/sh.lattice.$s 2>/dev/null; done; rm -f ~/Library/LaunchAgents/sh.lattice.*.plist; rm -rf ~/.lattice`
> **Linux:** `systemctl --user disable --now lattice-hub lattice-agent 2>/dev/null; rm -f ~/.config/systemd/user/lattice-*.service; rm -rf ~/.lattice`

## Commands

```
lattice hub init [--mesh NAME] [--projects-root DIR] [--addr :7400]   scaffold config + token, pick a free port
lattice hub set-password --password PASS                              set/replace the admin password
lattice hub  [--addr :7400] [--db PATH] [--token CODE]                run the controller + dashboard
lattice agent --hub HOST:PORT --token CODE [--name NAME]              run a leaf agent
lattice update [--base URL] [--restart]                               self-update from the latest release
lattice doctor [--json]                                               diagnose this machine (config, hub, capabilities, integrations)
lattice uninstall [--dry-run] [--yes]                                 completely remove Lattice from this machine (services + ~/.lattice)
lattice version
```

## Build from source

Requires Go (see `go.mod`) and Node. From one machine, `scripts/build.sh` bundles the dashboard,
embeds it, and cross-compiles every target — pure-Go SQLite (`modernc.org/sqlite`) means
`CGO_ENABLED=0` cross-compiles cleanly with no per-OS toolchain.

```sh
bash scripts/build.sh   # → dist/lattice-<os>-<arch>[.exe]
```

A pushed `v*` git tag triggers `.github/workflows/release.yml`, which builds all targets +
checksums and publishes them (plus `get.sh` / `get.ps1`) as GitHub Release assets — which is what
makes the install one-liners above resolve.

## Repo layout

```
internal/hub/     the hub role — REST API, embedded dashboard, agent enrollment, sessions, auth
internal/agent/   the per-device agent — metrics, PTY, file ops, capabilities, tunnels
internal/proto/   the wire protocol shared by hub and agent
internal/tunnel/  the yamux tunnel used for the embedded editor + preview proxies
dashboard/        React + TS + Vite + Tailwind — the web UI the hub serves
install/          get.sh / get.ps1 (hub bootstrap) + uninstall.sh / uninstall.ps1 — published as release assets
scripts/          build + fleet helpers
docs/             architecture, decisions, roadmap, changelog
```

See `docs/ARCHITECTURE.md` for the design, `docs/DECISIONS.md` for the architectural
record, and `CHANGELOG.md` for release history.

## License

[Apache License 2.0](LICENSE) © 2026 shleesauce.

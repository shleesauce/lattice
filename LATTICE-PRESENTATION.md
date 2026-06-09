# Lattice

### Your machines, woven into one private mesh — with a single dashboard, from any browser.

Lattice turns a pile of personal computers — Macs, Windows PCs, Linux boxes, even a phone — into **one private mesh you control from a single web page**. Install one small program on the machine you want as your "hub," open the link it prints, and you get a live view of every machine, an interactive terminal and file browser for each one, long-lived Claude and terminal sessions, and a full **VS Code editor in your browser** — all over **your own private network**. No cloud account, no SaaS subscription, no per-machine fiddling. Your code and machines never touch anyone else's server.

> Everything described in this document has been verified against the actual source code — every feature listed is implemented and working, not a roadmap promise. A "what's under the hood" map is included at the end.

---

## Who it's for

- **Anyone running more than one computer** who's tired of SSH-juggling, VNC, and remembering which box has what.
- **Developers** who want a browser-based command center: jump into a terminal or a VS Code editor on any machine, from any device, in one click.
- **People using Claude / AI coding tools across machines** who want long-running sessions that don't die when they close the laptop lid or the browser tab.
- **Home-lab and multi-machine power users** who want a private, self-hosted mission control with zero cloud dependency.

### Real situations it solves

- *"I'm on my laptop in the kitchen and I want to check on a build running on my desktop upstairs."* → Open the dashboard on your phone, see it live, drop into its terminal.
- *"I started a long Claude session on my Mac Studio and had to leave."* → Reopen the dashboard anywhere; the session is still running exactly where you left it.
- *"I need to edit a file on my Linux box but I'm on a Windows machine."* → Open VS Code for that machine right in your browser.
- *"My desktop is asleep."* → Wake it from the dashboard with one click, then connect.

---

## What you can do

### See your whole fleet, live
Every machine appears on one screen with its real-time **CPU, memory, disk, and uptime**, updated continuously. Lattice is smart about identity: it **merges each physical machine across every way it can see it** — the Lattice agent, your Tailscale network, and your SSH config — so one computer shows up once, not three times, with a clear note of how it's reachable. Machines that are asleep or offline are shown honestly as "reachable" or "offline," never falsely green.

### Work on any machine, right in your browser
- **Interactive terminal** to any machine — a real shell, full keyboard, instant.
- **Claude sessions** — run your AI coding assistant on whichever machine has the horsepower, and drive it from the browser.
- **A real VS Code editor** (code-server) embedded in the page for any machine — browse, edit, and run, tunneled securely through the hub so nothing extra is ever exposed on the network.
- **Live app preview** — if you start a dev server on a machine, Lattice can proxy it to your browser so you can see it from anywhere.

### Sessions that refuse to die
This is the headline feature. Your terminal and Claude sessions:
- **Survive a browser refresh** — close the tab, reopen it, you're right back in with full scrollback replayed.
- **Survive a hub restart** — even if the controller itself reboots, your running sessions on each machine keep going and are automatically re-adopted. Nothing is lost.
- Keep a rolling **scrollback buffer** so you always reconnect with context, and Claude conversations are saved as a readable transcript with each tool run.

### It puts work on the right machine for you
When you start a session, Lattice **scores your machines** — free memory, current load, CPU cores, and whether you're already working there — and picks the best one automatically, while showing you the ranking. Need it on a specific box? Pin it. Machines that can't run the job (e.g., no editor installed) are filtered out before you ever hit an error.

### Remote operations
- **File browser** for any machine — list directories, open and download files.
- **Wake-on-LAN** — power on a sleeping machine from the dashboard; Lattice even remembers its hardware address so a *peer* machine can wake it.
- **Per-machine capability awareness** — Lattice knows which boxes have Claude, Node, a code editor, etc., and plans around it.

### Manage your mesh in one place
- **Add, rename, and remove** machines from a simple panel.
- **Per-machine join tokens you can revoke** — every machine you add gets its own credential; remove the machine and its token dies, so it can't rejoin.
- **Integrations panel** that **detects and guides** SSH, Syncthing, and Tailscale on each machine — including a "sync your folders across the mesh" walkthrough. Lattice *guides* you; it never silently installs or reconfigures these for you.

---

## Why it's safe

Lattice is built single-tenant and private by design — **you run your own hub for your own machines.** Verified protections:

- **One admin password** per hub, stored only as a bcrypt hash. Your browser session is an HttpOnly, strict-same-site cookie; scripts can use a bearer token.
- **Private network is the encryption layer.** Lattice rides on your **Tailscale** tailnet (WireGuard), so traffic between machines is encrypted end-to-end. There's no public endpoint to attack.
- **Secure by default.** A configured hub *refuses* to listen on a public address with no password unless you explicitly opt in for a trusted network.
- **Credentials never leak into process lists** — tokens are passed through environment variables, not command lines, so they don't show up in `ps` or Task Manager. Token comparisons are constant-time.
- **Brute-force resistant** — login attempts are rate-limited per source.
- **It only touches its own footprint.** Lattice never modifies your shell config, your files, your `~/.claude`, or your Tailscale/SSH/Syncthing setup. Everything it installs lives in one folder plus one service entry (details below).
- **Every download is checksum-verified** (fail-closed) before it runs — the installer and the self-updater both refuse to run a binary that doesn't match.

---

## Get started in ~2 minutes

You only need **one machine** to begin. No Tailscale, no accounts, nothing else.

### Step 1 — Run one command

On the machine you want as your dashboard, paste this into a terminal:

**macOS / Linux**
```sh
curl -fsSL https://github.com/shleesauce/lattice/releases/latest/download/get.sh | sh
```

**Windows (PowerShell)**
```powershell
irm https://github.com/shleesauce/lattice/releases/latest/download/get.ps1 | iex
```

That's the entire install. It downloads one small program (and **checks its fingerprint** before running it), starts it as a background service that survives reboots, **adds this very machine to your fleet automatically**, and prints a link like `http://your-machine:7400`.

### Step 2 — Open the link and set a password

Open that link in any browser. A short setup wizard asks for three things:

1. **A password** (protects your dashboard)
2. **A name** for your mesh (anything you like)
3. **A folder** where your projects live

Click through it and **you're done.** You land on your dashboard with this machine already showing live — its stats, a terminal, a file browser, and the editor. **On one machine, that's the whole setup.** 🎉

### Step 3 (optional) — Add your other machines

Want more computers in the same dashboard?

1. **Get them on the same network.** Machines on different networks (home desktop + travel laptop + phone) need **[Tailscale](https://tailscale.com/download)** — it's free, takes ~2 minutes per machine, and you sign each one into one account. *(Machines already on the same Wi-Fi don't need it.)* Lattice shows you the exact command and walks you through it — but you install Tailscale yourself.
2. **Add the machine.** In the dashboard, open **Manage mesh → Add machine**, give it a name, and copy the two-step command it hands you:
   - **Step 1:** get that machine on your network (the Tailscale command — skip if it's already there)
   - **Step 2:** join Lattice (one line)

   Run those on the other machine and it appears in your fleet within seconds.

---

## Remove it just as easily

Changed your mind? Offboarding is one command and removes **everything** — the service *and* all data — while touching nothing else on your machine.

**Easiest (if Lattice is installed):**
```sh
lattice uninstall
```
It lists exactly what it will remove, asks you to confirm, then stops the service(s) and deletes its data folder. Works offline. (`--dry-run` to preview, `--yes` to skip the prompt.)

**Or from the web, the same way you installed:**
```sh
# macOS / Linux
curl -fsSL https://github.com/shleesauce/lattice/releases/latest/download/uninstall.sh | sh
```
```powershell
# Windows
irm https://github.com/shleesauce/lattice/releases/latest/download/uninstall.ps1 | iex
```

It removes **both** the hub and the agent if a machine ran either, it's safe to run twice, needs **no admin rights**, and leaves your files, your shell, Claude, and your network setup exactly as they were. After it runs, your machine is just as it was before you ever installed Lattice.

---

## What it actually puts on your machine

Everything lives in **one data folder plus one service entry** (plus a `lattice` symlink in `~/.local/bin`, or a user-PATH entry on Windows, so the command works) — all at the **user level — no admin, no system directories:**

| | macOS | Linux | Windows |
|---|---|---|---|
| **Program, settings, database, logs** | `~/.lattice/` | `~/.lattice/` | `%USERPROFILE%\.lattice\` + `%LOCALAPPDATA%\Lattice\` |
| **The background service** | a LaunchAgent (`sh.lattice.hub` / `…agent`) | a systemd `--user` service (`lattice-hub` / `…agent`) | a Scheduled Task (`LatticeHub` / `LatticeAgent`) |

**Never touched:** your projects and files, your shell config, Claude / `~/.claude`, and your Tailscale / SSH / Syncthing setup.

---

## What you need (and honest limits)

**You need:**
- One reasonably-always-on machine for the hub (Mac, Windows, or Linux). That's the only hard requirement.
- **Tailscale** (free) on each machine **only** if you want to connect machines across different networks. Not needed to try it on one machine or a shared Wi-Fi.
- To run a **Claude session** or the **VS Code editor** on a given machine, that machine needs `claude` / `code-server` installed — Lattice detects this and only offers the feature where it'll actually work.

**Honest limitations:**
- Lattice relies on **Tailscale for transport encryption** (it serves plain HTTP on your private network); it is **not** designed to be exposed directly to the public internet.
- **Phones** get the full dashboard in their browser, but don't run an agent — you view and control other machines from a phone, you don't add the phone as a worker.
- A running **Claude session stays on the machine that created it** (it isn't migrated mid-flight to another box).
- The one-command install / self-update pull from **GitHub Releases**, so they work once a release is published (a local source override exists for development).

---

## Single program, every platform

Lattice is **one small binary** that acts as either the hub or an agent depending on how it's launched — cross-compiled for macOS (Apple Silicon + Intel), Windows, and Linux (Intel + ARM) from a single build, with no heavy dependencies. The same artifact runs everywhere, which is why install, update, and uninstall are all one-liners.

**Built-in commands:**

| Command | What it does |
|---|---|
| `lattice hub` | run the controller + dashboard |
| `lattice agent` | run a worker on a machine |
| `lattice update` | self-update to the latest release (checksum-verified) |
| `lattice doctor` | diagnose this machine (config, hub, capabilities, integrations) |
| `lattice uninstall` | completely remove Lattice from this machine |

---

## Under the hood — verified against the source

Every headline feature maps to real, working implementation. A sample of where each lives:

| Feature | Implementation (verified) |
|---|---|
| Live fleet + CPU/mem/disk metrics | `internal/agent/metrics.go`, `internal/hub/registry.go`, `dashboard/src/lattice/Fleet.tsx` |
| One machine merged across agent + Tailscale + SSH | `internal/hub/devices.go` (union-find dedup) |
| Sessions survive browser refresh (scrollback replay) | `internal/agent/ring.go` (256 KB ring), `internal/agent/terminal.go` |
| Sessions survive hub restart (re-adoption) | `internal/hub/store.go` (SQLite), `internal/hub/sessions.go` (`adoptSessions`) |
| Smart placement across machines | `internal/hub/placement.go` (`ScorePlacement`) |
| Browser VS Code, tunneled through the hub | `internal/agent/codeserver.go`, `internal/hub/editorproxy.go`, `internal/tunnel/` (yamux) |
| Live app preview proxy | `internal/hub/previewproxy.go` (port-allowlisted) |
| Interactive terminal (PTY over WebSocket) | `internal/agent/terminal.go`, `internal/hub/terminal.go` |
| File browser + Wake-on-LAN | `internal/agent/files.go`, `internal/agent/wake.go` |
| Per-machine revocable tokens; master token protected | `internal/hub/enrolltokens.go`, `internal/hub/store.go` |
| Password + cookie + bearer auth; login rate-limit | `internal/hub/auth.go` (bcrypt, constant-time, limiter) |
| Secure-by-default public-bind refusal | `internal/hub/hub.go` (`isPubliclyBound`) |
| Integrations detect-and-guide (never install) | `internal/hub/devices.go`, `internal/doctor/doctor.go`, `dashboard/.../ManageMesh.tsx` |
| One-command install + auto-enroll + checksum | `install/get.sh`, `install/get.ps1` |
| First-run setup wizard | `dashboard/src/components/FirstRunWizard.tsx` |
| Self-update (checksum-verified) | `internal/update/update.go` |
| One-command uninstall (offline + scripts) | `internal/uninstall/uninstall.go`, `install/uninstall.sh`, `install/uninstall.ps1` |
| Single binary, 5 cross-compiled targets | `main.go`, `scripts/build.sh` (`CGO_ENABLED=0`, pure-Go SQLite) |

---

*Lattice — self-hosted, cross-platform, private by design. One command to install, one command to remove, and your machines were never anyone else's business.*

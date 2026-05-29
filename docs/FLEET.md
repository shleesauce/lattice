# Dogfood Fleet Reference

The product is built and tested against Dylan's real fleet. All machines are on Tailscale
tailnet **`tail3c8bee.ts.net`** and reachable by MagicDNS name. There is already a full SSH
mesh + passwordless sudo (see global memory `reference_ssh_command_center`) — use it freely
to install/run/verify the agent during development.

| Alias | MagicDNS | OS | SSH user | Role | Sleeps? | Notes |
|-------|----------|----|----|------|--------|-------|
| mini-ops | mini-ops.tail3c8bee.ts.net | macOS 26.5 (M1, 8GB) | mini-ops | **HUB** | no (sleep=0) | always-on home anchor; build all binaries here |
| studio | studio-dylan.tail3c8bee.ts.net | macOS 26.5 | dylanstory | agent + hub failover | no (sleep=0) | office anchor |
| mbp | dylans-macbook-pro.tail3c8bee.ts.net | macOS 26.5 | dylanstory | agent | yes (lid) | Wi-Fi; WoL unreliable |
| pc | pc-dylan.tail3c8bee.ts.net | Windows 11 (26200) | dylan | agent | yes (S3/hibernate) | Ethernet, NIC wake-armed → WoL works; cmd.exe default shell |
| s26 | s26-dylan.tail3c8bee.ts.net:8022 | Android (Termux) | u0_a609 | agent (last) | n/a | fragile; Termux sshd dies when backgrounded |

Excluded (Dylan-only fleet): `kinzs-macbook-air`, `pc-kinzie`, `iphone-kinzie`.
Stale tailnet node "M" (offline) — ignore / flag for cleanup.

## Hub host (mini-ops) facts
- Tailscale IP 100.81.231.110; MagicDNS `mini-ops.tail3c8bee.ts.net`.
- **Free port for hub: 7400.** In use already: 3001, 4000, 5173, 5678, 5679, 8222, 8384
  (Syncthing), 22000 (Syncthing), 49273. Also: PM2 runs homebase backend/dashboard/agent +
  uptime-kuma + Vaultwarden; Caddy + `tailscale serve` use :8443. **Don't collide.**
- Toolchain: `node`, `pnpm`, `npm` present. **Go NOT installed** → `brew install go` first.
- PM2 is the canonical process manager on mini-ops — run the hub under PM2 for dogfooding
  (or launchd), consistent with the homebase pattern.

## Build strategy
Cross-compile every target FROM mini-ops:
- macOS agents: `GOOS=darwin GOARCH=arm64`
- PC agent:     `GOOS=windows GOARCH=amd64`  → copy `lattice.exe` to pc via scp
- (Linux/other later)
Never install a Go toolchain on the leaves.

## Wake-on-LAN
Only the **PC** is a live WoL case (Ethernet + wake-armed NIC, same LAN as mini-ops). Get its
MAC at build time via `ssh pc "getmac /v"` (the earlier `getmac /fo list` call glitched — use
`/v` plain or `ipconfig /all`). mbp is Wi-Fi → treat WoL as best-effort only.

## Known per-OS quirks (the agent is meant to ERASE these; mind them only in install/bootstrap)
- PC over SSH: cmd.exe default; `&` chaining no-ops; `findstr` mishandles `/`; prefer
  `ssh pc 'powershell -NoProfile -Command -'` with the script on stdin, or single commands.
- `ssh -n` nulls stdin (don't use it when piping a script via stdin to a remote bash).
- Termux: keep it last; needs foreground/wakelock; Termux:Boot for persistence.

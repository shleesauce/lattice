# Lattice

**A self-hosted, cross-platform mesh command center you can install.**

Lattice turns a pile of personal machines — Macs, Windows PCs, Linux boxes, phones —
into one private mesh with a single dashboard. You install one agent on each device,
pick one to be the hub, and you get: live fleet status, remote command execution, an
interactive terminal for any machine, a file browser, wake-on-LAN, and (later) a
browser-based VS Code-style workspace — all over your own Tailscale tailnet, no cloud,
no per-OS fiddling.

> **Provisional name.** "Lattice" is a working codename — rename before any public release.
> (Avoid collisions: `helm`=k8s, `fleet`=fleetdm.)

## The one thing that defines success

**Packageable.** Success is *not* "works on Dylan's machines" — it's that a stranger with
a dozen devices across platforms can run one installer per machine and stand up their own
mesh dashboard. Every decision is judged against that bar. Dylan's fleet is the dogfood
prototype, not the product.

## Status

Phase 0 — scaffolding. See `docs/STATE.md` for the live handoff and `docs/ROADMAP.md`
for the build order. Read `docs/ARCHITECTURE.md` before writing code.

## Repo layout

```
agent/       Go — the per-device agent (single binary, all platforms)
hub/         Go — the hub role (serves API + dashboard; same binary, role=hub)
dashboard/   React + TS + Vite + Tailwind — the web UI the hub serves
install/     Per-OS installers (install.sh, install.ps1, termux)
scripts/     Dev/build/dogfood helpers
docs/        Architecture, roadmap, decisions, state (handoff), fleet reference
```

## Quick context for a fresh session

This project was born out of a hand-built SSH+Syncthing+Tailscale mesh across Dylan's
fleet (see global memory: `reference_ssh_command_center`, `reference_file_sync`,
`project_mesh_command_center`). That hand-built setup proved the concept and is the
dogfood target. Lattice productizes it: one uniform agent that hides every per-OS quirk
we hit doing it by hand.

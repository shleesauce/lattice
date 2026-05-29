# Lattice — New-Session Kickoff Prompt

Paste everything below the line into a fresh Claude Code session started in
`~/AI-Hub/projects/lattice/`. Run it on Opus, autonomous.

---

You are building **Lattice**, a packageable cross-platform mesh command center. The project
is scaffolded at `~/AI-Hub/projects/lattice/`. **Before doing anything, read these in order:**
`docs/STATE.md`, `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, `docs/DECISIONS.md`, `docs/FLEET.md`,
and the global memory files `project_mesh_command_center`, `reference_ssh_command_center`,
`reference_file_sync`. They contain every decision, the dogfood fleet, the ports, and the
per-OS landmines already learned. Do not re-litigate settled decisions (DECISIONS.md) without a
concrete reason.

**The goal that defines success: PACKAGEABLE.** Not "works on my machines" — a stranger with a
dozen mixed-OS devices must be able to install one agent per machine and get a working mesh
dashboard. Judge every choice against that.

**Operating mode — full autonomy. Do not stop to ask.** You have full SSH access to the whole
fleet (aliases `studio`, `mbp`, `emu`, `pc`, `s26`) and passwordless sudo on the Macs + admin on
the PC. Use it. Install dependencies (`brew install go` first), write code, cross-compile,
deploy agents to the real machines, run them, and verify — all without checking in. Only surface
to Dylan when (a) a phase is genuinely complete and testable, or (b) you hit a true hard blocker
that no reasonable default resolves. Pick sane defaults for everything else and record them in
STATE.md.

**Use parallelism and verification loops aggressively:**
- Spin up parallel subagents for independent tracks (e.g. Go hub, Go agent, React dashboard,
  installer) via the Agent tool — brief each like a new hire with the relevant docs.
- After each track lands, spawn **independent verification ("loopback") agents** that exercise
  the *running* system and adversarially check the implementer's claims — never trust a
  self-report. Verify by: SSH into each machine to confirm the agent service is up; `curl` the
  hub API; Playwright-screenshot the dashboard and assert live machines render; run a real
  command end-to-end (UI → hub → agent → back) and assert the output. Use desktop control,
  browser control, Playwright, screenshots, SSH — whatever proves it actually works.
- Consider driving the whole build with the Workflow tool (fan-out implement → adversarial
  verify → synthesize) since this is large, multi-track, opt-in orchestration.

**Build order (full detail in ROADMAP.md):**
1. **Phase 1 — Heartbeat + Registry + Remote Exec.** Go `lattice` binary (`hub` + `agent`
   subcommands); agents dial the hub over WebSocket and heartbeat; hub persists to SQLite,
   serves a React/Vite/Tailwind dashboard (dark-first) on port **7400**; dashboard shows the
   live fleet and can run a one-shot command on any machine with streaming output.
   **Cross-compile from mini-ops** (darwin-arm64 for the Macs, windows-amd64 for the PC); never
   install Go on the leaves. **Dogfood: hub on mini-ops; agents on studio, mbp, pc.**
   Done when: open the dashboard in a browser, see 4 live machines, run `uname -a` on the Macs
   and `ver` on the PC from the UI and watch output stream back.
2. **Phase 2** — interactive xterm.js terminal per machine, file browser, wake-on-LAN (wake the PC).
3. **Phase 3** — code-server workspace proxied through the hub.
4. **Phase 4 (the success criterion)** — per-OS installers (`install.sh`, `install.ps1`, termux),
   one-time-token enrollment, "create your mesh" onboarding, GitHub Releases + Homebrew + winget.

**Discipline:**
- Commit at meaningful milestones with concise messages (the repo lives in the synced `ai-hub`
  folder — `.gitignore` already excludes build artifacts/secrets).
- Keep `docs/STATE.md` current — update it as you finish/￼start work so any session can resume.
- Respect the safety rails in global CLAUDE.md (confirm before destructive/shared-infra ops; the
  fleet is shared with Kinzie's separate machines — don't touch those).
- Run the hub under PM2 on mini-ops (matches the homebase pattern) unless you find a reason not to.

**Deliver:** a working, browser-testable Lattice that Dylan can open and drive against his real
fleet — at minimum Phases 1–2 fully working and verified, with Phase 3–4 as far as you can take
them — then report back with how to open it and what to test.

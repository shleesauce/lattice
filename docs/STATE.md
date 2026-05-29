# Lattice — Live State / Handoff

**Read this first every session.** Update it at the end of every working session: what's
done, what's in flight, what's next, what's blocked. This is the source of truth for resuming.

---

## Current phase
**Phase 0 → Phase 1.** Scaffold complete. No code written yet. Next action: stand up the
`lattice` Go module with `hub` + `agent` subcommands (see ROADMAP Phase 1).

## What exists
- Repo scaffold at `~/AI-Hub/projects/lattice/` (syncs across the fleet via Syncthing `ai-hub`).
- Docs: README, ARCHITECTURE, ROADMAP, DECISIONS, FLEET, this STATE, and KICKOFF.md.
- Git initialized; nothing committed yet.
- Empty dirs: agent/ hub/ dashboard/ install/ scripts/.

## What does NOT exist yet
- Any Go code, any dashboard code, any installer.
- Go is not installed on mini-ops (`brew install go` is step 1 of Phase 1).

## Environment facts (verified 2026-05-29)
- Hub host = mini-ops; port 7400 free; build host = mini-ops (cross-compile all targets).
- Full SSH mesh + passwordless sudo already exists across the fleet — use it to deploy/verify.
- Dogfood fleet + per-OS quirks: see FLEET.md.

## Build/verify loop (use every phase)
After implementing, VERIFY by exercising the running system, not by reading code:
- SSH into each target, confirm the agent process/service is up.
- `curl` the hub API; assert fleet membership + status.
- Playwright screenshot the dashboard; assert machines render live.
- Run a command through the full UI→hub→agent→back loop; assert real output.
Spawn verification subagents for this; don't trust an implementer agent's self-report.

## Open questions for Dylan (don't block on these; pick sane defaults and note them)
- Final product name (Lattice is provisional).
- Whether to run the hub under PM2 (recommended, matches homebase) or launchd.

## Changelog
- 2026-05-29: Phase 0 scaffold created (originating session, ~50% context, before handoff).

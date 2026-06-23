# Lattice — Live State / Handoff

**Read this first every session.** Update it at the end of every working session: what's
done, what's in flight, what's next, what's blocked. This is the source of truth for resuming.

---

## ⛔ HARD RULE — NO headless `claude -p` (D35, 2026-06-01)
The Claude tab is an **interactive `claude` in a PTY**, NOT headless stream-json. Do **not** add
`-p` / `--print` / `--output-format stream-json` / `--input-format stream-json` /
`--include-partial-messages` / `--replay-user-messages` anywhere in Lattice — headless usage moved to a
separate capped Agent SDK credit pool on June 15 2026; interactive stays on normal subscription limits.
The launch seam is `claudeCommand()` in `internal/agent/terminal.go` (guardrail comment lives there).
Do not reintroduce headless mode until Dylan **explicitly** authorizes it. See **D35** (supersedes D17/D21).

---

## Current phase

### ✅ v0.2.1 — SHIPPED + FLEET-DEPLOYED (2026-06-12). Tag v0.2.1, GitHub **Latest**, **whole fleet 5/5 on v0.2.1**.
The held post-ship audit (12 fixes) + real-use dogfood fixes (terminal reliability BUG-002/006/007, quieter
ntfy BUG-005, red status dots BUG-009, in-panel file viewer BUG-008) were bundled into **v0.2.1** and released.
**Cut:** CHANGELOG `[0.2.1]` → tag → release.yml run **27395584284** (now WITH the go vet/test gate) published all
11 assets + SHA256SUMS → `latest`→v0.2.1. **Deployed:** restarted the pm2 hub to refresh its release cache, fired
the one-click cascade v0.2.0→v0.2.1 (**all 5 acked `updated`, no storm, no dupes — D38 held again**), pm2-restarted
hub+agent onto the swapped v0.2.1 binary; all 5 reconnected on v0.2.1, banner cleared, `pm2 save` done. The hub now
serves the v0.2.1 dashboard (file viewer / status dots / reconnect fix) — **users must hard-refresh the browser** to
load the new asset bundle. **Still OPEN for a v0.2.2 (need a phone / a repro):** BUG-001 mobile Workspace layout +
BUG-003 terminal paste (both HIGH, device-only verifiable), BUG-004 Fable-5 field (needs repro), BUG-005 live notify
toggle (follow-up feature). Mini-ops hub runs `dist/lattice-darwin-arm64` under pm2 with `LATTICE_TOKEN` env (the
master token — NOT the file). Full scope in `docs/V0.2.1-PLAN.md`; dogfood log in `docs/DOGFOOD-BUGS.md`.

### ✅ v0.2.0 MILESTONE — SHIPPED (2026-06-11). Tag v0.2.0 @ `b061502`. (superseded by v0.2.1 above)
**Built over 5 sessions** (audit → fixes → identity centerpiece → security/backend HIGHs → frontend/installer → ship),
held on local master per the v0.1.5 discipline, then shipped in S5. **Decision (Dylan):** stop cutting a public release
per small patch; make v0.2.0 a deliberate multi-session milestone. Full triage in **`docs/V0.2.0-MILESTONE.md`**.

**Post-ship audit (2026-06-11, after the tag) — 9 fixes on master, UNRELEASED, held per discipline (commits `b02643e..af32836`).**
A token-conscious pass over the shipped v0.2.0 diff (4-reviewer fan-out) found + fixed **2 HIGH passwordless-hub holes
the S3 work missed** — file-read/download/remove ungated (read `.lattice-token` → fleet RCE; or `/remove` DoS) and
`/api/enroll/tokens` leaking/minting enroll creds unauthenticated — plus 2 D38 identity correctness bugs (pre-v0.2.0
agents minted unpersistable UUIDs → session orphaning; `adopt()` swallowed persist failures), the updater redirect
HTTPS-pin gap, 3 S4 frontend defects, installer IPv6/`$USER` robustness, and a release.yml `go test`/`vet` gate. These
ride the NEXT release; **do not redeploy the fleet for them.** Open RECOMMENDATIONS for Dylan (bigger/judgment): the
`register()` per-machine-token impersonation gap and the duel-detector non-convergence (service-managed ping-pong +
ntfy storm) are the two worth doing next — see the milestone doc's "Post-ship audit" section.

**Dogfood pass (2026-06-11) — v0.2.1 assembly started.** Logged 9 real-use bugs in `docs/DOGFOOD-BUGS.md`
(mostly mobile), ran a 9-agent triage against the real code → `docs/V0.2.1-PLAN.md` (the consolidated v0.2.1
scope: held audit + dogfood + carried recs). **SHIPPED the crown-jewel HIGH fix** (commit on master, held):
mobile terminal/session reliability — hub ws **keepalive** (relayBrowserInput had no ping/pong → mobile NAT
dropped idle sockets) + client **auto-reconnect** (it showed "reattaching…" but never did — refresh-only) +
**debounced resize** (fixes BUG-002/006/007). Validated go build/vet/test + dashboard build/eslint; **needs
on-device confirmation**. v0.2.1 now = 17 commits ahead of `v0.2.0` tag. Remaining dogfood work scoped in the
plan: 001 mobile-layout + 003 paste (device-tuned), 009 status-dots + 008 file-viewer (no device, next batch),
005 notify policy (needs Dylan's 1-line pref), 004 (needs a repro). **2 decisions for Dylan in V0.2.1-PLAN.md.**

**Pass 2 (same day) — prevent-recurrence + supply-chain, 4 more commits on master (`b4225aa..750bbe0`), held.**
Attacked the *cause* of the pass-1 HIGHs (authz wired per-handler → siblings missed): (1) a **declarative
route-authorization table** (`routePolicy` + `gate()` in `internal/hub/http.go`) — every route names a policy, unknown
policy fails closed, no route can ship ungated; (2) **centralized agent sub-action authz** in an `agentActions` map
enforced in `handleAgentSub` before dispatch (unlisted action 404s; 5 inline guards removed; `TestAgentSubActionGating`
added); (3) **Go toolchain 1.26.3→1.26.4** closing 2 *called* stdlib vulns (`govulncheck` re-run → 0 affecting code).
Also ran `go test -race ./...` → no races. Total post-ship: **13 fixes across both passes, all unreleased.** Installer
dedup deferred (served installer is an embedded text/template). The 2 security recs above remain the top next-work.

**Session 5 (2026-06-11) — SHIPPED + fleet-deployed + the D38 migration proven live.** Assembled CHANGELOG `[0.2.0]`
from S1–S4 (commit `b061502`), pushed master (`2b5c263..b061502`), tagged **v0.2.0** → release.yml run **80917580778**
published all 11 assets (incl. the NEW `lattice-windows-arm64.exe`) + SHA256SUMS + the CHANGELOG body; `latest` →
v0.2.0; windows-arm64 checksum verified; published binary stamps `v0.2.0`. **Then fired the REAL one-click cascade
v0.1.9→v0.2.0 on the live fleet** (hub's release cache had already refreshed → no pre-restart needed): **all 5 acked
`updated` in 8s** (pc/emu/mbp/studio self-restarted their service; mini-ops pm2 swap-on-disk), **err-log frozen (NO
storm)**, **pc stayed ONE process (no duel)**. Re-execed the pm2 hub + mini-ops agent onto v0.2.0; all 5 reconnected
in ~20s. **D38 migration PROVEN end-to-end on real hardware:** every agent re-registered under its SAME `host-os` id
(`agents` table still exactly the 5 ids, no orphans/dupes, zero session loss) — the empty-AgentUUID→adopt-legacy path
worked through the mixed-version window AND after the hub went v0.2.0. `updateAvailable=false`, banner cleared,
`pm2 save` done. **The one-click cascade remains the standard regression test — it just passed its hardest run yet
(an identity-format migration) with zero incident.**

**Residual live-verifications NOT done in S5 (need Dylan present / a billed turn — NOT v0.2.0 blockers, these are
prior-version features that already shipped + are unit-tested):**
- **/fpreview HMR on a phone** (v0.1.7 residual) — needs Dylan's device. The responsive frontend (S4) is also best
  eyeballed on a real phone now that v0.2.0 is live — same session.
- **Telemetry $cost/context% + auto-name on a billed Claude turn** (v0.1.5 residual) — needs a real billed session.
- **Mesh-editor (code-server) on the darwin fleet** (M2-P2 / workstream A) — was NEVER built in v0.2.0 (it's the next
  milestone's headline), so there's nothing to "verify"; it's future work, not a v0.2.0 gap.

**Session 1 (2026-06-11) — audit + 6 fixes. Local commit `446d3db` (ahead 1, not pushed).** Ran a 4-agent read-only
audit (security / backend-resilience / frontend-UX / ops-distribution) → triaged into fix-now vs backlog (table in
the milestone doc). Shipped the low-risk wins:
- **[CRIT ops]** `install/get.sh` `set -euo pipefail` → `set -eu` — `-o pipefail` is a bashism that ABORTS under a
  real `/bin/sh` (dash/BusyBox = the Linux target), so the advertised `curl|sh` Linux cold-install died on line 11.
  (NOTE: the PUBLIC one-liner pulls the v0.1.9 *release asset*, so the public installer stays broken until a release
  ships this fix — rides v0.2.0 unless Dylan wants a one-off hotfix release.)
- **[CRIT frontend]** `ErrorBoundary` around App + the lazy Workspace — a render throw no longer white-screens the console.
- **[HIGH backend]** no `recover()` on bare goroutines = one panic crashes the hub. Added `superviseLoop`
  (restart-on-panic) for the 5 long-lived loops + `goSafe` for the transcript-derivation one-shots.
- **[MED security]** per-machine enroll tokens 64→128-bit. **[MED backend]** `audit_log` reap → index-friendly range
  delete. **[MED ops]** refreshed `docs/ROADMAP.md` (M3 shipped, v0.2.0 framed).
- All green: `go build/vet/test`, `tsc`/`eslint`/`build`, `get.sh` parses under dash+sh.

**Session 2 (2026-06-11) — THE CENTERPIECE: agent identity (D38). Held on local master (commit `446d3db`+1, NOT pushed).**
Retired the reconnect-storm class at the root instead of patching causes. The agent id is no longer `hostname+os`
(derived fresh per register → same-hostname collisions + silent process duels); it's now a **persistent per-machine
id** stored at `~/.lattice/agent-id` and sent in the additive `RegisterPayload.AgentUUID`. The hub keys its registry
on it; **hostname/os are display-only**. A new per-process **`InstanceID`** + a **duel detector** in `register()`
handle two rival processes for one id: **newcomer wins**, the loser's instance is **banished 60s** (so its reconnect
gets `OK:false` → non-retryable → it stops/exits), with a **loud WARN + ntfy**. Newest-process-wins matches reality
(a re-enroll/restart beats a stale orphan); a managed loser restarts with a fresh instance and re-wins cleanly.
- **Migration is safe + additive (the careful part):** `resolveAgentID` is **hub-authoritative with legacy
  continuity** — an empty `AgentUUID` reuses the legacy `host-os` id *iff a record already exists under it* (existing
  fleet keeps its id → **zero session orphaning**), else mints a fresh UUID. Two same-hostname NEW boxes get distinct
  UUIDs (the minted id is stored under the UUID, never `host-os`). Old agents send empty fields ⇒ behavior identical
  to today; the duel check needs both instances non-empty, so a mixed-version fleet degrades to legacy with no false
  positives. Full protection once the new binary is fleet-wide (deploy later in the milestone). `lattice uninstall`
  already wipes `~/.lattice`, so the id file self-cleans.
- **Files:** `internal/proto/proto.go` (+AgentUUID/InstanceID), hub `identity.go` (NEW: resolveAgentID/newAgentUUID/
  agentRecordExists/agentDisplayName/duelGuard) + `agentws.go` + `registry.go` (+instanceID) + `hub.go` (+duel field,
  errDuelRejected, offline/read-skew comment) + `store.go` (+AgentExists); agent `identity.go` (NEW: persistent id +
  processInstanceID + adopt/persist) + `agent.go` (thread identity, adopt hub-assigned id) + `tunnel.go` (read id from
  the shared holder, retry until assigned). Removed dead `computeAgentID`/`osID`.
- **Verified:** `go build`/`vet`/`go test ./...` all green; new unit tests (resolveAgentID 3 paths, duelGuard TTL,
  proto round-trip + omitempty, agent persist/instance-stability) + a **real-WebSocket end-to-end duel test**
  (P1 live → P2 newcomer wins → P1 reconnect refused → winner same-instance reconnect accepted); all 5 cross-compile
  targets build. Dashboard untouched (no id-format parsing; existing fleet ids stay `host-os`).

**Session 3 (2026-06-11) — security + backend HIGHs. Held on local master (NOT pushed).** Shipped 7 fixes:
- **[HIGH security] Passwordless hub fail-closed.** New `requirePrivileged` (auth.go): the RCE-class
  endpoints — `handleExec` / `handlePower` / `handleUpdate` — now require the master token as a Bearer
  credential EVEN when no admin password is set (auth was a pass-through before → anyone reaching the port
  could run arbitrary commands on every fleet box). When a password IS set, requireAuth already gated it, so
  this only bites the auth-off path. Trade: a passwordless browser dashboard can't fire these without the
  token (set a password for cookie auth).
- **[HIGH security] Setup-wizard takeover gate.** `setupAllowed` (setup.go) gates POST `/api/setup` to
  loopback OR master-token Bearer, so a remote tailnet peer can't claim admin in the first-run window.
  `/api/setup/status` now reports `tokenRequired` (per-connection loopback check); the wizard shows a "hub
  token" field when remote and `submitSetup` sends it as Bearer. check-root stays open (read-only).
- **[HIGH backend] Cascade wall-clock budget.** `updateAgents` caps total cascade time at `updateFleetBudget`
  (6 min) and clamps each agent to `min(updateAgentTimeout, remaining)`; once the budget is spent the rest are
  marked `pending` (applies on next start), so a few wedged agents can't pin the single-flight lock for the
  whole fleet.
- **[HIGH backend] pm2 hub `restartRequired`.** `handleUpdate` detects the hub's service label before
  responding and returns `restartRequired:true` + `restartHint` when it runs under something Lattice can't
  restart (pm2 / bare process) — instead of a misleading green check followed by a reconnect-poll that reloads
  onto the OLD code. UpdateProgress renders an amber "restart required" panel with the command.
- **[MED backend] SQLite transactions** on the three multi-row reaps (`purgeDeleted`,
  `MarkAgentSessionsExitedExcept`, `DeleteSessionRow`) — wrapped in one tx each, ops routed through the tx
  handle (required under the single-conn pool); shared `deleteSessionTx` helper.
- **[MED security] Rate-limit** the two ungated capability endpoints (`/api/hooks/state`,
  `/api/approvals/{nonce}`): generic per-IP `rateLimiter` (300/min, loopback-exempt), swept on the hourly tick.
- **[MED security] HTTPS-pin** the binary self-updater (`update.Apply`) — `requireSecureBase` rejects a
  non-https download base unless loopback or `LATTICE_DOWNLOAD_INSECURE=1` (the documented local mock-cascade
  opt-out); skipped under `--insecure`.
- **Verified:** `go build/vet/test` + `gofmt` + all 5 cross-compile targets + dashboard `tsc`/`eslint`/`build`
  green; new unit tests (requirePrivileged 3 cases, setupAllowed, requestIsLoopback, rateLimiter,
  requireSecureBase, isLoopbackHost); **live smoke on a throwaway loopback hub** (live `:7400` untouched) —
  passwordless exec/update → 401 without token, exec → 404 (reaches handler) WITH the master token, update →
  401 with a wrong token; fresh-init hub: setup-status `tokenRequired:false` on loopback, POST `/api/setup`
  succeeds token-free from loopback, status flips `needsSetup:false`. The remote-rejection setup path is
  unit-tested (TestSetupAllowed).

**Session 4 (2026-06-11) — frontend responsive/a11y + cross-OS installer gaps. Held on local master (NOT pushed).**
Shipped 8 fixes via two parallel subagents over disjoint file sets (orchestrator re-ran every verification gate):
- **[CRIT frontend] The app's first `@media` breakpoints** (`design/app.css`) — tablet ≤900px + phone ≤640px for
  Fleet/TopBar. Phone collapses `.cr3`'s 3-col grid (rail+map+side) into ONE flex column; topbar drops to
  logo+seg+icon-search+version+settings. **Purely additive — zero existing rules touched, desktop byte-identical**
  (verified: `git diff` has no `-` lines outside additions).
- **[HIGH frontend] FleetMap motion controller** (`lattice/FleetMap.tsx`) — the forever-60fps rAF now pauses under
  `prefers-reduced-motion` AND when the tab is hidden (holds a single static frame), with live `matchMedia`/
  `visibilitychange` handling and leak-safe cancel-before-reschedule. Fleet/selection changes push a static redraw
  so paused mode still reflects updates.
- **[HIGH frontend] Keyboard/AT machine selection** (`Fleet.tsx`, `FleetMap.tsx`, `app.css`) — rail rows + Recent
  rows are now operable buttons (Enter/Space, `aria-pressed`, the primary AT path); the canvas got `tabIndex`/
  `role=application`/live `aria-label`/arrow-cycle selection + `:focus-visible` outlines.
- **[HIGH frontend] Focus trap** — new `useFocusTrap(active)` hook (useEscape house style) wired into `Modal` +
  `CommandPalette`: Tab/Shift+Tab containment + focus-restore-on-close, without stealing the palette's own input focus.
- **[MED frontend] `useReleases` error distinction** — returns `{data,error}`, keeps last-good data across a
  transient failure; `VersionBadge` shows a muted amber dot + "couldn't check for updates" instead of implying current.
- **[HIGH ops] Windows-on-ARM** — `build.sh` builds `windows/arm64` + `release.yml` publishes
  `lattice-windows-arm64.exe`; both PS installers (`get.ps1`, `install.ps1.tmpl`) detect arch via
  `PROCESSOR_ARCHITECTURE`+`PROCESSOR_ARCHITEW6432` (the latter catches an amd64 PS emulated on ARM) → arm64 vs amd64.
- **[HIGH ops] Linux reboot survival** — best-effort `loginctl enable-linger` (non-sudo → `sudo -n` → verify) after
  `systemctl --user enable --now` in `get.sh` + `install.sh.tmpl`; falls back to the existing manual tip, never fatal
  under `set -eu`. A headless box now survives reboot without an interactive login.
- **[MED security] Installer-side HTTPS-pin** — `get.sh`/`get.ps1` now reject a non-https `LATTICE_DOWNLOAD_BASE`
  unless loopback or `LATTICE_DOWNLOAD_INSECURE=1` (mirrors the Go `requireSecureBase` added in S3; default GitHub
  https base unaffected).
- **Verified:** dashboard `tsc`+`vite build` + `eslint src` exit 0; `app.css` diff zero-deletion (desktop unchanged);
  `gofmt`/`go vet`/`go test ./...` exit 0; `windows/arm64` cross-compiles (20M bin); `sh -n` + `dash -n` clean on
  `get.sh` + rendered `install.sh.tmpl`. **Pending:** frontend live-verify used a mocked-API Playwright harness (no
  real hub) → a real-hub phone-width glance worth it on next deploy; no `pwsh` on this macOS host so the PowerShell
  installers are hand-reviewed only (`get.ps1`/`install.ps1.tmpl`). `scripts/deploy-fleet.sh:25` still hardcodes
  `windows-amd64` but it's the operator fleet-push (not an arch-detect/release surface) — left as-is.

**NEXT (S5 — the final milestone session, with Dylan):** residual live-verifications (S2 mesh-editor on the darwin
fleet; /fpreview HMR-on-phone; telemetry $cost/context% + auto-name on a billed Claude turn) + full regression (the
one-click cascade as the standard test) + assemble CHANGELOG `[0.2.0]` + **deploy the v0.2.0-dev build to the fleet**
+ tag/push/release. Smaller residual backlog: DockPreview iframe states, Workspace token-drift, Windows/WSL2 editor,
single-box hub watchdog, the LOW goroutine/CSRF sweep. Full triage in `docs/V0.2.0-MILESTONE.md`. Live fleet stays
**v0.1.9** until the S5 deploy.

### ✅ v0.1.9 — RELEASED (2026-06-10). Dashboard is now fully offline + the one-click cascade is PRODUCTION-PROVEN end-to-end.
Tag **v0.1.9** @ `2b5c263`, Latest. release.yml run **27325388090**. **Whole fleet 5/5 on v0.1.9**, hub v0.1.9, banner
cleared, pm2 saved.

**Change:** the dashboard no longer loads Google Fonts. `dashboard/index.html` pulled JetBrains Mono + Space Grotesk
from the Google CDN, but NOTHING in the UI used either family — the app renders in locally-vendored Hanken Grotesk +
IBM Plex Mono (`--font-ui`/`--font-mono`, `src/design/fonts/fonts.css`). So those `<link>`s were a render-blocking
request to Google for fonts that were downloaded and never applied. Removed them → a self-hosted fleet console now
loads with ZERO automatic external requests (offline/airgap + privacy + faster first paint). **Proven zero-visual:**
exhaustive grep — no CSS/TSX/inline-style references "Space Grotesk" or "JetBrains" anywhere; built bundle has no
googleapis/gstatic; live login page Playwright-screenshotted on the v0.1.9 hub renders correctly in the local fonts
(no fallback). (Closes the "vendor Google Fonts before release" item in "Known minor items".)

**✅ THE CASCADE IS NOW PRODUCTION-PROVEN (the regression test).** Fired one-click Update on the LIVE fleet
(v0.1.8→v0.1.9) — the FIRST real public cascade with the COMPLETE fix stack on both ends (ack-before-restart v0.1.6 +
Windows self-exit v0.1.8 + tri-state). Result: **all 5 acked `updated`; pc stayed EXACTLY ONE `lattice.exe` (PID
9416, v0.1.9); err-log 4s-delta = 0 (no storm).** This confirms the Windows fix in production via a genuine public
release (v0.1.8 was verified via a local mock cascade; this is the real thing). The one-click update path is now the
clean end-to-end regression test for every future release — fire it, watch all-`updated` + pc-single + no-storm.
Hub + local mini-ops agent are pm2 (no launchd) so they swap-on-disk but need a `pm2 restart` to re-exec (done);
the 4 launchd/schtask agents self-restart cleanly. Release-cache is 30-min TTL → `pm2 restart lattice-hub` first to
make the hub see the new tag.

### ✅ v0.1.8 — RELEASED (2026-06-10) + the Windows self-orphan fix VERIFIED ON REAL pc. Tag **v0.1.8** @ `02e0dec`, Latest.
release.yml run **27323831598** published the CHANGELOG `[0.1.8]` body + all assets. **Whole fleet 5/5 on v0.1.8**,
hub v0.1.8, banner cleared, pm2 saved.

**What it fixes — the Windows reconnect storm from the v0.1.7 cascade.** The agent's Windows self-restart was
`schtasks /End`+`/Run`, but `/End` does NOT kill the wscript-wrapped `lattice.exe` that *initiated* the call, so old
+ new agents dueled under one id. KEY INSIGHT: an EXTERNAL `/End` (fleet-sync's installer over SSH) kills the agent
cleanly — only the agent calling `/End` on ITSELF fails. **Fix** (`internal/agent/update.go`): after a SUCCESSFUL
restart on Windows the agent `os.Exit(0)`s (indirection var `exitAfterRestart`, gated on `goos=="windows"`), so the
scheduled task's fresh instance is left as the sole agent. NEVER exits on a restart ERROR (would leave no agent).
macOS `kickstart -k` / linux `systemctl restart` already SIGKILL the old process, so they're untouched. Plus a hub
**single-flight guard** (`h.updating atomic.Bool` in `handleUpdate`): an overlapping `POST /api/update` → 409.
Unit-tested: windows-self-exits / restart-error-no-exit / non-windows-no-exit / overlap-409 (all green).

**✅ VERIFIED ON REAL WINDOWS (the gate the bug-fix required).** Couldn't use the public cascade (the fix is in the
RECEIVING agent, so it only applies to the NEXT cascade after deploy — same pattern as v0.1.6→v0.1.7). Instead:
shipped v0.1.8 → fleet-sync (pc on the fixed agent, 1 process) → then pointed the LIVE hub at a **local mock "v0.1.9"**
release over the tailnet (built v0.1.9-stamped bins + SHA256SUMS, served on `0.0.0.0:7491`, set the hub's
`LATTICE_RELEASES_API`+`LATTICE_DOWNLOAD_BASE` via `pm2 restart --update-env` re-supplying its real LATTICE_TOKEN +
NTFY so they weren't dropped) → fired `POST /api/update` ONCE → **all 5 acked `updated`; pc ended with EXACTLY ONE
`lattice.exe` on v0.1.9 (PID 9452); err-log 4s-delta = 0 (NO storm)**. Before the fix the same self-restart left TWO
dueling at ~1/sec. **Then fully RESTORED:** emptied the hub's mock env vars (pm2 `--update-env` MERGES — can't drop a
var by unsetting; set them to "" → code falls back to GitHub default), stopped the mock server, `fleet-sync` →
whole fleet back to real **v0.1.8**, `updateAvailable=false`, pc single, err log flat, pm2 saved, artifacts cleaned.
(Mock v0.1.9 binary was just v0.1.8 code re-stamped, so the fleet was never on foreign code.)

**FOLLOW-UP NOTE:** the published GitHub `latest` is **v0.1.8** (no phantom v0.1.9 was ever pushed public). The next
real release is v0.1.9 → cut it normally when there's real content.

### ✅ v0.1.7 — RELEASED (2026-06-10) + the REAL hardened-cascade live test PASSED. Tag **v0.1.7** @ `cea7afb`, Latest.
release.yml run **27323168481** published the CHANGELOG `[0.1.7]` body + all assets. **Whole fleet 5/5 on v0.1.7**,
hub v0.1.7, banner cleared, pm2 saved.

**Feature:** framework dev-server preview (Vite/Next). New `/fpreview/{agent}/{port}/` NO-STRIP route forwards the
FULL path (`servePreview(strip=false)` in `internal/hub/previewproxy.go`) so a `base`-configured Vite/Next serves
correctly (the old `/preview/` strip mode 404'd their root-absolute `/@vite/client` assets). Dock Preview tab gained
a **Vite/Next** toggle that switches the iframe to `/fpreview/` and shows the copy-paste `--base=/fpreview/…` string.
`previewproxy_test.go` proves strip→`/` vs no-strip→full-path. **Smoke-verified LIVE on the v0.1.7 hub** (curl through
the real mini-ops tunnel: `/preview/…/@vite/client`→backend sees `/@vite/client`; `/fpreview/…/@vite/client`→backend
sees the full `/fpreview/…/@vite/client`; trailing-slash 302 OK). **Residual (Dylan's device):** live HMR/hot-reload
over the WebSocket with a real Vite/Next app, browser then phone — can't be done headless. Also new in v0.1.7:
`LATTICE_RELEASES_API` override shipped in v0.1.6 carried forward.

**✅ THE REAL HARDENED-CASCADE LIVE TEST (v0.1.6→v0.1.7) — PASSED.** This is the test deferred from v0.1.6 (the
v0.1.5→v0.1.6 cascade couldn't test the fix; this one can — both ends were v0.1.6 hardened). Fired `POST /api/update`
on the live v0.1.6 hub. **All 5 agents acked `status=updated`, ZERO timeouts, ZERO red failures** — including
**studio, which reported "did not respond in time" in v0.1.5, now cleanly `updated` (restarted `sh.lattice.agent`)**.
The launchd/schtask agents (studio/emu/mbp/pc) restarted their service AFTER acking (the ack-before-restart fix
working live); mini-ops (pm2) acked with no self-restart as expected. Hub release cache is 30-min TTL → had to
`pm2 restart lattice-hub` first to make it see v0.1.7 before firing. Hub + mini-ops agent are pm2 (no launchd) so
they swap-on-disk but need a pm2 restart to re-exec v0.1.7 (done).

**🔴 NEW BUG SURFACED BY THE LIVE TEST — Windows agent orphans itself on one-click update → reconnect storm.**
During the cascade pc (DESKTOP-2HF6TVF) ran TWO agents dueling (v0.1.6 ⇄ v0.1.7, ~30 reconnects each, "superseded by
reconnect"). Root cause: the agent's self-restart on Windows is `schtasks /End` then `/Run` (`restartByLabel` in
`internal/update/update.go`) — but `/End` does NOT reliably kill the wscript-wrapped `lattice.exe` that *initiated*
the call, so the old process survives while `/Run` spawns a new one. darwin's `launchctl kickstart -k` SIGKILLs the
old cleanly; Windows has no equivalent here. (My double-fire of `/api/update` amplified it, but a SINGLE fire would
likely orphan too.) **RECOVERED live:** SSH to pc → `taskkill /F /IM lattice.exe` (killed both) + stopped the wrapper
→ `schtasks /Run /TN LatticeAgent` once → 1 agent on v0.1.7, err log flat, fleet 5/5 stable. **FOLLOW-UP (next, see
spawn_task chip):** Windows agent must kill the old process / `os.Exit` after triggering `/Run` so the update doesn't
orphan it. Needs real pc verification (Parsec/UAC friction noted below). Until fixed, a Windows box in a one-click
cascade may need a manual `taskkill /F /IM lattice.exe` + `schtasks /Run /TN LatticeAgent`. See AI-Hub LESSONS-LEARNED.

### ✅ v0.1.6 — RELEASED (2026-06-10). Public `origin/master` + tag **v0.1.6**, marked Latest on GitHub.
Cascade hardening. release.yml run **27321815054** published the CHANGELOG `[0.1.6]` body + all 5 binaries +
get.sh/get.ps1 + SHA256SUMS + uninstall scripts. Tag at `cf56439`. **Published artifact verified** (downloaded
`lattice-darwin-arm64` stamps `v0.1.6`, checksum OK vs the published SHA256SUMS; `releases/latest` → v0.1.6).
**Fleet: all 5 on v0.1.6** (mini-ops/studio/mbp/emu/pc) via `scripts/fleet-sync.sh` (NOT the live cascade — see
below); hub `version=v0.1.6`, `/api/releases.updateAvailable=false` (banner cleared); pm2 saved.

**Why NOT deployed via the live one-click cascade (important):** the original plan ("the v0.1.5→v0.1.6 cascade IS
the hardened re-test") rested on a false premise. During a cascade the running hub swaps its binary on disk but
orchestrates the agents **in old memory** (the new `classifyAgentUpdate`/tri-state only runs after it restarts,
which is the LAST step), and the receiving agents are still the OLD build. So a v0.1.5→v0.1.6 cascade runs pre-fix
code on BOTH ends — it would reproduce the same rough v0.1.5 behavior and test none of the hardening. The fix only
gets a genuine cascade on the NEXT release. So v0.1.6 shipped via fleet-sync, and **the real hardened-cascade live
test is deferred to the v0.1.6→v0.1.7 cascade** — now de-risked by a full local proof (below).

**✅ LOCAL HARDENED-CASCADE TEST — PASSED (Dylan's chosen gate before tagging).** Stood up a throwaway hub + two
agents (all real v0.1.6 binaries, loopback only, copied bins, throwaway HOME/db/port — live :7400 untouched) against
a mock release server (new `LATTICE_RELEASES_API` override + `LATTICE_DOWNLOAD_BASE`) serving a "v0.1.7" build, then
fired `POST /api/update`. **8/8:** hub detected v0.1.7 → real download → SHA256 verify → atomic swap → both agents
acked over the live ws as `status=updated` (the exact path that read as a TIMEOUT in v0.1.5) → all three binaries
swapped to v0.1.7 on disk. Complemented by the deterministic unit tests (`TestHandleUpdateAcksBeforeRestart` proves
ack-BEFORE-restart ordering; `TestClassifyAgentUpdate` proves the updated/pending/failed tri-state). NOTE: the
harness has no installed service, so the agent acks+skips the restart — the kickstart-racing-the-ack itself is
covered by the ordering unit test, not the integration run (installing a real `sh.lattice.agent` launchd job on the
canonical node would risk the live fleet's label).

**New in v0.1.6 beyond the hardening:** `LATTICE_RELEASES_API` env override (`cf56439`) — a self-hosted fork can
point the release-notes panel + update check at its OWN releases instead of being pinned to upstream shleesauce
(also what unlocked the local mock-release cascade test). In `internal/hub/releases.go` via `releasesEndpoint()`.

**v0.1.6 commits (`e8e136f` + `e66066a` + `cf56439`, all pushed). Cascade-hardening detail:**
- **Agent acks BEFORE restart** (root cause of the studio "did not respond in time"). v0.1.5 called
  `update.Restart()` — which `kickstart -k` kills the process — and only THEN `sendFrame`, so the restart raced the
  ack off the wire. Split `update.ServiceLabel()` (detect, no side effect) from `update.RestartByLabel()`; the agent
  now puts the label in the result, **acks the hub, waits `restartGrace` (750ms), THEN restarts**. Proven by a
  deterministic ordering test (`TestHandleUpdateAcksBeforeRestart` asserts the frame is queued before restart runs).
- **Tri-state outcomes**: hub classifies each agent `updated`/`pending`/`failed` (`classifyAgentUpdate`, table-tested).
  A round-trip timeout or an agent that dropped mid-cascade is **pending** (non-fatal — applies on next start), NOT a
  red failure; only an explicit agent error is `failed`. Hub also re-checks liveness right before each dispatch so a
  sleeping agent is marked pending immediately instead of blocking the fleet for `updateAgentTimeout`.
- **UI** renders the tri-state (amber `pending` dot + reason vs red `failed`) — **screenshot-verified** via a throwaway
  hub: studio=green "updated", mbp=amber "no confirmation yet — applies on restart", emu=red "checksum mismatch".
- **Real on-fleet cascade re-test = the FUTURE v0.1.6→v0.1.7 cascade**, not the v0.1.5→v0.1.6 one (which runs
  pre-fix code on both ends — see "Why NOT deployed via the live cascade" above). De-risked locally (8/8 mock
  cascade) + unit-proven. So when you cut v0.1.7, fire one-click Update on the live fleet and watch: agents should
  ack as `updated`, any slow/sleeping box shows amber `pending` (not red `failed`), no reconnect storm.

### ✅ v0.1.5 — RELEASED (2026-06-10). Public `origin/master` + tag **v0.1.5**, marked Latest on GitHub.
release.yml published the CHANGELOG `[0.1.5]` body + all 5 binaries + SHA256SUMS + installers (run 27282532198,
2m27s). Tag at `e706ffe`. Fleet on v0.1.5: **studio / emu / pc / mini-ops live on v0.1.5**; **mbp** (closed-lid
MacBook) re-slept and is on its prior binary — reconnects/upgrades when opened. Hub `version=v0.1.5`,
`/api/releases.updateAvailable=false` (header alert cleared). Direction: "your machines as one AI team you don't
have to babysit."

**Real one-click-cascade test (the approved final step) — findings:** firing `POST /api/update` while the fleet
was on the pre-release dev build worked but was ROUGH: the hub self-updated safely (pm2 host → no disruptive
self-restart, binary swapped), pc reached v0.1.5, BUT studio's update RPC reported "did not respond in time" and
the sleeping mbp dropped — AND a **reconnect storm** erupted on pc (two agents sharing one id: a manual
`Start-Process` verification agent I'd left running from the prior session + the cascade-restarted task agent —
see AI-Hub/LESSONS-LEARNED 2026-06-10). Recovery: `taskkill` both pc agents + stop task → storm dead; re-ran
`fleet-sync` (HEAD==tag so it stamps exactly `v0.1.5`) → studio/emu/pc/mini-ops clean on v0.1.5. The studio
timeout coincided with the storm flooding the hub, so it may be load-induced, not a systematic race — **needs a
clean-fleet re-test + hardening: spawned task `task_80fd182f` (v0.1.6).** The update still APPLIES on a timeout
("applies on next start"); nothing corrupted. Track C wake confirmed AGAIN here: mbp wake returned
`onSubnet:true` via relay `desktop-2hf6tvf-windows` (the full same-subnet relay path) and physically woke mbp
(it re-slept mid-install before the download finished).

**Build/verify history below (pre-release):**

- **✅ Per-session Claude permission mode** (`f0e07b1`) — picker + live Shift+Tab cycle button.
- **✅ Fire-and-forget phone notify + approve/deny** (`8951899`, 2026-06-09) — BUILT + VERIFIED, not yet
  fleet-deployed. "Ping my phone" toggle on a Claude session → when it goes quiet waiting on input
  (or finishes) the hub pushes ntfy with **Approve / Deny / Open** buttons; Approve=Enter, Deny=Esc,
  Open deep-links the session. Agent `watchIdle` reuses `lastOutNano` (works with no browser attached),
  emits `session_idle` frames (`LATTICE_IDLE_SECS`, def 45s). Hub: `notify.go` (ntfy client, reuses
  `LATTICE_NTFY_*`) + `approvals.go` (single-use expiring **capability URL** `/api/approvals/{nonce}`,
  ungated — nonce is the credential, no master token on the broker — injects keystroke via `term_input`).
  `audit_log` now has its first writer (`LogAudit`); `notify_on_idle` persisted per session. Verified:
  vet+build clean, 20 new tests, dashboard builds, ntfy.sh accepted the live action-button JSON, running
  hub 410s ungated on a bad nonce + fresh-DB migration OK.
  - **To activate on the live fleet:** deploy the new binary (`scripts/fleet-sync.sh`) + set `hubUrl`
    in the live hub's `~/.lattice/config.json` (needed for the Approve/Deny buttons) + `LATTICE_NTFY_TOPIC`
    in its env (reuse `homebase-mini-ops-1b595c0d11a1`, the topic the phone already subscribes to).
- **✅ In-app release notes + CHANGELOG-driven release bodies** (`3464720`, Phase G) — "What's new" panel.
- **✅ 4 parallel tracks MERGED to local master** (2026-06-09) — built by 4 background opus agents in isolated
  worktrees off `0bad383`, each independently re-verified, then merged **A→B→C→D**. All conflicts were additive
  (struct fields, switch cases, payload structs, CHANGELOG bullets) — resolved keep-both. Post-merge: full
  `go build`/`vet`/`test` green, dashboard `tsc`+`vite` clean, merged binary boots with no panic and every new
  endpoint answers (`/api/hooks/state`, `/api/update`, `/api/workflows`, `/api/releases`). Track branches kept
  (`v015-session-core`/`v015-naming`/`v015-fleet`/`v015-update`); merge commits `86c59ca`/`dbd0610`/`4d83ced`/`4f878b1`.
  - **Track A — Session Launch & Intelligence** (`91f3013`/`1e678a3`/`7a90760`/`90a5caa`): **J** model picker in
    New Session (default Opus 4.8 1M, `--model`/`--effort low`, allow-list validated agent-side); **C** per-session
    Claude Code hooks via `--settings` (Stop/Notification(permission_prompt)/SessionEnd → token-gated
    `POST /api/hooks/state`, replaces the 45s PTY-quiet heuristic; telemetry derived hub-side from the transcript →
    rich cards); **D** PR-URL detection off the transcript → card link + one "PR opened" ntfy; **E** workflow
    templates (Implement Issue / Review PR → seeded worktree session, auto-placed). NO headless `-p` (D35 honored).
  - **Track B — Session Naming** (`6908216`): right-click/double-click inline rename (`PATCH /api/sessions/{id}`,
    title-locks the session) + free heuristic auto-name from the first user message (never an LLM call — D35 billing).
  - **Track C — Fleet & Wake** (`4663b59`): relay-aware WoL (picks a live same-subnet agent to emit the packet,
    "no relay reachable on that subnet" instead of silent no-op), wake-then-place in one action, agent self
    sleep/shutdown (`POST /api/agents/{id}/power`).
  - **Track D — One-Click Auto-Update** (`c937eaf`): admin-gated `POST /api/update` → hub self-updates then
    cascades agents in **lockstep** (new `TypeUpdate`/`TypeUpdateResult` proto), reusing the fail-closed
    SHA256SUMS verify + atomic swap; "update available → vX" banner + progress UI. Verified vs a mock
    `LATTICE_DOWNLOAD_BASE` (real download→verify→swap; tampered checksum → 502, binary untouched).
- **✅ FLEET-DEPLOYED + LIVE-VERIFIED on the real fleet (2026-06-09).** Ran build gate → `scripts/fleet-sync.sh`.
  Hub + all 4 darwin agents (mini-ops/studio/mbp/emu) now on **v0.1.5 (`v0.1.4-15-g4f878b1`)**; local
  `lattice-agent` also restarted onto it. **Activation done:** `hubUrl=http://mini-ops.tail3c8bee.ts.net:7400`
  in `~/.lattice/config.json` (backup `config.json.bak-prev015`) + `LATTICE_NTFY_TOPIC=homebase-mini-ops-1b595c0d11a1`
  injected into the hub via `pm2 restart --update-env` + `pm2 save` (confirmed in the live process env; ntfy now
  actually enabled for the first time). **Verified LIVE:**
  - **J** — created a real claude session on studio; the actual process is
    `claude --session-id … --permission-mode plan --model claude-opus-4-8[1m] --settings <hooks>` → the **1M model
    variant launches** + `--settings` wired + **no `-p` (D35 honored at runtime)**. Placement correctly excluded
    mbp/mini-ops/pc with reasons.
  - **C** — `~/.lattice/hooks/notify.sh` laid down on studio; ran it with the session's real injected env → POST
    `/api/hooks/state` over tailnet returns 200 and **a valid-token Stop writes a `turn_done` audit row**
    (`audit_log` id 48/49, agent `studio-darwin`) — a bad token 200s without auditing, so this proves acceptance.
    Precise-state pipeline is live. Agents also report `lanIPs` (subnet reporting live).
  - **C wake** — `POST /api/agents/desktop-2hf6tvf-windows/wake` (empty body) → hub resolved MAC, chose relay
    `mini-ops-darwin`, emitted the magic packet, returned `onSubnet:false` (any-live fallback) — **and it
    physically woke the sleeping Windows box** (exited→reachable→SSH up), which let the pc install complete.
  - **B** — `PATCH /api/sessions/{id}` renamed the session live (unicode title persisted).
  - **E** — `/api/workflows` live: bad URL → 400 "not a GitHub issue or pull-request URL", GET → 405, auth-gated.
  - **D** — banner source `/api/releases` live (`updateAvailable:false`, running ≥ public v0.1.4, 3 releases);
    `/api/update` admin-gated (no-token 401, GET 405). Cascade NOT fired (disruptive + needs a real newer release).
- **⚠️ Residual NOT-live-verified (deferred, documented):** telemetry $/ctx% derivation + auto-name derivation
  (need a real billed claude turn; both unit-tested and their idle trigger is the same path proven above);
  PR-detect ntfy (needs a real PR URL in output; unit-tested); wake-then-place full round-trip + power
  sleep/shutdown (no eligible sleeping box; unit-tested — wake half proven by the pc boot); full update cascade
  (mock-verified; gating live-verified). **pc (Windows): v0.1.5 BINARY deployed but agent not yet enrolled** —
  woke headless via WoL so the logon-scoped `LatticeAgent` task hasn't run (`LastTaskResult 0x41303`); it will
  enroll on Dylan's next interactive logon. Not a v0.1.5 regression (binary installed fine; darwin all enrolled).
- **✅ Header version badge + prominent one-click-update alert** (`4ada3c7`, 2026-06-09; CHANGELOG `e706ffe`).
  Dylan ask: surface the running version IN the header and make "you're out of date" unmissable. Built: a subtle
  `v…` chip in the topbar (click → What's new) that, when `/api/releases.updateAvailable`, becomes a pulsing teal
  "Update available → vX" badge + a full-width under-header strip ("You're on … — Lattice vX is available" +
  **Update now** + what's new + dismiss). Reuses the existing Track-D update flow (`UpdateProgress` extracted from
  the old slim `UpdateBanner`, which was deleted — no double banner; new `UpdateAlert.tsx` + shared `useReleases`
  hook). Verified: dashboard build clean, **Playwright screenshots of BOTH states confirmed good** (up-to-date chip
  vs out-of-date alert+strip+Update-now modal). Hub rebuilt + restarted → the new dashboard is LIVE
  (hub `v0.1.4-16-g4ada3c7`); CHANGELOG `[0.1.5]` filled in the missing bullets (model picker, session cards,
  one-click update, templates) + date → 2026-06-09 so the GitHub release body is complete.
- **Still local-master-only (head `e706ffe`, ahead 17). Public stays v0.1.4. NOT ready to auto-declare tag/push** —
  core paths are live-verified but residuals remain Dylan's call before tagging v0.1.5: pc Windows agent durably
  online (binary deployed + enrolls; needs a logon), and optionally a real one-click cascade + a billed-turn check
  of telemetry/auto-name. The new update UI itself is screenshot-verified.

### ✅ FLEET RELIABILITY: dead watchdog REVIVED + reconnect storm KILLED (2026-06-07)
Resumed from a clean backlog and investigated the STATE-flagged watchdog "health anomaly." Found TWO real
production bugs — both fixed and verified live; no fleet-sync needed (watchdog is mini-ops-only; storm fix was
operational).

**🔴 Bug 1 — the D33 "always-on layer 3" auto-recovery had been silently DEAD for ~2 days.**
- Root cause: `scripts/fleet-watchdog.sh` read the enroll token **once at startup** (line 28) and probed the
  now **auth-gated** `/api/devices` as its reachability gate. The master token was rotated on 2026-06-05
  (go-live prep) *after* the watchdog process started Jun-4, so the daemon held the **stale** token in memory.
  Post-rotation every sweep got **401 → empty body → misreported as "hub unreachable" → skipped**. A 401 was
  indistinguishable from a down hub, so the failure hid in plain sight (curl `-fsS` swallows the status).
- The 17951 pm2 restart counter was a **stale cumulative** count from the old bash-3.2 crash loop (process was
  actually up 3.5 days, 0 unstable) — confirmed cosmetic, reset to 0 via `pm2 reset` (no process restart).
- **Fix (committed):** the sweep loop now (1) **re-reads `.lattice-token` every sweep** so a future rotation
  self-heals without a manual restart; (2) gates reachability on the **ungated `/api/health`** so an auth
  problem can never masquerade as "hub down"; (3) captures the `/api/devices` HTTP status and surfaces a
  **401/403 as a distinct LOUD error** ("auth rejected — token stale/rotated?") instead of "hub unreachable".
- **Verified live:** `bash -n` clean on bash 3.2.57; split-logic unit-tested (200 w/ token, 401 w/o);
  `pm2 restart lattice-watchdog` → re-read the rotated token → first sweep passes health + gets 200 + parses
  the roster + correctly no-ops (all 5 watched machines online = healthy silent success path). err log empty.

**🔴 Bug 2 — a continuous agent RECONNECT STORM (~150 reconnects/min) flooding the hub.**
- Surfaced while reading hub.err.log (5.7 MB). emu/mbp/studio each registered under **two alternating
  versions** (`v0.1.0` ⇄ `v0.1.0-2-g975298e`) → proof of **two agent processes per box sharing one id**
  (`<host>-darwin`), each "superseded by reconnect"-evicting the other in an infinite duel.
- Root cause: the Jun-5 re-enroll over SSH started the new launchd-managed agent (`~/.lattice/bin/lattice`)
  but never stopped the **old repo-path orphan** (`~/<user>/lattice/lattice`, ~27 h old, NOT under launchd).
- **Fix (operational):** on each of emu/mbp/studio, kept the launchd `sh.lattice.agent` pid, killed the
  unmanaged orphan. **Verified:** orphans did NOT respawn (1 agent/box now); hub.err.log went from 2–3
  lines/sec to **fully silent** (froze at 19:02:58, confirmed 2+ min quiet); fleet view = 5 machines online.
- **✅ RESOLVED (2026-06-08): the `agents:6` vs 5 quirk was a stale ghost row.** `/api/health` reports
  `len(fleet())`, and `fleet()` = live registry ∪ the persisted `agents` DB table ([hub.go:407](../internal/hub/hub.go)).
  The 6th was **`pc-windows`** (name "pc", offline, last seen Jun-6 20:05) — a dead duplicate of the same
  physical Windows box that now correctly registers as `desktop-2hf6tvf-windows`. It registered as `--name pc`
  once on Jun-6 (had also been removed at go-live Jun-5, then reappeared). Its only session was already
  `exited`; no enroll token bound. Removed via the P4 endpoint `POST /api/agents/pc-windows/remove` →
  `{"ok":true}` → `agents:5`, fleet/devices both clean at 5/5 online. **Permanent:** the live Windows box runs
  a single `lattice.exe` with `--name "DESKTOP-2HF6TVF"` (hostname) — confirmed via SSH `wmic` — so there is
  no `--name pc` source to recreate the ghost.

**State (2026-06-08):** working tree clean. **`29486ac` PUSHED** to `origin/master` (`github.com/shleesauce/lattice`);
`master` even with origin. Live hub unchanged (`v0.1.0-4-gbbbdfe2`), now reporting a clean `agents:5`. No agent
redeploy needed. **Both prior open items closed.**

### ✅ CODE-AUDIT BACKLOG FULLY CLEARED (2026-06-07) — shared <Modal> SHIPPED & DEPLOYED
The last backlog item is done. There were **two ways to build a floating dialog**: the canonical
`.scrim`/`.modal` token chrome (ConfirmDialog, SettingsPanel, NewSessionDialog) and an ad-hoc Tailwind zinc card
(ManageMesh — `fixed inset-0` + `bg-black/60`) that had drifted from the design contract. Added one
`dashboard/src/components/Modal.tsx` built on the canonical chrome (scrim + centered card, click-outside via
mousedown, Esc, `role=dialog`/`aria-modal`) and routed all four onto it:
- **ConfirmDialog / SettingsPanel / NewSessionDialog** — zero visual change (same `.scrim`/`.modal` classes), now
  via `<Modal>`; gained consistent a11y role + Esc-to-close.
- **ManageMesh** — dropped its zinc card + `bg-black/60` for `<Modal flush width={768}>`, so it now wears the
  contract chrome (raised bg, teal glow ring, blurred scrim). New `.modal.flush` CSS = padless flex-column card
  for the tabbed layout; body content left as-is (the tokens sit coherently on `--raised`, no seam).
- **Login / FirstRunWizard** — deliberately left as full-screen gates (not dialogs-over-content).
- **CommandPalette** — left as its bespoke `.cmdk` palette (top-aligned, animated; not a centered dialog).
**Decision (Dylan, 2026-06-07):** full merge to the contract style + verify via a throwaway no-password hub.
**Verified:** `tsc`/`build`/`eslint` green; Playwright screenshots of all four modals against a throwaway hub
(loopback `:7411`, no password — secure-by-default refuses a public open bind) match the contract; ManageMesh's
3 tabs render clean. **Committed `bbbdfe2`, PUSHED; hub rebuilt + `pm2 restart lattice-hub` → live
`v0.1.0-4-gbbbdfe2`, 6 agents.** Frontend-only → agents NOT redeployed. **The code-audit backlog (hooks +
modal) is now empty — future work starts fresh from this doc.**

### CODE-AUDIT BACKLOG CLEANUP — hook unification SHIPPED & DEPLOYED (2026-06-07)
The three dashboard data hooks (`useFleet`/`useDevices`/`useWorkspace`) each carried an identical copy of the
same plumbing — alive-guard lifecycle, initial load, optional poll, the single shared `/ws/dashboard`
subscription, reconnect resync. Extracted that skeleton into `dashboard/src/useLiveResource.ts`; each hook keeps
its own state shape, fetchers, and WS-frame semantics (fleet **carries** the agent list, devices **triggers** a
refetch + 6s poll, workspace carries sessions + optimistic upsert/remove + 4s poll). Deliberately NOT flattened
into one generic — the differences are real, not noise. **Return shapes byte-for-byte unchanged → zero consumer
edits.** Behavior-preserving; `tsc`/`npm run build`/`eslint` all green. **Committed `8a34589`, PUSHED to
origin/master, hub rebuilt (`scripts/build.sh`) + `pm2 restart lattice-hub`.** Live: `/api/health`→
`version:v0.1.0-3-g8a34589`, 6 agents, new SPA bundle served. Frontend-only → agents NOT redeployed (they don't
serve the SPA; full `fleet-sync.sh` would've been a no-op redeploy of 5 boxes). NOT done: logged-in click-through
(hub is now auth-gated — needs the admin password). **Remaining backlog item: merge the 2 dialog/modal styles
into one shared `<Modal>` (~9 screens, eyeball each vs the design contract).**

### MILESTONE M3 — "DISTRIBUTABLE": make Lattice a shareable self-hosted product
Roadmap (6 phases, decisions locked with Dylan): `~/.claude/plans/encapsulated-juggling-hennessy.md`.
Goal: someone else installs Lattice cold — download → stand up a hub machine → optionally enable
SSH/Syncthing/Tailscale → add their machines from the dashboard. Distributable, safe, secure.

**Phase 0 — Foundation fixes: SHIPPED `c2df0ef` (2026-06-04).** Audit-driven perf/correctness base for a
public build. SQLite single-conn + WAL pragmas + startup checkpoint (4MB unbounded WAL → 107KB); cached
agent capabilities (killed 3 subprocess spawns/5s/agent); coalesced fleet broadcasts (O(agents²)/tick →
≤1/sec); TTL-cached tailscale/ssh reads; reaped `command_history` + session index. Frontend: deduped
`useWorkspace` (fixed split-brain), one shared `/ws/dashboard` socket (4→1), deleted dead command-run
subsystem. Repo: `.github/workflows/ci.yml` (go vet/test/build + advisory lint), `.golangci.yml`, eslint,
hardened `deploy-fleet.sh`. Verified live: WAL stays small, CI green.

**Phase 1 — De-personalize: SHIPPED `5a27655` (2026-06-04).** Removed every "Dylan-only" assumption.
New `internal/hub/config.go` loads `~/.lattice/config.json` (`projectsRoot`/`meshName`/`addr`/
`excludedDevices`/`aiHubIntegration`); values become flag DEFAULTS so flags still override. `devices.go`
exclude-list is config-driven (dropped the hardcoded kinzie list). `projectcreate.go` AI-Hub registration
(+`~/AI-Hub/CLAUDE.md` scaffold text) gated behind `aiHubIntegration` — a generic hub scaffolds a plain
skeleton. Ops scripts: lifted tailnet host + fleet map + ntfy topic into gitignored `scripts/fleet.env`
(template `fleet.env.example`); deploy-fleet/fleet-sync/fleet-watchdog source it. No tailnet/ntfy/kinzie/
literal token left in any tracked script. Dylan's hub behavior preserved via his `~/.lattice/config.json`
(aiHubIntegration=true + kinzie excludes). Verified live: config loads, 24 projects listed, fleet reconnected.

Both committed to master (not pushed); the live hub on mini-ops runs the Phase 1 binary.

**Windows console-hide fix: SHIPPED `3275e3b` (2026-06-04, prior session's work, committed this session).**
The `LatticeAgent` schtask now launches via a hidden `wscript` wrapper so it never flashes a console window
on (re)launch. `install.ps1.tmpl` generates the wrapper at enroll time; `scripts/windows/hide-agent-window.ps1`
is the one-shot fixer for already-enrolled boxes. (Detail in the Windows note below.)

**Phase 2 — Hub bootstrap installer + first-run wizard: BUILT & VERIFIED END-TO-END (this session).**
Fixed the chicken-and-egg (distribution used to assume a hub was already running). Three pieces:
- **`lattice hub init`** subcommand (`internal/hub/init.go` + `main.go` dispatch): mkdir `~/.lattice` 0700,
  persists a **stable** enrollment token (`~/.lattice/.lattice-token`, reused across restarts), picks a free
  port (prefers `:7400`, else an OS-granted port), writes `config.json` with `setupComplete:false`. Idempotent
  (re-run = no-op unless `--force`; never churns the port/token out from under a running service).
- **Setup state + endpoints** (`internal/hub/setup.go`, `config.go`): `GET /api/setup/status`,
  `POST /api/setup/check-root` (live path validation — exists / will-create / parent-missing, `~` expansion),
  `POST /api/setup` (validates, **bcrypt cost-12** hashes the admin password, load-modify-saves config
  preserving existing fields, applies `projectsRoot`/`meshName` LIVE under a `cfgMu`). All three gated 409
  once complete. **No auth is enforced yet** — Phase 2 only collects+hashes+stores the password; Phase 3
  wires the login/middleware. Stable token resolution added to `hub.go`: flag → `LATTICE_TOKEN` → persisted
  file → generate+persist (only the empty-flag path changed, so pm2's `--token` is untouched).
- **First-run web wizard** (`dashboard/src/components/FirstRunWizard.tsx` + App.tsx gate): full-screen 4-step
  gate (admin password+confirm → mesh name → projects root w/ debounced live check → review → Finish), reusing
  the `NewProjectWizard` chrome. App.tsx now gates on `/api/setup/status` (fails OPEN on error so a blip can't
  lock you out); the heavy Dashboard hooks moved into an inner component so the gate stays hooks-legal.
- **Hub bootstrap installers** (`install/get.sh` + `install/get.ps1`, NEW, standalone — NOT hub-served):
  detect OS/arch, download the binary from GitHub Releases (`LATTICE_DOWNLOAD_BASE` overridable for local
  testing), run `hub init`, install a persistent **hub** service (launchd `sh.lattice.hub` / systemd
  `lattice-hub` / Windows schtask `LatticeHub` reusing the hidden-wscript pattern), start it, print the
  finish-setup URL. The real GitHub Releases pipeline is Phase 5.

**Backward-compat:** `setupComplete` is a `*bool`, so a legacy config with no such field (Dylan's running hub)
→ `NeedsSetup=false` → never sees the wizard.

**Verified (against a throwaway HOME + db on `:7411`, the live `:7400` hub untouched):** `hub init` writes
config+token, idempotent re-run, free-port pick; the unconfigured hub serves the wizard (Playwright screenshot
`/tmp/lattice-wizard.png` — branded 4-step UI); `setup/status`=needsSetup:true, `check-root` all three cases,
`setup` rejects short password (400) then succeeds (200, bcrypt `$2a$12$…` in config, `setupComplete:true`,
projects root created + `/api/projects` live-scans the NEW root), re-submit→409, status no longer leaks; the
browser gate flips to the dashboard post-setup. Dylan's real `~/.lattice/config.json` confirmed unchanged.
`go build/vet/test` green, `npm run build` green, all 5 cross-compile targets built.

**NOT done in P2 (by design / deferred):** auth ENFORCEMENT (Phase 3); the live GitHub Releases hosting for
`get.sh`/`get.ps1` (Phase 5 — for now the download base is parameterized + the scripts are repo files);
clean-VM/reboot-survival test of the bootstrap on a truly fresh second machine (Go pieces + script syntax
verified locally; the launchd/systemd/schtask install path mirrors the proven agent installer but wasn't run
on a blank box this session).

**Phase 3 — Security hardening (single admin password ENFORCEMENT + Tailscale boundary): BUILT & VERIFIED
(this session). Decision recorded as D37.** Auth enforcement on top of the Phase-2 bcrypt hash.
- **`internal/hub/auth.go`** (new): session store (30-day TTL, in-memory, hourly cleanup), `lattice_session`
  cookie (HttpOnly + SameSite=Strict, **not Secure** — plain http over the tailnet), `authEnabled()`
  (hash != ""), `authed()` (constant-time **Bearer == enroll token** OR live cookie), `requireAuth`
  middleware, per-IP login rate-limiter (10 fails / 5 min → 429), `/api/auth/{status,login,logout}`.
- **Gating** (`http.go routes()`): GATED all `/api/*` except health/setup/auth + `/ws/dashboard|session|
  terminal` + `/editor/*` + `/preview/*`. OPEN: `/api/health` (watchdog liveness), setup, auth, `/dl`,
  `/install.*`, static. TOKEN-GATED untouched: `/ws/agent`, `/ws/tunnel` (now constant-time token compare).
- **Hardening:** WS `CheckOrigin` → same-origin (`checkSameOrigin`); raw token no longer logged (`tokenHint`);
  `/api/enroll` now admin-gated; generic errors on the login surface (authed API keeps the QA transparency
  toasts — now operator-only). **`lattice hub set-password`** subcommand lets a legacy hub opt into auth.
- **Frontend:** `Login.tsx` (full-screen single-step gate), App.tsx auth step (after setup, fetch
  `/api/auth/status`, fail OPEN), logout button in `SettingsPanel`.
- **Backward-compat (CRITICAL):** auth is enforced ONLY when a password hash exists. Dylan's legacy hub (no
  hash) → `authEnabled=false` → every gate passes through → **open exactly as today, no lockout**.
- **Verified — full gating matrix on a password-protected throwaway hub `:7413` + a no-password hub `:7414`
  (live `:7400` untouched):** OPEN surfaces (health/install.sh/auth-status/static) 200; GATED no-creds
  (fleet/sessions/devices/enroll/editor/preview) **401**; login wrong→401, right→200+`Set-Cookie`; gated with
  cookie→200, with Bearer→200, bad Bearer→401; logout→cookie revoked→401; **WS cross-origin→403,
  same-origin→101, no-creds→401**; `/ws/agent` reachable (400 bad-handshake, NOT 401), `/ws/tunnel` token-gated;
  rate-limit trips at the 11th attempt (429); **no-password hub `/api/fleet`→200 (no regression)**; token log
  shows hint only. Login page renders (Playwright `/tmp/lattice-login.png`). `go build/vet/test` + full build
  (dashboard + 5 targets) green.
- **NOT done in P3 (deferred to P4, noted in D37):** per-machine **revocable** enrollment tokens (roadmap
  lists under P3 but it's an enrollment redesign that belongs with the Manage-Mesh machine-removal UI). Also
  deferred/locked: multi-user accounts, internet-exposed TLS/auto-cert (Tailscale is the boundary).

**Phase 4 — "Manage Mesh" dashboard area: BUILT & VERIFIED (this session).** Promotes fleet management into a
first-class surface (behind the P3 auth gate) + lands the per-machine revocable tokens carried over from P3.
- **Per-machine revocable enrollment tokens** (`store.go` `enroll_tokens` table, `enrolltokens.go`,
  `hub.go tokenValid`): `GET/POST /api/enroll/tokens` (mint labeled token + returns the install one-liners),
  `POST /api/enroll/tokens/{token}/revoke`. Agent register + tunnel now accept **master token OR a non-revoked
  per-machine token**; a per-machine enroll is bound to its agentId + stamps last_used_at. **SAFETY (verified):
  the master token is ALWAYS valid + never revocable** — the whole live fleet enrolls with it, so this is purely
  additive. 16-hex per-machine tokens (vs 8-hex master) are visually distinct.
- **Agent rename + remove** (`store.go` `agent_labels` table, `http.go handleAgentSub`): `POST
  /api/agents/{id}/rename` (label overlays Name in `fleet()`/devices, survives re-register), `POST
  /api/agents/{id}/remove` (closes live conn, orphans sessions, deletes row + label, revokes its per-machine
  token). A removed box re-enrolling with the MASTER token reappears (documented, expected).
- **Syncthing capability** (`proto.Capabilities` + `agent/capabilities.go`): `syncthingInstalled` (binary
  resolve) + `syncthingRunning` (TCP dial 127.0.0.1:8384, 500ms). Flows through `Device.Capabilities`.
  (Tailscale + SSH were already detected hub-side in devices.go.)
- **Frontend `ManageMesh.tsx`** (opened from a gear in the Fleet rail header, `aria-label="Manage mesh"`):
  3 tabs — **Machines** (list/inline-rename/confirm-remove + revocable join-tokens; "Add agent" for
  reachable-only boxes), **Add machine** (label → mint join command → per-OS copy one-liners → live "waiting
  for {name} to join…" → "✓ joined"), **Integrations** (per-machine SSH/Syncthing/Tailscale detect+guide pills
  + "how to enable" expanders + the "sync your brain" Syncthing pairing walkthrough; explicit "Lattice never
  installs/configures these"). `useDevices` now exposes `refetch` so mutations reflect immediately.
- **Verified end-to-end (throwaway hub `:7416` + REAL agent processes, live `:7400` untouched):** mint
  per-machine token → real agent enrolls → token bound (agentId + lastUsedAt) → **revoke → agent restart
  REJECTED** ("registration rejected: invalid token"); **master token still enrolls (online:true) — the
  critical fleet regression PASSES**; rename → label in fleet/devices; remove → gone; Syncthing cap detected
  true/true on mini-ops. All 3 Manage Mesh tabs render with real fleet data (Playwright
  `/tmp/p4-{machines,add,integrations}.png` — 9 machines, tailscale IPs in green, detect+guide banner).
  `go build/vet/test` + full build (dashboard + 5 targets) green; gofmt clean.
- **NOT done in P4 (deferred):** the "add a brand-new PHYSICAL machine via the UI on fresh hardware" test
  (verified instead with a real agent process enrolling via a minted token — same code path); UI-driven
  rename/remove click-through (the APIs are Playwright-reachable + were curl-verified end-to-end).

**Phase 5 — Packaging, release channel & docs: BUILT & VERIFIED (this session). ✅ M3 MILESTONE COMPLETE.**
The final phase — makes the Phase-2 bootstrap one-liner actually resolve and the product shareable.
- **Product name FINALIZED as "Lattice"** (Dylan, 2026-06-04) — D9 closed. No rename churn (already the module
  path / dashboard / release-URL name). README/CHANGELOG ship branded.
- **`lattice update`** self-update (`internal/update/update.go`, top-level `main.go` dispatch): pure-Go,
  downloads `lattice-<os>-<arch>` from the release base (`--base` / `LATTICE_DOWNLOAD_BASE` / GitHub Releases
  latest), **verifies SHA256SUMS (aborts on mismatch, warn+proceed if absent)**, atomically replaces the running
  binary (unix rename / windows move-aside+rollback), and `--restart`s the detected service (launchd/systemd/
  schtask) or prints the exact restart command.
- **GitHub Releases pipeline** (`.github/workflows/release.yml`): a `v*` tag → `LATTICE_VERSION=<tag>
  build.sh` (stamps main.Version) → `sha256sum lattice-* > SHA256SUMS` → publishes the 5 binaries + `get.sh` +
  `get.ps1` + `SHA256SUMS` as assets via `softprops/action-gh-release@v2` (prerelease when the tag has a `-`).
  Asset names are exactly what `get.sh`/`get.ps1`/`lattice update` expect at `releases/latest/download/`.
- **Public docs:** `README.md` rewritten as a real quickstart (install hub one-liner → first-run wizard → add
  machines → optional Syncthing/Tailscale + features/security/update/commands/build); `CHANGELOG.md`
  (Keep-a-Changelog, framed as the **v0.1.0 candidate**).
- **Verified:** `lattice update` end-to-end against a local file server — valid checksum swaps the binary
  (1.0.0-old → 9.9.9-new), corrupt checksum **aborts with the binary unchanged + no temp litter**. `go
  build/vet/test` + full build (dashboard + 5 targets) green; gofmt clean. release.yml validated for
  structure/keys (YAML parses; mirrors ci.yml conventions) — **the actual GitHub Actions run is UNVERIFIED**
  (can't execute Actions locally; needs a real push + `v*` tag, which Dylan controls — nothing is pushed yet).

### ⭐ M3 "DISTRIBUTABLE" — ALL 6 PHASES SHIPPED + GONE LIVE (2026-06-04/05)
`c2df0ef`(P0) · `5a27655`(P1) · `3275e3b`(win-fix) · `bf420cd`(P2) · `8bde1e6`(P3) · `482a55b`(P4) ·
`d3a59fd`(P5) · `90884c6`(audit + tailnet scrub). Lattice is an installable, manageable, auth-gated,
self-updating self-hosted product.

**GO-LIVE EXECUTED this session:**
- ✅ **master PUSHED** to `origin` (`github.com/dylanstoryyy/lattice`, **PRIVATE**) — `f83e2ac..90884c6`.
- ✅ **Hub DEPLOYED** — live `:7400` rebuilt + `pm2 restart lattice-hub`; now `version: 90884c6` (was
  `c2df0ef`). Verified: fleet reconnected (studio/emu/mbp/mini-ops/pc online); `/api/setup/status`→
  `needsSetup:false` + `/api/auth/status`→`authRequired:false` (legacy config → no wizard, no lockout, dash
  stays open for Dylan exactly as before); `/api/enroll/tokens`→`[]` (P4 endpoint live).
- ✅ **Local mini-ops agent redeployed** (`pm2 restart lattice-agent`) → now reports `syncthing
  Installed/Running: true`. **Remote fleet agents NOT redeployed** (deferred — see below); they reconnect
  fine to the new hub (proto changes are additive) but still report `syncthing:false` until updated.
- ✅ **`v0.1.0` tag PUSHED** → `release.yml` ran **green** → GitHub Release **v0.1.0** published with all 8
  assets: 5 binaries + `get.sh` + `get.ps1` + `SHA256SUMS` (correct `hash␣␣name` format). **Roadmap P5
  criterion "a tag produces downloadable artifacts + checksums" — VERIFIED.**

**PUBLIC-READINESS PREP — DONE this session (2026-06-05), repo kept PRIVATE pending pressure-test:**
- ✅ **Master token ROTATED.** The old token (the literal previously echoed here) was the whole fleet's
  enroll token AND the post-D37 admin API Bearer. Generated a fresh `latt-`+24hex token → wrote
  `.lattice-token` → re-created the hub (`pm2 delete/start lattice-hub` so it re-read the file) → re-created
  the local mini-ops agent with it → **re-enrolled the entire fleet** (studio/mbp/emu via `install.sh`, pc via
  `install.ps1`, all over SSH with the new token — which also updated them from `c2df0ef` to `90884c6`).
  **Verified:** all 5 machines online on the new token + new binary; an agent presenting the OLD token is
  REJECTED ("invalid token"); stale dup records (`dylans-mac-studio.local`, `pc-windows`) removed via the P4
  remove endpoint. The new token lives only in `.lattice-token` (gitignored) — never commit it.
- ✅ **Old token SCRUBBED** from this doc + **purged from git history** (filter-repo `--replace-text`,
  force-pushed). Since it was rotated first, the historical copies are dead strings anyway. ⚠️ The history
  rewrite changed all commit SHAs — the phase SHAs cited above (`c2df0ef` etc.) are pre-rewrite; treat them as
  historical labels, not lookups.
- ⏳ **Repo still PRIVATE — NOT flipped public yet** (Dylan: pressure-test first). Until public, the
  `releases/latest/download/get.sh` one-liner + `lattice update`-vs-GitHub 404 for anyone without repo auth
  (they work with a `LATTICE_DOWNLOAD_BASE` override / against the hub's own `/install.sh`). **Before flipping
  public, still review `docs/FLEET.md`** (tailnet hostnames + MACs — not a credential, but personal infra).

**STILL OPEN (Dylan's call):**
1. **Flip the repo public** + run the **clean-machine install smoke** (the last unverified audit criterion) on
   a fresh box per the README — after pressure-testing.
   - **✅ PRE-PUBLIC EXPOSURE AUDIT DONE & CLEAN (2026-06-08).** Verified nothing sensitive ships when public:
     repo PRIVATE; `fleet.env`/`.lattice-token`/`lattice.db`/`docs/FLEET.md` all gitignored AND **never committed**
     (0 commits each — `FLEET.md` is explicitly `git check-ignore`'d); the **live token, any `latt-` token,
     `tail3c8bee`, the ntfy topic, `dylanstory`/`emulationstation`/`DESKTOP-2HF6TVF`, personal emails = 0
     occurrences in the tracked tree AND 0 across all 6 commits of history** (pickaxe `git log --all -S`). The
     only regex hits are false positives (a `100.64.0.1` CGNAT addr in `hub_test.go`; placeholder MACs in
     `metrics.go`/`wake.go`/`proto.go` comments). `fleet.env.example` is fully placeholdered w/ a security note;
     only `master` + tag `v0.1.0` go public; README has no personal leftovers. **Exposure-wise, safe to flip.**
   - **✅ SANDBOXED INSTALL SMOKE DONE — 21/21 PASS + caught & fixed a real install bug (2026-06-08, commit
     `1a1ec4c`, NOT pushed).** Ran the REAL published v0.1.0 `get.sh` against the REAL published release assets
     (`gh release download v0.1.0`, served locally) in an isolated `HOME` + throwaway port, with `launchctl`
     PATH-shimmed so it never touched the live launchd session or the live :7400 hub. Verified end-to-end:
     download → **SHA256 checksum gate** → binary installed (byte-identical to the asset) → `hub init`
     (token/port/config; **correctly avoided the live :7400**, picked a free port) → generated launchd plist is
     valid (`plutil -lint`) and points at the sandbox bin → boot hub → first-run wizard flow (needsSetup:true →
     check-root → short-pw 400 → valid setup 200 + **bcrypt hash written** → needsSetup flips false → re-setup
     409). **🔴 BUG FOUND & FIXED:** the hub's `--db` flag defaulted to a **cwd-relative** `"lattice.db"`
     ([hub.go:129](../internal/hub/hub.go)); the get.sh services run `lattice hub` with no `--db` and an
     unpredictable cwd (launchd→`/`, systemd→`$HOME`, nohup→curl|sh's dir) → on a fresh box the db would land at
     `/lattice.db` (perm-denied → crash-loop) instead of the data dir. Fixed: default to
     `filepath.Join(configDir(),"lattice.db")` = `~/.lattice/lattice.db`. **Proven:** a fresh hub from `cd /`
     now reports `agents:0` (isolated) + writes `~/.lattice/lattice.db`, no stray `/lattice.db`. `go
     build/vet/test` green. The live pm2 hub passes `--db` explicitly → unaffected, no redeploy needed.
   - **✅ v0.1.1 RELEASED with the fix (2026-06-07).** Pushed `1a1ec4c` + a CHANGELOG commit (`5f9ab57`, master
     even with origin), tagged + pushed **`v0.1.1`** → `release.yml` ran **green** (4m9s) → GitHub Release
     **v0.1.1** published with all **8 assets** (5 bins + get.sh + get.ps1 + SHA256SUMS), `prerelease:false` so
     **`releases/latest` now resolves to v0.1.1**. **Verified the PUBLISHED artifact carries the fix:**
     downloaded the v0.1.1 `lattice-darwin-arm64` (SHA256SUMS check OK), version stamps `lattice v0.1.1`, and
     booted from `cd /` it reports `agents:0` + writes `~/.lattice/lattice.db` (no stray `/lattice.db`). So a
     real `curl get.sh | sh` / `lattice update` now pulls a hub that survives a fresh install. ✅ **CI maintenance DONE
     (2026-06-07, commit `734e9f8`, PUSHED):** bumped `softprops/action-gh-release` v2.6.2→**v3.0.0** (SHA
     `b430933…`), off the Node-20 runtime GitHub force-deprecates 2026-06-16. v3.0.0 is a pure Node 20→24
     runtime move (release notes), no input/behavior change, so the release job is untouched; SHA-pinned per repo
     convention. Not yet exercised (no release cut since) — the next real release tag will run it.
   - **Remaining gate:** the TRUE clean-OS test (loading the launchd/systemd service on BLANK hardware + reboot
     survival) still needs a FRESH box (no spare in the fleet — Dylan to provide/decide). The install-breaking
     db footgun is gone and the bootstrap is now smoke-verified against the real release. The public flip itself
     is Dylan's call (exposure audit says safe).
2. ~~**Auth is OFF** (no password set)~~ — **RESOLVED (confirmed 2026-06-07).** A password HAS been set:
   `~/.lattice/config.json` carries a 60-char bcrypt `adminPasswordHash`, `/api/auth/status`→`authRequired:true`,
   and gated endpoints (`/api/fleet`) correctly 401 without a session. The dashboard is login-gated on the
   tailnet. (Earlier handoffs said "passwordless / run set-password" — that's stale; set-password was run since.)

**Health anomaly noticed (unrelated to M3, flagging):** `pm2 lattice-watchdog` shows `restart_time=17951` —
far above the ~177 from the bash-3.2 crash-loop the audit fixed. It reads `online` now, but that counter
suggests it may have regressed into looping again at some point. Worth a look (not touched this session).

---

### POST-QA FIX EFFORT — ALL 18 FINDINGS SHIPPED, COMMITTED & PUSHED (2026-06-04) ✅
6-session fix plan for the 18 QA findings (`docs/FIX-PLAN.md` + `docs/QA-FINDINGS-2026-06-03.md`) is
**COMPLETE.** S1 (dock remote paths, F17) · S2 (claude placement hardening + session reaping, F14/F18) ·
S3 (★ session transcripts/history, F16/F15) · S4 (editor de-chrome, F12) · S5 (fleet/nav polish,
F1/F2/F3/F5/F6/F11) · S6 (wizard/settings UX, F7/F8/F9/F10). Every finding is 🟢 fixed or a documented
by-design note. **All changes are COMMITTED on master and PUSHED; HEAD = `10d6b4a`.** The full per-session
handoffs (what changed, how each was verified) live at the bottom of `docs/FIX-PLAN.md`. The QA fix batch
itself landed as `8c6cb63`; commits after it are the final-audit + UX follow-ups below.

**Post-batch follow-ups (final audit + Dylan UX, all on master, all live):**
- `8c6cb63` — the 18-finding QA fix batch (S1–S6).
- `e973277` — final-audit pass: closed the project-name-click side note (verified project-scoped by
  inspection) and corrected the stale F4 watchdog note.
- `312083d` — **F4 hub-side Windows recovery (Dylan's call):** `DESKTOP-2HF6TVF` mapped to alias `pc` +
  os_family `windows` and added to the watchdog `WATCH`, so the hub SSHes `schtasks /run /tn LatticeAgent`
  when its agent dies. Tradeoff: a sleeping Windows box yields one down + one recovered ntfy ping per
  sleep/wake cycle (bounded by RECOVER_TRIES; drop from `LATTICE_WD_WATCH` if noisy).
- `ec18881` — **cache-control fix:** the static handler sent NO `Cache-Control`, so browsers heuristically
  cached a stale `index.html` → users stayed frozen on an old bundle after a rebuild (this is why a fixed
  control still looked broken). Now `index.html` + unhashed root files = `no-cache` (always revalidate);
  `assets/*` (Vite content-hashed) = `immutable`. Future rebuilds appear on a normal refresh.
- `10d6b4a` — **Fleet/Workspace pill is now ONE flip-switch (Dylan):** a click anywhere in the pill toggles
  to the other view (even the already-active half). Single `<button role="switch">`; labels are
  presentational. (Only the rounded-corner pixels, outside the pill, don't flip — expected.)

**Live-confirmed (2026-06-04):** `/api/health` → `version: 10d6b4a` (clean); all three lattice pm2 services
online, 0 unstable restarts. **Whole fleet on `10d6b4a`:** studio / emu / pc(DESKTOP-2HF6TVF) / mini-ops AND
**mbp** — mbp woke up and was finally fleet-synced, so the long-standing version skew is GONE. Only the
Galaxy S26 is offline (no agent — reachable-only, by design).

**F2 decision (Dylan, 2026-06-04):** "N woven" counts only machines whose agent is **live right now**
(`isWoven = hasAgent && online && status !== 'reachable'`) — a box with a dead/installed-but-down agent does
NOT count as woven. Shipped as-is; no change.

**Known follow-ups (non-blocking):**
- The enrollment token is echoed in this tracked `STATE.md` (`.lattice-token` is gitignored, but the value
  is in the doc + git history). Network-gated, low risk; rotate + scrub if it ever leaves the private repo.
- Windows-under-schtasks claude auth + transcript fetch from the pc agent remain UNTESTED (no claude ran
  there) — possible future finding, called out in the S2/S3 handoffs.
- Browsers that loaded the dashboard BEFORE `ec18881` still hold the old no-cache-less `index.html`; they
  need ONE hard refresh (Cmd+Shift+R) to adopt the fix, then auto-update forever.

**Windows agent no longer pops a console window (2026-06-04, Dylan):** the `LatticeAgent` schtask ran
`lattice.exe` (console-subsystem Go binary) under `LogonType=Interactive`, so every logon/boot launch AND
every watchdog `schtasks /run` on sleep/wake flashed a focus-stealing console window on the desktop.
Fix: launch via a hidden `wscript` wrapper (`%LOCALAPPDATA%\Lattice\run-agent-hidden.vbs`, GUI-subsystem
host → `sh.Run(cmd, 0, True)` = SW_HIDE + wait, so the task still tracks the process and RestartCount /
MultipleInstances=IgnoreNew are unchanged). **Run context is untouched** (still interactive user, NOT
session-0/S4U) so the D35 interactive-claude/PTY + claude-auth assumptions still hold.
- `internal/hub/installtmpl/install.ps1.tmpl` now generates the wrapper at enroll time and points the
  task action at `wscript.exe "<vbs>"` — every future enrollment is windowless from the start.
- `pc` (DESKTOP-2HF6TVF) was fixed live this session (task repointed + agent restarted hidden;
  verified `MainWindowHandle=0`). One-shot reusable fixer: `scripts/windows/hide-agent-window.ps1`.
- ⚠️ This is a working-tree edit on the Windows node; per the no-git-sync fleet model it must be
  COMMITTED on the canonical git node (mini-ops) — Syncthing propagated the files, not the commit.
- Aside: UAC over Parsec on `pc` is unreliable (secure-desktop prompts don't render to the remote
  session), so the schtask edit had to be run from a local elevated shell. Relevant for any future
  remote admin on that box.

### AUDIT + ops/backend hardening SHIPPED & VERIFIED (2026-06-03)
Full audit of the whole system → `docs/AUDIT-AND-TEST-PLAN.md` (also carries a complete,
trace-free manual test plan covering every feature/button). Fixed everything fixable this session:
- **🔴 FIXED — the fleet watchdog was DEAD.** `scripts/fleet-watchdog.sh` used `declare -A`
  (bash 4+) but runs under macOS **bash 3.2.57** (no homebrew bash on mini-ops) → it crash-looped
  **177×**, exhausted PM2's restart cap (status `waiting`), spewed **~2.7 MB/day** of error spam, and
  delivered ZERO auto-recovery. The D33 "always-on layer 3" was fiction. **Rewrote it bash-3.2-safe**
  (file-based per-machine state under `$TMPDIR/lattice-watchdog`, no associative arrays), added a
  **WATCH allowlist** (`studio/Dylans-Mac-Studio.local/mbp/emu`; override via `LATTICE_WD_WATCH`) so it
  stops storming the always-asleep Windows box (`DESKTOP-2HF6TVF`), and guarded the `set -u` crash.
  **Verified:** `pm2 restart lattice-watchdog`, now 3 min+ uptime with the restart counter FROZEN,
  `watchdog.err.log` empty, clean sweeps, correct mbp recovery attempts + one legit ntfy alert, and it
  gracefully skipped a sweep during the hub restart instead of dying. `pm2 save`d.
- **🟠 mbp version skew (UNRESOLVED — unreachable).** mbp flaps connect/disconnect every ~20 s on a
  stale binary (`v=8642fea` vs studio `v=bb64e7e`) and is **SSH-unreachable right now** (timeout), so it
  can't be synced this session. This churn is what fills the hub log (the hub itself is healthy — the
  `↺ 27` is manual redeploys, NOT crashes; `hub.err.log` shows no panics). **Follow-up: when mbp is
  awake, `bash scripts/fleet-sync.sh`.**
- **Backend hardening (build/vet/test green, hub rebuilt + restarted):** added WS-safe
  `ReadHeaderTimeout: 15s` to the `http.Server` (Slowloris mitigation that does NOT sever the long-lived
  WebSockets/proxies); added a `405` method guard to `handlePlacement` (verified GET→405, POST→200);
  removed the dead `proto.TypeCapabilities` constant (nothing sent/handled it).
- **Cleared ~10 MB of stale rotated logs** from the repo root; truncated the watchdog spam logs.
- **Left intentionally:** `audit_log` table (kept for recoverability per `bb64e7e`); the Google-Fonts
  CDN links in `dashboard/index.html` (local fonts are Hanken Grotesk + IBM Plex Mono — DIFFERENT
  families from the CDN's Space Grotesk + JetBrains Mono; removing blind risks a visual regression —
  needs a screenshot-verified frontend pass). Both noted in the audit doc.

### New-session project PICKER SHIPPED & VERIFIED (2026-06-02)
- `NewSessionDialog` (project flow) now carries a **project selector** over every folder the hub scans
  under the AI-Hub projects root (`/api/projects`). The project target became `project: Project | null`:
  launched from the sidebar it pre-fills (and a **CHANGE** button retargets); launched cold it opens on a
  searchable list (name + path, filtered by substring). Session type / name / placement stay hidden until a
  project is chosen; Create reads "Pick a project" until then.
- **Entry points for a cold (project-less) new session:** ⌘K palette → **"New session…"** (Actions group,
  sparkles), and a **New session** button on the workspace empty state. Both set
  `newTarget={kind:'project', project:null}`. Both dialog mounts (App.tsx + Workspace.tsx) now receive
  `projects` + `projectsState`.
- **Scope toggle** — when the dialog is opened on a DEVICE (Fleet → New session), it now shows a segmented
  **Project ⇄ <device>** switch so you can flip to the project picker without reopening; default stays the
  device. Launched from a project there's no specific device to pin, so the toggle isn't offered.
  (`ScopeTabs` in `NewSessionDialog.tsx`, `.scope-tabs` in `app.css`.)
- Verified via Playwright against a live hub: palette action → picker lists all ~25 AI-Hub projects, search
  filters, selecting reveals type/name/placement with a working CHANGE; the device dialog's Project tab
  reveals the same picker. Files: `NewSessionDialog.tsx`, `CommandPalette.tsx`, `App.tsx`, `Workspace.tsx`,
  `app.css` (`.proj-pick-*`, `.scope-tabs`). `tsc` + `npm run build` clean.

### Right-side DOCK (Claude-Code-desktop-style) + state persistence SHIPPED & VERIFIED (2026-06-02)
- **🆕 Right dock** (`dashboard/src/components/workspace/DockPanel.tsx`): a togglable panel beside the
  active session, comfortable for anyone from Claude Code desktop. Toggle lives in the TabStrip (panel
  glyph, top-right); resizable left edge (`.dock-resize`, clamped); contextual to the active session's
  machine + projectPath. Four views, all reusing existing infra (no backend/agent changes):
  - **Files** — `/api/agents/{id}/files` tree, breadcrumb nav, folders-first, click file = download.
    Starts at the session's project dir (empty path → agent home fallback; `// go to home` on error).
  - **Terminal** — ad-hoc PTY via `/ws/terminal?agent=`, auto-`cd`'d into the project. Reuses a
    generalized `XtermSession` (new `makeUrl`/`initialInput`/`bare` props; the `/ws/terminal` and
    `/ws/session` frame protocols are identical so one component drives both).
  - **Git** — the same PTY auto-running `git --no-pager -c color.ui=always status` + `diff --stat` in the
    project (colored, interactive for more git commands).
  - **Preview** — 🆕 **tunneled** (D36, 2026-06-02): `<machine>:` label + port field → iframe at
    `/preview/{agentId}/{port}/`; the hub relays to `127.0.0.1:{port}` on the machine over the editor's
    yamux tunnel (D27). Works for any localhost dev server, any device that reaches the hub — no LAN /
    `0.0.0.0`. Shipped **strip-mode**: verified for plain/relative-asset servers; default **Vite/Next**
    don't render yet (root-absolute assets + base→redirect-loop) — fix = no-strip framework mode, deferred
    to a real-device/HMR session. See `docs/NEXT-tunneled-preview.md` + D36.
    Backend: `internal/hub/previewproxy.go`, `internal/tunnel/handshake.go` (+`_test`), `agent/tunnel.go`.
  Verified live: opened a project terminal, toggled the dock → Files listed the lattice tree (34 entries),
  Git showed this session's real modified/untracked files, Terminal connected, Preview shell rendered;
  dock open-state + active-view + width all persisted across refresh. Files: `DockPanel.tsx`,
  `XtermSession.tsx` (generalized), `Workspace.tsx` (layout + toggle + resize), `app.css` (`.dock-*`).
- **🆕 UI state persists across refresh** (`usePersisted.ts`, a localStorage-backed useState): view
  (Fleet/Workspace — no more always-bouncing-to-Fleet), open session tabs + active tab (which **reattach
  live** — sessions survive server-side, the UI reconnects), selected fleet machine, sidebar mode,
  collapsed rail, expanded projects, filter facets, and the dock state. Guarded the tab-prune effect on
  `sessionsState==='ready'` so restored tabs aren't wiped during the initial empty-sessions load.
- **Header sizing fix:** the top bar had collapsed to 35px (flex hugged the controls) so the seg/search
  were crammed edge-to-edge. Pinned `.topbar` to 52px + a shared `--tb-ctl: 34px` height for seg/search/
  gear → all aligned with breathing room.

### UI feedback pass + ROOT-CAUSE token fix SHIPPED & VERIFIED on the live hub (2026-06-02)
Dylan reviewed the chrome and flagged: settings/search hard to read, buttons not visible, ugly line
spacing, wants Claude-Code-desktop-style session filters. Root-caused and fixed all of it:
- **🔑 ROOT CAUSE — the design tokens were never loaded.** `src/design/colors_and_type.css` (defines
  EVERY `var(--*)` token + the local webfonts) was **imported nowhere** — `main.tsx` only pulled
  `index.css` + `app.css`. So every `background: var(--raised)` / `var(--green)` silently resolved to
  empty → **transparent** modals, palette, buttons, chips, metric-bar fills, borders, glows. The app only
  looked themed because of canvas colors + Tailwind classes. **Fix: one import in `main.tsx`** (before
  app.css). This alone fixed Dylan's #1/#2/#4 (settings + palette backgrounds now solid `#171D25`, the
  green "New session" button and all filled buttons now render). Verified the tokens resolve at runtime
  and `.btn-run` bg = `rgb(47,217,138)`. (The "Google-Fonts-CDN" note in the old deferred list is stale —
  fonts are local woff2 in `src/design/fonts/`.)
- **Scrims darkened** (`.scrim` 60%→82% + blur 8px, `.cmdk-scrim` 58%→80%) so the busy mesh no longer
  bleeds behind modals/palette — the readability complaint.
- **Line spacing** — reworked the Workspace empty state (3 evenly-spaced lines, dropped the cramped
  `<br/>` cram, added a "press ⌘K to jump anywhere" hint) and the Settings labels (title + subtitle
  stacked via `.set-section` instead of the inline-wrapping `.flabel`).
- **🆕 Claude-Code-desktop session filters** (`Sidebar.tsx`): a facet menu off the Projects header —
  **Group by** (Project / Machine / Kind), **Sort by** (Recent / Name), **Status** (All / Live / Detached /
  Orphaned), **Kind** (All / Claude / Terminal / Editor) + Reset. Project mode keeps the rich tree
  (now sorted, hides empty projects when a session facet is active); Machine/Kind modes render flat
  grouped session lists (`GroupedSessions`). Active facets light a teal dot on the button. Verified with 2
  live terminal sessions: Group-by-Machine → "MINI-OPS (1)"/"STUDIO (1)", Group-by-Kind+Live →
  "TERMINAL (2)", Reset clears. Files: `Sidebar.tsx` (FilterMenu/FilterRow/GroupedSessions + state),
  `SettingsPanel.tsx`, `main.tsx`, `Workspace.tsx` (empty state), `app.css` (`.prj-*`, `.set-section`).

### Transparency + scaling pass SHIPPED & VERIFIED on the live hub (2026-06-02)
Second pass same day (transparency / scaling / UI), all Playwright-verified on `:7400`, hub rebuilt +
restarted, zero console errors:
- **Transparency — no more silent failures.** Every user-initiated action that used to swallow its error
  (`catch {}`) now surfaces a real toast with the hub's actual message: start Claude session, open device
  editor, switch machine, reconnect orphan, archive/trash/restore/delete/empty-Trash, and project-create
  warnings. New `errMsg()` in `Workspace.tsx` strips the `${status}:` prefix and unwraps `{error}` JSON.
  Plumbed via a stable `onNotify` prop (App's `flash()` now takes a `'error'` kind → red toast `.toast.err`).
  Verified by forcing `POST /api/sessions` → 500 in Playwright: clicking a project `+` showed
  "Couldn't start lattice — no eligible machine". (Was: nothing happened at all.)
- **Transparency — hub-unreachable banner.** When the dashboard WS drops (`conn === 'down'`), a slim red
  `.conn-banner` ("Hub unreachable — reconnecting… · showing last-known state") drops under the top bar so
  stale data is never mistaken for live. Verified by killing the WS via `routeWebSocket`.
- **Scaling — code-split the bundle.** The Workspace tree (xterm.js + marked + dompurify = the bulk) is now
  `React.lazy`-loaded behind a `<Suspense>` (fallback: "weaving the workspace…"). Default Fleet view no
  longer pulls any of it. **Initial JS 594 kB → 210 kB (gzip 162 kB → 64 kB, ~60% lighter)**; the >500 kB
  build warning is gone. Verified on `:7400`: Fleet loads only `index-*.js`; switching to Workspace
  lazy-fetches `Workspace-*.js` (200) on demand. Matters for phone-over-tailnet (D7/D15).
- Files: `App.tsx` (lazy/Suspense, toast kind, conn banner), `Workspace.tsx` (onNotify + errMsg + 8
  surfaced catches), `design/app.css` (`.toast.err`, `.conn-banner`). Embedded files 5 → 7 (split chunks).

### UX chrome + cross-view nav SHIPPED & VERIFIED on the live hub (2026-06-02)
Functionality/UX/polish pass on the dashboard — all verified via Playwright on the embedded hub (`:7400`),
zero console errors, hub rebuilt + `pm2 restart lattice-hub` (version `576eaf9-dirty`):
- **⌘K command palette** (`dashboard/src/components/CommandPalette.tsx`): the top-bar "Search the mesh"
  box + ⌘K/Ctrl-K now open a real palette — jump to a view, an active session, a project (reuse-or-spawn
  a Claude session), focus/wake a machine, new-project wizard, or settings. AND-substring filter,
  full arrow/enter/esc keyboard nav. App owns the data; routes into Workspace via a nonce'd **intent bus**
  (`WorkspaceIntent` in `Workspace.tsx`) since Workspace owns its own tab state. The intent effect gates on
  `sessionsState/projectsState === 'ready'` (Workspace mounts fresh when crossing from Fleet → its lists
  arrive async; consuming against an empty list was the one real bug found + fixed).
- **Settings panel** (`dashboard/src/components/SettingsPanel.tsx`): the previously-dead gear now opens a
  modal — **primary coding machine** picker (D32, persisted, drives placement) + honest About (version,
  fleet size, hub URL). Fixed a latent API bug: `saveSettings` posted `{primaryAgent}` but the hub wants
  `{key,value}` — replaced with `setPrimaryAgent()` (POST `{key:'primary_agent',value}`); verified the
  save round-trips (studio → mini-ops → restored).
- **Fleet → Workspace continuity:** Fleet "Recent" rows + side-panel session arrows now open the *specific*
  project/session (via the same intent bus) instead of dumping you in a generic Workspace.
- **Workspace polish:** collapsed 48px rail now shows clickable status dots for active sessions (collapsing
  no longer strands your work); project filter got a clear (×) button + Esc-to-clear.
- New CSS lives in `dashboard/src/design/app.css` (`.cmdk-*`, `.set-*`, `.rail-mini-*`). `tsc` clean.
  NOT yet committed (working tree dirty) and NOT fleet-synced beyond the mini-ops hub rebuild — frontend
  only, so no agent redeploy needed; other dist binaries rebuilt for version consistency.

**D35 + UX hardening SHIPPED & VERIFIED on the fleet (2026-06-01).** Branch `feat/d35-no-headless-claude`
(through `5ef0c5c`), fleet-synced to studio/emu/mini-ops/pc (mbp was asleep — needs a sync when awake; it
has no claude so it can't host claude sessions anyway). What landed tonight, all verified live on studio:
- **D35:** headless `claude -p` purged everywhere; Claude tab = interactive `claude` in a PTY (xterm).
- **Sidebar = Claude-desktop model:** clicking a project only expands it; a `+` by the name starts ONE
  Claude session; empty projects show nothing. New project sessions DEFAULT to the **Studio** (the
  `primary_agent` setting was stale — pointed at `dylans-mac-studio.local-darwin`; fixed to `studio-darwin`).
- **Machine chip = clean switcher** (no placement scores). Switching relocates by creating fresh on the new
  box and **trashing** (recoverable, ends process) the old. Sessions never migrate on their own (D32).
- **Archive now ends the host process** (was leaking — only delete did). `endHostProcess()` shared by both.
- **One view per session** — dropped the duplicate claude/terminal tab pair (it was two sockets to one
  process, so keystrokes went to the wrong tab).
- **Resume fixed (was 100% broken):** `claude --session-id X --resume X` is rejected by the CLI; on resume
  we now pass ONLY `--resume`. Verified: kill → resume → full conversation replays.
- **PATH fixed:** claude launches via a login shell + an explicit tool-dir prepend (`/opt/homebrew/bin` etc.),
  so `node` and SessionStart hooks resolve. Verified: node v26, zero "command not found" hook errors.

Follow-up cleanup (2026-06-02, `bb64e7e`): the three deferred items are now done — removed the dead audit
pipeline (endpoint/handlers/store methods/frontend; `audit_log` table kept for recoverability); removed the
inert `onOpenEditor`/`editorAvailable` project-editor plumbing from Sidebar (device editors + EditorPane
untouched) plus dead `globalApproval`/`perMachineApproval` Settings fields; replaced the onboarding seed's
3s sleep with an **agent-side readiness check** — `SessionCreatePayload.SeedInput` is typed in once the PTY
output settles (first output + 700ms quiet, 15s ceiling). All deployed to studio/emu/mini-ops/pc (mbp asleep).

**Phases 1, 2, 3 (Workspace) & 4 COMPLETE & verified on the real fleet (2026-05-29).** The SUCCESS
CRITERION (packageable) is MET and the workspace is built + verified (D15–D25).

**IDE Milestone (M2) — P1 BUILT & VERIFIED END-TO-END on mini-ops (2026-05-30).** The embedded editor
works: real VS Code (code-server 4.112.0) renders inside Lattice through the hub, proxied over the
yamux tunnel (D27), with file tree + git + extension-host WebSocket all live. Every DONE-WHEN met
(see "### P1 — DONE & VERIFIED" below). **NEXT: P2** (Monaco-rail retirement D31 in the workspace UI +
multi-machine code-server install/verify on studio/mbp/pc + the Cursor-grade AI chrome D29).

**IDE Milestone (M2) — architecture DISCUSSED & DECIDED with Dylan (2026-05-29).**
Turn the workspace into a real IDE (compete with Cursor / VS Code / Claude Code desktop / Codex /
T3 Code): deep editor abilities + a Cursor-grade AI experience, all over the mesh, as a distributable
product. **Decisions D26–D31 recorded** (docs/DECISIONS.md); roadmap **P1–P4** in docs/ROADMAP.md;
full spec/plan at `~/.claude/plans/rippling-wishing-candy.md`.

### IDE architecture (ratified D26–D31)
- **D26** EMBED **code-server** as the editor core; do NOT fork VS Code (supersedes D16). Keep the
  Claude chat + sessions + placement + onboarding as OUR chrome around it.
- **D27** Expose the per-agent editor via a **second dial-out WS tunnel multiplexed with yamux** (hub
  reverse-proxies `/editor/{sessionId}/*` → `yamux.OpenStream` → agent → local code-server) —
  preserves D2 (zero inbound on leaves). **Biggest technical piece; build/spike first.**
- **D28** Distribute code-server via **hub-as-distribution** (extends D14); a new on-demand **`editor`**
  session kind reusing the D18 lifecycle + D19 placement; fetched+cached on first use, torn down with
  the session.
- **D29** AI **chrome-first** (reuse the Phase-3 Claude runner beside the editor) → a **Lattice VS Code
  extension** (Cmd-K, autocomplete, in-workbench chat) later.
- **D30** Editor on **all four machines**; Windows runs **code-server inside WSL2** (`/mnt/c`).
- **D31** The embedded VS Code **replaces the read-only Monaco rail**; the sidebar drives it.
- **Mobile / same-URL (D7/D15):** every layer is reachable from a phone browser at the SAME hub URL
  (`http://mini-ops.tail3c8bee.ts.net:7400`) over the tailnet — no app install, Tailscale is the only
  prerequisite. The Lattice chrome (fleet, Claude chat, terminal, sessions, file nav) is already
  mobile-responsive (drawer sidebar + `md:` breakpoints). The embedded editor (D27) renders through the
  hub too (phone → hub only, never direct to the agent → works on CGNAT/mobile data), BUT code-server's
  workbench is desktop-grade — usable on a phone, not optimized. Realistic split: phone = chat/terminal/
  quick peeks; laptop = heavy editing. A mobile-friendly editor affordance is a P3/P4 nice-to-have.

### P1 spike — code-server subpath proxy (2026-05-30): ✅ PASS (visually verified)
**code-server 4.112.0** (Code 1.112.0) on mini-ops. Stood up a stdlib Go reverse proxy
(`/tmp/cs-spike/proxy.go`, v3 ~180 lines) mapping `/editor/test/*` → 127.0.0.1:9444. **Playwright
loaded the full VS Code workbench through the subpath** — file tree showed `hello.js`, extension host
connected, WS stayed alive, no blocking console errors. Screenshot `/tmp/cs-spike-shot.png` (101 KB,
1280×720, valid PNG — I viewed it). code-server emits **relative** asset URLs, so a trailing slash
makes everything resolve under the prefix.

**Verified recipe the hub MUST implement** (more than the trailing-slash redirect — two extra pieces):
1. **Director:** strip the `/editor/{id}` prefix; set `X-Forwarded-Host`=hub host, `X-Forwarded-Proto`,
   `X-Forwarded-Prefix=/editor/{id}/`; set the upstream `Host` to code-server.
2. **302 redirect** `/editor/{id}` → `/editor/{id}/` (trailing slash MANDATORY).
3. **ModifyResponse:** rewrite `Location` headers — code-server 302s with a RELATIVE `./?folder=…`;
   resolve it against the prefix so the browser stays under `/editor/{id}/` (else infinite redirect loop).
4. **WebSocket tunnel:** `httputil.ReverseProxy` strips hop-by-hop `Upgrade`/`Connection`, so the
   extension-host WS never upgrades. Detect `Upgrade: websocket` → bypass ReverseProxy → `net.Dial` the
   backend + `http.Hijack` the client + bidirectional `io.Copy`.
5. **code-server flag `--trusted-origins`** = the hub host (use `*` for dev) — its `authenticateOrigin()`
   compares the browser `Origin` to the backend `Host` and **403s the WS** without this. Plus `--auth
   none` (tailnet + hub already gate access, D2/D3) + `--disable-telemetry`.
Harmless non-blockers: a 404 on optional `vsda.js` (code-signing WASM, absent in this build) + an
ERR_ABORTED on the webview iframe (browser sandbox) — workbench fully functional.

**Footprint / D28 note to settle in P1:** the brew cellar is ~384 MB (full VS Code + Node runtime). The
standalone release tarball is ~95 MB compressed — viable for hub-as-distribution (D28), but the simpler
alternative is **per-node install** (brew / official `install.sh`) with the hub just spawning+proxying.
Decide in P1: hub-served tarball (keeps D28's "no per-machine install") vs per-node install (simpler,
heavier per box). The WSL2 Windows path (D30) likely wants the Linux tarball inside WSL regardless.

Reference impl + evidence: `/tmp/cs-spike/proxy.go`, `/tmp/cs-spike-shot.png`, `/tmp/cs-server.log`.

### P1 — DONE & VERIFIED END-TO-END on mini-ops (2026-05-30) ✅
The embedded editor is built and proven on the real fleet. **D28 distribution decision (settled):
per-node install** for P1 — the agent detects an existing `code-server` (`resolveCodeServer`: PATH →
brew/`/usr/local` → `~/.local/bin`) and just spawns+proxies it; placement excludes machines without it.
Hub-served tarball stays a clean future add (the resolver is the only seam that'd grow a fetch path).

**Architecture as built (Arch-1: hub terminates HTTP, agent is a dumb per-session pipe):**
- **Shared** `internal/tunnel/` — `WSConn` (gorilla `*websocket.Conn` → `net.Conn`, binary frames),
  `Config()` (yamux keepalive 30s / write-timeout 10s), and the stream handshake (`WriteStreamHeader`/
  `ReadStreamHeader`: the hub writes `sessionId\n` as the first bytes of every stream so the agent
  routes it). yamux = `github.com/hashicorp/yamux v0.1.2`.
- **Hub** — `tunnel.go` (`/ws/tunnel?token&agent`: token-gated, `yamux.Server`, per-agent session in a
  registry that closes a stale prior session on reconnect). `editorproxy.go` (`/editor/{sessionId}/*`:
  the proven spike recipe verbatim — prefix strip + `X-Forwarded-*`, relative-`Location` rewrite, the
  trailing-slash 302, manual WS hijack — but `Transport.DialContext` opens a yamux stream + writes the
  handshake instead of a TCP dial). Routes in http.go; `kind=editor` accepted in sessionapi.go +
  `code-server not installed` exclusion in placement.go.
- **Agent** — `editor.go` (`editorSessions` registry mirroring claude/terminal: spawns code-server on a
  free `127.0.0.1:PORT`, per-session `--user-data-dir` + shared `--extensions-dir`, waits for the port,
  supervises, tears down on close). `codeserver.go` (resolver + WSL probe). `tunnel.go` (2nd dial-out to
  `/ws/tunnel` with its own reconnect/backoff; `yamux.Client`; Accept loop reads the handshake →
  `addrFor(sessionId)` → `net.Dial` loopback → bidirectional `io.Copy`). `state.go` 3rd registry +
  `setSink`; `capabilities.go` probes code-server/WSL; `SessionEditor` cases in agent.go.
- **code-server argv:** `--bind-addr 127.0.0.1:PORT --auth none --trusted-origins * --disable-telemetry
  --disable-update-check --disable-workspace-trust --user-data-dir <base>/<sid> [--extensions-dir
  <shared>] <cwd>`.
- **Frontend** — `SessionEditor.tsx` (iframe `src=/editor/{id}/`, loading overlay), `editor` tab in
  SessionPane (TAB_ORDER), `editor` kind in NewSessionDialog (gated on `codeServerInstalled` for device
  sessions), types.ts `SessionKind`+`Capabilities`. *(Monaco rail NOT yet retired — that's P2/D31.)*
- **Proto** — `SessionEditor` kind + `Capabilities.CodeServerInstalled/Version` + `WSLAvailable`.

**DONE-WHEN — all verified (real browser via Playwright + lsof + on-disk):**
1. ✅ open a project on mini-ops → full VS Code workbench renders through the hub (screenshot: lattice
   file tree, `master*`, 21-change SCM badge). 2. ✅ edit+save → marker written to disk via the editor.
   3. ✅ git works (M markers / SCM badge); search panel present. 4. ✅ **no new inbound listener on the
   agent** — agent has ZERO listening sockets (`lsof -a -p AGENT -iTCP -sTCP:LISTEN` empty), only two
   outbound loopback conns to :7400 (`/ws/agent`+`/ws/tunnel`); code-server bound `127.0.0.1` only.
   5. ✅ **hub restart re-adopts** — session stays `live`, tunnel re-establishes, `/editor/{id}/` serves
   200, SAME code-server pid survives. Editor close tears code-server down cleanly.

### P2 — IN PROGRESS
- ✅ **D29 AI chrome — Claude beside the editor (Cursor-style), 2026-05-30.** An editor session now
  renders a resizable split: VS Code (iframe) on the left, the project's Claude chat on the right, with
  a draggable divider + an `AI` collapse toggle (split open by default). The Workspace auto
  create-or-reuses ONE Claude session per project **pinned to the editor's machine** (so the AI shares
  the editor's filesystem); a `· ai` suffix marks it. Machines without claude (mbp) show "claude isn't
  installed on <machine> — editor only" and just get the editor. Reuses the Phase-3 stream-json runner
  (D17, Max subscription) untouched. Verified on mini-ops via Playwright: open editor → paired Claude
  auto-spawns on the same agent → sent a prompt → assistant replied in the right pane. (SessionPane
  split + EditorPane in `SessionPane.tsx`; pairing effect in `Workspace.tsx`.)
- ✅ **One-click "Open Editor" per project (2026-05-30).** Each project row in the Sidebar has an
  Open-Editor (`</>`) action (shown only when an online machine has code-server — `editorAvailable`).
  It **creates-or-reuses a single editor session per project** (never spawns a 2nd code-server for the
  same project) and opens it embedded in the Lattice shell. Verified on mini-ops via Playwright: click →
  VS Code workbench mounts INSIDE the Lattice iframe (2 editor WS), 2nd click reuses (session count
  stays 1), editor tab/glyph/labels render. (Workspace.tsx `onOpenEditor`/`editorAvailable`, Sidebar.tsx
  CodeIcon button + editor KindGlyph.)
- ✅ **Fleet rollout to studio + mbp (D30/D28, 2026-05-30).** code-server 4.112.0 installed via brew on
  both; their agents redeployed with the tunnel-capable binary (via the hub installer, preserving
  `--name mbp` / `--name Dylans-Mac-Studio.local` so agentIDs stay stable). **All three Macs now report
  `codeServerInstalled` and have live tunnels.** Cross-machine editor VERIFIED via Playwright: a
  device-scoped editor on mbp AND on studio each rendered its remote home in VS Code through the hub
  (browser → hub@mini-ops → yamux tunnel → remote code-server), 2 editor WS each, clean teardown. The
  milestone headline ("full VS Code on ANY fleet machine, one hub URL") is real for the 3 Macs.
  ✅ **D23 remote project paths FIXED (2026-05-30):** `resolveCwd` now rebases any
  `.../AI-Hub/projects/<rest>` path onto the agent's own `$HOME`, so a project session placed on ANY
  machine opens the correct Syncthing-synced copy (editor/claude/terminal alike). Verified: a
  project-scoped editor pinned to studio (hub sent `/Users/mini-ops/AI-Hub/projects/lattice`) opened
  `/Users/dylanstory/AI-Hub/projects/lattice` — the real lattice tree (agent/, internal/, go.mod…).
  **pc (Windows) still pending: needs the WSL2 spawn path (D30) + pc online.**
- ✅ **Monaco-rail retirement DONE (D31, 2026-05-30).** Deleted the read-only rail
  (ProjectFilesPanel/FileViewer/MonacoPanel/useFileBrowser) + dropped the `@monaco-editor/react` dep.
  The embedded VS Code's own EXPLORER is now the file surface. **Project name-click → opens the editor**
  (create-or-reuse, falls back to expand when no code-server anywhere); **device name-click → device
  editor** when that machine has code-server (else expand). Verified on mini-ops via Playwright: click a
  project → VS Code fills the pane, no separate rail, workbench mounts. *(Top-level Phase-2
  `FileBrowser.tsx` + `/api/agents/{id}/files` left intact — separate dashboard feature.)*
- Install code-server on studio/mbp/pc and verify the editor cross-machine (Windows = code-server inside
  WSL2, D30 — `codeserver.go`/`detectWSL` already gate this; the spawn path needs a WSL `wsl.exe -e`
  wrapper).
- Cursor-grade AI chrome beside the editor (D29): reuse the Phase-3 Claude runner; later a Lattice VS
  Code extension (Cmd-K / autocomplete / in-workbench chat).

### Build approach (unchanged from prior phases)
Parallel subagents per phase, **adversarially verified against the real fleet** (mini-ops/studio/mbp/pc)
— never trust an implementer agent's self-report. Rebuild `bash scripts/build.sh` → `pm2 restart
lattice-hub`; deploy agents via the hub installer (launchd LaunchAgents — D22 / Windows Scheduled Task);
token in `.lattice-token`; push to `origin/master`.

---

## Prior milestone (Phase 3 Workspace) — DONE & VERIFIED, kept for reference below
The reframed Phase 3 shipped: Projects→Sessions + Devices sidebar, long-lived Terminal + Claude
(interactive `claude` in a PTY since D35; originally stream-json, Max subscription) sessions surviving browser detach + hub restart, smart placement, the
onboarding wizard, and the (now-to-be-retired) read-only Monaco file rail. Full detail follows.

## Phase 3 (Workspace) — DECIDED (D15–D25), BUILT & VERIFIED  [D16 superseded by D26 for the IDE milestone]
- **D15** shell: browser-first SPA now → **Tauri** wrapper later (bundles the Go agent as a
  sidecar). **D16** editor: lean (file tree + Monaco + the two tabs), code-server dropped.
  **D17** Claude tab: the LOCAL `claude` binary headless in stream-json (subscription; verified
  flags) — NOT the pay-per-token Managed Agents API. *(SUPERSEDED by D35: now interactive `claude` in a PTY.)* **D18** persistence: first-class Session
  entity, processes outlive the browser (the core fix — today PTYs die with the browser WS).
  **D19** placement: capability filter (Claude needs `claude` present — mbp lacks it) + headroom +
  locality boost, visible/overridable. **D20** portability: placed + resumable (not live-migrated).
  **D21** trust: skip-perms default + audit log + per-machine approval kill switch.
- **Build = parallel workstreams (WS-1 proto gates):** see ROADMAP.md Phase 3 for WS-1…WS-9.
- **Fleet facts that drive the build:** claude installed on mini-ops/studio/pc, NOT mbp; node
  everywhere; studio already aliases `claude --dangerously-skip-permissions --permission-mode
  bypassPermissions`; AI-Hub + ~/.claude are Syncthing-synced fleet-wide.

## Phase 4 — what shipped and is VERIFIED (the success criterion)
Self-hosted, no-cloud: **the hub is the distribution + enrollment point**.
- Hub serves: `GET /dl/{name}` (agent binaries from `--dist`, default `dist/`; strict
  basename allowlist + path.Base → traversal-safe), `GET /install.sh` + `GET /install.ps1`
  (rendered from go:embed templates with the hub URL baked from the request `Host`),
  `GET /api/enroll` → {hubUrl, token, unix one-liner, windows one-liner}.
- Installers: detect OS/arch, download the agent from the hub, install a **persistent service**
  — launchd LaunchAgent (macOS), systemd --user unit (Linux), per-user logon Scheduled Task
  (Windows) — and enroll via token. Idempotent. Token passed at runtime, never baked in.
- Dashboard "Add device" modal: copy-paste join command (macOS/Linux + Windows tabs).
- **VERIFIED on real hardware:** re-provisioned mbp via `curl …/install.sh | sh -s -- --token …`
  → launchd service installed, KeepAlive-respawn confirmed, rejoined mesh. Provisioned pc via
  `install.ps1` → Scheduled Task `LatticeAgent` Running, rejoined mesh. One command per machine.
- NOT done (deferred): GitHub Releases / Homebrew tap / winget manifest (the public-distribution
  channel). The private-mesh story — hub-as-distribution — is complete and is the stronger
  packageability proof. Windows-on-ARM build + detection deferred (amd64 runs under emulation).

## Workspace UX — DECIDED (D15–D21) + Phase 3 BACKEND/FRONTEND BUILT & VERIFIED on the fleet (2026-05-29)
The 7 questions resolved as D15–D21. Backend (proto + sessions + claude runner + placement +
re-discovery + audit) and frontend (workspace shell: Projects→Sessions sidebar, Terminal + Claude
tabs, machine chip, Monaco) are BUILT, building green (`go build/vet/test`, `npm run build`), and
**verified against the real fleet**. Build plan: ROADMAP Phase 3 + `~/.claude/plans/stateful-
watching-ripple.md`. New decisions from verification: **D22** (Claude needs a launchd LaunchAgent for
Keychain auth), **D23** (projectPath portability gap — follow-up).

### VERIFIED working (exercised the running system, not just code)
- **Placement (D19):** `/api/placement` + `/api/sessions` score correctly — mini-ops/studio eligible
  with RAM/load/cores breakdown + locality; **mbp hard-excluded "claude not installed"**; pc "offline";
  pin override works. `/api/projects` returns the 24 synced projects.
- **Claude tab (D17) — END TO END:** a Claude session on studio's launchd agent returned a real
  **"PONG"** over `/ws/session` with real token usage (in 13700 / out 5 / cache 30028); full
  stream-json (`system→stream_event→user→assistant→result`) replayed + streamed live. Uses the **Max
  subscription** (no API key). Hub-assigned `--session-id` ⇒ Lattice sessionId == Claude sessionId.
- **Persistence (D18) — END TO END:** terminal process outlives browser detach; **scrollback replays
  on reattach**; session + process **survive a HUB RESTART**, are re-adopted (status live), scrollback
  intact (1053 B), and **live I/O still works** after the restart.
- **Frontend:** workspace shell renders (WORKSPACE|FLEET toggle, Projects→Sessions sidebar, empty-state),
  matches the dark emerald/zinc aesthetic. Screenshot: /tmp/lattice-workspace.png.

### Device projects (D24) — ADDED & VERIFIED (2026-05-29, same session)
Sessions now have a **`scope`**: `project` (synced, auto-placed) or **`device`** (pinned to one machine,
cwd = that machine's **home**) — for machine-local work (set up programs, organize files, admin a box).
UI: a **DEVICES** section under PROJECTS (fleet, online-first, CLAUDE capability chip, offline dimmed),
each device → its device sessions + "+ new session"; static machine chip for device sessions. Strict
placement (runs on the device or fails; capability filter still applies). Agent resolves empty/`~` cwd to
home (the hub can't — home paths differ per box; partially addresses D23). **Verified on the fleet:**
device terminal on mini-ops → `pwd`=`/Users/mini-ops`; device Claude on mbp → 400 "claude not installed";
device Claude on studio → real "PONG" in its home. Schema: idempotent `sessions.scope` ALTER migration.

### Right-side file explorer (IDE-style) — ADDED & VERIFIED (2026-05-29)
3-column workspace layout: left Projects/Devices sidebar | center session panes | **right file rail**.
Clicking a PROJECT label opens its file tree on the right (browsed via the hub's **local agent** —
`Agent.local` — at the project's hub-host path, so no sync needed), with breadcrumb nav + a lazy Monaco
read-only viewer (click a file → syntax-highlighted content; size-capped + download for big/binary).
Clicking a DEVICE label opens that machine's home files via its own agent (bonus). The in-session Monaco
panel was removed (the right rail is now the sole browser). Below md the rail overlays as a drawer.
Verified: clicked `lattice` → tree rendered; clicked `.gitignore` → viewer showed it. Shared engine:
`useFileBrowser.ts` + `FileViewer.tsx` + `ProjectFilesPanel.tsx`. Follow-up: write-back is not wired
(read-only viewer); file edits still go through a Claude/terminal session.

### "Begin new project" onboarding wizard (D25) — ADDED & VERIFIED (2026-05-29)
A `POST /api/projects` endpoint + a 4-step workspace wizard ("+ new project" in the PROJECTS header):
name → folder (live kebab + collision check) → stack/port → connectors/MCPs/agents/related/envs →
review (register + auto-launch toggles). The hub scaffolds the AI-Hub skeleton (README, CLAUDE.md,
docs/PROJECT_CONTEXT, **docs/ONBOARDING.md brief**, .env, .gitignore, .claude, git init) directly in
projectsRoot (syncs everywhere), registers it (row in **~/AI-Hub/UNIVERSAL_RULES.md** — NOT CLAUDE.md —
+ regenerate PROJECT_INDEX.md + KB stub), and launches a Claude session on the **local agent** (loopback
detection → `Agent.Local`, so files are co-located) **seeded** with the onboarding brief. Connectors/etc.
are captured as intent for Claude to wire. **Verified end-to-end** (scaffold/register/launch/seed) on the
fleet; test project fully reverted. Caveat: the onboarding session lands on mini-ops which can't auth
claude WHILE this Claude-desktop session monopolizes it (the known local-agent contamination; works on a
clean box). Follow-ups: auto-link the KB stub into wiki `_index`/`_map`; optional `/gsd:new-project` seed.

### Bugs found & fixed THIS session (via adversarial fleet verification)
1. `claude --print --output-format=stream-json` **requires `--verbose`** — launcher omitted it (instant exit). FIXED.
2. Launcher now **scrubs `ANTHROPIC_API_KEY`/`CLAUDECODE`/`CLAUDE_CODE_*`** from the child env → forces subscription auth (also the cost rule). FIXED.
3. **D18 core bug:** long-lived PTY/claude were spawned from the **per-connection context**, so a hub
   restart (agent↔hub link drop) KILLED every session. Now rooted at the **process-global base context**
   (newAgentState(ctx) → registries' baseCtx). FIXED + re-verified across a hub restart.
4. **D22 (operational):** macOS Claude auth needs a **launchd LaunchAgent** (GUI-session Keychain), not
   a nohup daemon — studio reprovisioned via the hub installer; "Not logged in" → real "PONG".

### Fleet state after this session
- mini-ops: hub + local agent under PM2 (new binary, claude=true). studio: **launchd LaunchAgent**
  (`sh.lattice.agent`, new binary, claude=true) — reprovisioned from nohup. mbp: launchd (new binary,
  claude=false — correct, no claude installed). pc: OFFLINE (asleep) — still on old binary, redeploy when awake.
- Cosmetic: a stale offline `studio` agent record (old `--name studio`) coexists with the launchd
  `Dylans-Mac-Studio.local` record — agentID = hostname+os, and the installer uses the real hostname
  while deploy-fleet used `studio`. Harmless dup; clean up by removing the offline row.

### NOT YET DONE (follow-ups)
- **D23 projectPath portability** (name-relative resolution) — needed for true cross-machine resume.
- **Cross-machine Claude resume** (orphan on A → `--resume` on B) not yet exercised end-to-end (needs D23 + Syncthing-timing retry).
- **Audit log + approval kill switch** wired in backend but not yet exercised/verified.
- **Interactive frontend QA** (click into a session, drive the Claude chat UI in-browser) — only the empty-state was screenshotted.
- **pc (Windows)** redeploy + Claude-session auth on Windows (credential store) untested.
- **WS-9 Tauri** desktop packaging — not started (deferred sub-phase).
- Monaco write-back, projects served per-machine vs hub-only listing.

## Phase 2 — what shipped and is VERIFIED
- **Interactive terminal:** per-agent PTY via `github.com/aymanbagabas/go-pty` (unix + Windows
  ConPTY, CGO-free). Dashboard xterm.js terminal over `/ws/terminal?agent=<id>`. Verified: real
  zsh session on mbp through the browser (independent Playwright agent) + WS-level echo test.
- **File browser:** scoped list (`GET /api/agents/{id}/files?path=`) + download
  (`GET /api/agents/{id}/download?path=`), dirs-first, breadcrumb + up-nav. Verified: listed
  mini-ops /tmp + PC `C:\` (cross-OS), downloaded a seeded file, browser UI renders 30+ entries.
- **Wake-on-LAN:** agent broadcasts a magic packet (SO_BROADCAST, unconnected socket +
  per-interface directed broadcasts — macOS needs this). **Verified end-to-end on real hardware:**
  slept the PC → it dropped offline (still visible via offline-retention) → POST wake from the
  dashboard (mini-ops agent as LAN sender) → PC woke + agent reconnected in ~35s.
- **Offline-machine retention:** hub `fleet()` merges persisted (offline, last-known metrics+MACs)
  with the live registry, so a sleeping machine stays visible and wakeable. Agents report their
  MACs in the heartbeat → Wake is turnkey (no manual MAC entry).
- Reviewer majors fixed: WoL SO_BROADCAST + macOS broadcast path; PTY `explicitClose` data race
  (atomic.Bool).

## Phase 1 — what shipped and is RUNNING
- Single Go binary (`github.com/dylanstoryyy/lattice`), roles `hub`/`agent`. Go 1.26.3 installed.
- **Hub LIVE under PM2 on mini-ops** (`pm2 ls` → `lattice-hub`, port 7400), reachable at
  `http://mini-ops.tail3c8bee.ts.net:7400`. SQLite at `lattice.db` (WAL). PM2 list saved.
- **4/4 agents online:** mini-ops (local agent, PM2 `lattice-agent`), studio, mbp (darwin/arm64),
  pc (windows/amd64). Macs started via `scripts/deploy-fleet.sh` (nohup); PC via WMI
  `Win32_Process.Create` (session 0, survives SSH logout).
- Dashboard (React/Vite/Tailwind dark "fleet console") embedded in the hub binary, served at `/`.
- **Verified E2E:** WS-level test ran `uname -a` on 3 Macs + `ver` on PC, all exit 0 with real
  output; independent Playwright agent drove the browser UI (select machine → type → Run →
  streamed output + exit code rendered). Screenshots: /tmp/lattice-dash.png, /tmp/lattice-ui-after.png.

## Key implementation decisions (this session)
- **Pure-Go SQLite (`modernc.org/sqlite`)** so the whole mesh cross-compiles `CGO_ENABLED=0`
  from mini-ops → 5 targets (darwin arm64/amd64, windows amd64, linux amd64/arm64). See D10.
- gorilla/websocket; gopsutil/v4 for heartbeat metrics; single-writer per conn.
- Enrollment token persisted in `.lattice-token` (gitignored; rotated 2026-06-05 — never echo the value here).
- Hardened after adversarial review: hub read+write deadlines (half-open socket cleanup),
  agent backoff reset after healthy session, exec rejected to swept-offline agents, stable
  fleet ordering.

## Build / deploy / run (operational)
- Build all: `bash scripts/build.sh` (dashboard → embed → cross-compile to dist/).
- Deploy agents: `bash scripts/deploy-fleet.sh [all|studio|mbp|pc]`.
- Hub: `pm2 start scripts/pm2-hub.config.cjs` / `pm2 restart lattice-hub`.
- Open: `http://mini-ops.tail3c8bee.ts.net:7400`.

## Environment facts (verified 2026-05-29)
- Hub host = mini-ops; port 7400; build host = mini-ops (cross-compile all targets).
- Full SSH mesh + passwordless sudo across the fleet — use it to deploy/verify.
- PC live-adapter MAC for WoL (Phase 2): `30-56-0F-4C-4D-3A` (Ethernet 3, Realtek, on LAN).
- Dogfood fleet + per-OS quirks: see FLEET.md.

## Known minor items (non-blocking, deferred)
- Dashboard pulls Google Fonts over CDN — vendor them for true offline/privacy before release.
- Agent persistence is light (nohup / Win32 detached) — proper per-OS services are Phase 4.
- agentID = hostname+os; add a hardware-id/nonce before shipping (two same-hostname boxes collide).

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

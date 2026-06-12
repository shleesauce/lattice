# Dogfood Bugs — running log of bugs/observations hit while using Lattice for real across the fleet.

## Status (v0.2.1, held on master, unreleased)
- **BUG-002 reconnect** — ✅ fixed (hub ws keepalive + client auto-reconnect). *Needs on-device confirm.*
- **BUG-005 ntfy noise** — ✅ fixed (per-turn ping dropped; permission/exit/PR kept). Live toggle = follow-up.
- **BUG-006 resize spam** — ✅ fixed (debounced + delta-checked). *Needs on-device confirm.*
- **BUG-007 git/terminal tabs** — ✅ fixed (was a symptom of 006). *Needs on-device confirm.*
- **BUG-008 file viewer** — ✅ built (in-panel markdown/text viewer). *Needs a runtime click-through.*
- **BUG-009 status dots** — ✅ built (red dot when a session is blocked on a decision). *Needs a live waiting session to see red.*
- **BUG-001 mobile layout** — 🔨 planned; needs a real phone to tune (Workspace shell has no mobile rules).
- **BUG-003 terminal paste** — 🔨 planned; text-paste fix needs a phone; image-paste = bigger (agent upload endpoint).
- **BUG-004 Fable 5 field** — ❓ needs a concrete repro (no code defect found).
See `docs/V0.2.1-PLAN.md` for the full consolidated scope.

### BUG-001 · Mobile UI scaling/layout broken
- when: 2026-06-11, on phone
- severity: HIGH
- area: dashboard
- doing: using Lattice from phone
- expected: dashboard scales and lays out correctly on mobile; text block moves/positions correctly
- got: mobile UI scaling is off, text block doesn't move correctly, general layout problems
- repro/notes: open dashboard on phone viewport. Multiple symptoms — likely needs responsive pass. Specific offender: "text block" (input/composer?) mis-positions.
- status: open

### BUG-002 · Major lag on session "reconnecting"
- when: 2026-06-11, on phone
- severity: HIGH
- area: agent
- doing: using active sessions from phone
- expected: sessions reconnect quickly/seamlessly (or not drop at all)
- got: every couple minutes the session greys out with a red "trying to reconnect" error; auto-reconnect is slow/ineffective, but a quick manual page refresh fixes it instantly
- repro/notes: observed on mobile. Two-part: (a) connection drops too often (~every few min — shouldn't be happening), (b) auto-reconnect path is far slower/worse than a full page reload, which suggests the client reconnect logic isn't recovering well. Likely WS keepalive/heartbeat timeout + a reconnect path that doesn't re-establish cleanly. Compare client reconnect vs. fresh-load handshake.
- status: open

### BUG-003 · Copy/paste broken in terminals (text + images)
- when: 2026-06-11, on phone
- severity: HIGH
- area: editor
- doing: trying to copy/paste text and paste photos into a terminal/session
- expected: paste text into the terminal; paste images/photos as attachments
- got: copy/paste doesn't work in the terminals at all; can't paste any photos
- repro/notes: mobile. Two facets — (a) text clipboard paste fails, (b) image paste unsupported/fails. May be xterm/clipboard handling or mobile paste-event capture.
- status: open

### BUG-004 · Fable 5 introduces text-field bug
- when: 2026-06-11, on phone
- severity: MED
- area: editor
- doing: using the Fable 5 model in a session
- expected: text field behaves normally regardless of model
- got: selecting/using Fable 5 introduces a "weird bug" in the text input field
- repro/notes: vague — switch model to Fable 5, observe input field misbehaving. Need specifics on the symptom (cursor jump? duplication? clearing?) next time it happens.
- status: open

### BUG-005 · ntfy phone notifications too frequent; want a live toggle
- when: 2026-06-11, on phone
- severity: MED
- area: agent
- doing: receiving ntfy phone notifications for session activity
- expected: notifications only for meaningful events (not every turn); ability to enable/disable phone notifications live, per-session, on the fly
- got: WAY too many ntfy notifications — fires every single time
- repro/notes: two parts — (a) notification trigger is too noisy (should fire on completion/attention-needed, not every event), (b) feature request: a live in-session toggle to turn phone notifications on/off at any time. Check the ntfy publish call site in the hub/agent notification path.
- status: open

### BUG-006 · Side-panel terminal glitches on resize (repeating new lines)
- when: 2026-06-11, on phone
- severity: HIGH
- area: preview
- doing: using the terminal in the right-side panel, adjusting the panel split size
- expected: terminal resizes cleanly; content reflows without spamming new lines
- got: terminal is glitchy and barely works; every time the panel split is resized it repeatedly starts new lines like it's refreshing/recreating
- repro/notes: mobile. Resize → repeated newline spam suggests the terminal (xterm fit/reflow) re-inits or re-renders the prompt on every resize event. Likely missing debounce on the resize→fit, or PTY cols/rows churn re-printing the prompt.
- status: open

### BUG-007 · Git and Terminal panel tabs are identical / both broken
- when: 2026-06-11, on phone
- severity: HIGH
- area: preview
- doing: switching between the Git and Terminal tabs in the side panel
- expected: Git tab shows git status/diff view; Terminal tab shows a working shell — two distinct, functional views
- got: no difference between Git and Terminal — they behave the same, and both are completely broken
- repro/notes: mobile. Possibly the Git tab is unimplemented and falling through to the terminal component, plus the shared terminal backend is broken (see BUG-006). Confirm whether Git is a stub.
- status: open

### BUG-008 · Want in-panel file viewer (view .md/files from all machines, no download)
- when: 2026-06-11, on phone
- severity: MED
- area: preview
- doing: trying to view files (.md and others) from machines in the fleet
- expected: browse/view files directly in the panel, served from each machine
- got: files must all be downloaded first; no way to just view them in-panel
- repro/notes: feature request — host/serve fleet files so they render in the panel (esp. markdown preview). Cross-machine file viewing without round-tripping a download.
- status: open

### BUG-009 · Session status dots should be state-driven (4 colors), not always green
- when: 2026-06-11, on phone
- severity: MED
- area: dashboard
- doing: looking at the per-project/session status dots in the left panel
- expected: dot color reflects live session state —
    - green = complete / no attention needed
    - blue = done & wrapped up, no decision required (informational)
    - yellow = actively running (esp. multi-agent / parallel work)
    - red = issue/concern/error/blocked OR needs a decision (a question that can't continue without an answer)
- got: every session always shows a green glowing dot regardless of state
- repro/notes: feature request — derive dot color from session status. Needs a status model that distinguishes running vs. done-no-decision vs. complete vs. blocked/needs-input. Hook into agent state + a "waiting on user" detector for red.
- status: open

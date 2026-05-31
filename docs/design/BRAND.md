# Lattice — Brand & Design System

> **Direction LOCKED 2026-05-30:** *Living Mesh — "Cool Fabric, Warm Life"* on a
> weightless neon wireframe. Seeded from a 5-image inspiration board (warped
> glowing wireframe, soft luminous grid, radial node-burst, terracotta breeze-block
> lattice, lit triangulated dome). This is the source of truth + the kickoff brief
> for claude.ai/design (paste-ready prompt at the bottom).

---

## 0. The one idea

**Your machines rest in cool light; where work runs, they ignite warm.**

Color = state. The whole UI is a cool, glowing wireframe lattice at rest. Wherever
a session is **alive / running**, that node blooms **warm**. A glance at the mesh
tells you what's working. Weightless, holographic, pure light — no material, no
drop shadows; depth comes from luminosity and layered darkness.

---

## 1. Essence

Lattice is your **personal compute mesh** — every machine you own woven into one
living fabric you command from one screen (even your phone). Work flows to the best
machine; sessions survive sleep/disconnect; a full AI-paired IDE (VS Code + Claude)
runs on any node. Self-hosted, private, no cloud — **yours**.

The name is the metaphor: a *lattice* is an ordered web of interconnected nodes. The
design makes that web literal and **alive** — a glowing wireframe that breathes and
ignites where work happens.

**Personality:** alive · calm · connected · sovereign · quietly powerful.
**Tension it holds:** a **control room** (fleet, status, metrics — dense, precise)
*and* a **calm creative workspace** (editor + AI — focused). Cool calm; warm where it counts.

---

## 2. The look — Living Mesh / neon wireframe

A weightless, glowing **wireframe lattice** on near-true-black (refs: the warped
electric mesh + the soft luminous grid). Lines glow **cool**. Live nodes bloom
**warm**, with a breathing pulse; light travels **warm** along the lattice toward
nodes where work is running. Empty/hero space *is* the mesh — a real, dimensional,
slightly warping glowing grid, not a faint texture. No shadows: depth is light + dark layers.

---

## 3. Color tokens (dark-only)

### Backgrounds — near-true-black so neon reads; depth via layers, not shadow
| token | hex | use |
|---|---|---|
| `bg.void` | `#000000` | editor / terminal — deepest |
| `bg.base` | `#050608` | app canvas |
| `bg.surface` | `#0A0D11` | panels, cards, sidebar |
| `bg.raised` | `#0F141A` | popovers, menus, active/hover rows |
| `bg.overlay` | `rgba(0,0,0,0.70)` | modal scrim |

### Borders / lattice lines (cool, faintly lit)
| token | hex |
|---|---|
| `border.subtle` | `#14202A` |
| `border.default` | `#1E2D3A` |
| `line.glow` | `cool @ low alpha` (lit hairlines / the mesh) |
| `border.focus` | `cool` (#2DE2C0) |

### Text
| token | hex |
|---|---|
| `text.primary` | `#E9EFF1` |
| `text.secondary` | `#A4B0B8` |
| `text.muted` | `#6E7B84` |
| `text.faint` | `#46525A` |

### COOL — the fabric: structure, idle, brand-at-rest, primary actions, links, focus
| token | hex |
|---|---|
| `cool` | `#2DE2C0` (signature teal) |
| `cool.bright` | `#5BF0D4` (hover/peak) |
| `cool.blue` | `#38BDF8` (secondary cool — the wireframe-line blue) |
| `cool.dim` | `#1AA890` |
| `cool.ink` | `#04130F` (text/icon on a cool fill) |

### WARM — life: **only** for alive / active / running. Warm always means "work is happening here."
| token | hex |
|---|---|
| `warm` | `#F5A623` (amber-gold) |
| `warm.bright` | `#FFC24B` |
| `warm.dim` | `#C77F18` |
| `warm.ink` | `#1A0E00` |
| `warm.bloom` | `0 0 16px rgba(245,166,35,0.35)` (glow on live nodes) |

### Status — the heartbeat (warm = alive · cool = at-rest · red = problem)
| state | hex | motion |
|---|---|---|
| `live` (active, in use) | `#F5A623` warm | **breathing warm bloom** |
| `starting` (igniting) | `#FFC24B` warm-bright | quick pulse |
| `detached` (running, unattended) | `#38BDF8` cool blue | steady |
| `ready / idle` | `#2DE2C0` cool teal | steady, faint |
| `orphaned` (offline, resumable) | `#F2792E` warm-orange | steady (alert) |
| `exited` (done) | `#4A5560` cool gray | none |
| `danger` `#FF5C6C` · `warning` `#FFB454` · `info` `#38BDF8` | | |

> Disambiguation to refine in claude.ai/design: `live` / `starting` / `warning` are
> all warm — keep them separable by brightness + motion (live = rich gold breathing,
> starting = pale fast pulse, warning = banner context).

---

## 4. Typography

- **Display / UI:** a humanist grotesk — warm + precise (seeds: **Hanken Grotesk**,
  General Sans, Mona Sans). Headings slight negative tracking, weight 600. Reserve
  tracked uppercase **only** for tiny system labels (`PROJECTS`, `DEVICES`).
- **Mono (code · terminal · metrics · IDs):** **IBM Plex Mono** — a primary face here.
- **Scale:** 11 / 12 / 13.5 / 15 / 18 / 24 / 32. Body line-height ~1.5.

---

## 5. Shape · space · depth

- **Radii:** `sm 6` · `md 10` · `lg 14` · `xl 20` · `full` (dots/pills).
- **Spacing:** 4-based (4/8/12/16/24/32); generous gutters in the workspace.
- **Depth = light, not shadow.** No drop shadows. Separation via background layers
  (`void → base → surface → raised`) + glowing hairlines + the warm/cool blooms.
  Cards read as faintly-lit panels on black, not floating material.

---

## 6. Texture & motion

- **The mesh is real.** A weightless glowing wireframe lattice (cool), gently
  warping, present in hero/empty states — and literally the substrate of the fleet
  view (machines = nodes on the mesh).
- **Warm = alive, and it moves:** live nodes breathe (warm bloom, ~2.8s ease-in-out);
  when a session is placed/active, a **warm light travels** along the lattice line
  from hub → node. Idle = steady cool glow.
- **Weightless transitions:** fade + glow (opacity/bloom), not slide + shadow.
  150–220ms ease-out.

---

## 7. Iconography & mark

- **Icons:** line, ~1.7 stroke, rounded joins; the node/dot recurs.
- **Mark (from ref #3 — the radial node-burst):** cool teal nodes radiating from a
  **warm live core** — literally "cool fabric, warm life," and reads as a lattice +
  a living network igniting at the center.
- **Wordmark:** lowercase `lattice` in the humanist display — calm.

---

## 8. VS Code theme mapping (the embedded editor wears the skin)

The system emits a matching VS Code theme. Warm marks **where you're actively
working** (cursor, active tab, selection); structure stays cool; code stays calm.

```
editor.background                 #000000   (bg.void)
sideBar.background                #0A0D11
activityBar.background            #050608
editorGroupHeader.tabsBackground  #0A0D11
tab.activeBackground              #0F141A
tab.activeBorderTop               #F5A623   (warm — the tab you're IN is "alive")
statusBar.background              #050608
terminal.background               #000000
editorCursor.foreground           #F5A623   (warm — you, working)
editor.selectionBackground        rgba(245,166,35,0.16)   (warm, low)
editor.lineHighlightBackground    rgba(245,166,35,0.06)   (active line, warm)
focusBorder                       #2DE2C0   (cool)
```

Syntax (calm-cool, warm only on literals so values feel "live"):
```
comments        #5A6670   italic
keywords        #5BC8FF   (cool blue)
strings         #2DE2C0   (teal)
functions       #7FD9FF   (soft cyan)
numbers/const   #FFB454   (warm)
types/classes   #9EE6C9   (mint)
variables       #E9EFF1
```

---

## 9. Surfaces the system must cover

1. **Mesh / fleet view** — machines as nodes on the glowing wireframe; live machines
   bloom warm, idle stay cool; warm light flows to active nodes; offline/wake state.
2. **Workspace shell** — left rail (projects→sessions, devices), tab strip, active pane.
3. **The editor split** — themed VS Code (warm cursor/active tab) ∣ Claude chat, draggable divider.
4. **Claude chat** — message stream, tool calls, composer.
5. **Terminal** — themed PTY (true-black, warm cursor).
6. **Dialogs** — new session / placement preview (ranked machines), new-project wizard, enroll.
7. **Status & chips** — the warm/cool dot+pulse language, machine chips, placement scores.

---

## 10. Paste-ready claude.ai/design kickoff prompt

```
Design a cohesive, dark-only design system + key-screen mockups for "Lattice" — a
self-hosted personal compute-mesh app. Lattice weaves all of one person's machines
into one living fabric they command from a single screen (even a phone): work is
placed on the best machine, long-running sessions survive sleep/disconnect, and a
full AI-paired IDE (embedded VS Code + a Claude assistant) runs on any machine.
Private, no cloud. It is BOTH a control room (fleet status, live metrics — dense,
precise) AND a calm creative workspace (code editor + AI chat). Not a generic SaaS
dashboard.

CORE IDEA — "Cool Fabric, Warm Life": the whole UI is a weightless, glowing
WIREFRAME LATTICE on near-true-black — holographic, pure light, NO drop shadows
(depth comes from luminosity + layered darkness). The lattice glows COOL (teal/blue)
at rest. Wherever a session is ALIVE / running, that node blooms WARM (amber-gold)
with a breathing pulse, and warm light travels along the lattice from hub toward
active nodes. Color encodes state: cool = structure/idle/at-rest, warm = work is
happening here. A glance at the mesh shows what's alive. Visual references the client
pinned: a warped glowing electric-blue wireframe; a soft luminous pixel-grid with
bloom; a radial node-burst (logo); architectural lattices (breeze-block, lit
triangulated dome).

PALETTE SEED (refine for contrast/harmony):
- backgrounds (near-true-black, layered): #000000 void, #050608 base, #0A0D11 surface, #0F141A raised
- text: #E9EFF1 / #A4B0B8 / #6E7B84
- COOL (fabric / structure / idle / primary actions / focus): teal #2DE2C0 + blue #38BDF8
- WARM (alive / active / running — used ONLY for live state, never decoration): amber-gold #F5A623, bloom glow
- status: live #F5A623 (warm breathing bloom), starting #FFC24B, detached #38BDF8 (cool),
  ready/idle #2DE2C0 (cool), orphaned #F2792E, exited #4A5560 gray, danger #FF5C6C

TYPE: a humanist grotesk for display/UI (warm + precise, NOT all-caps-heavy — tracked
uppercase only for tiny system labels) + IBM Plex Mono as a primary face for
code/terminal/metrics/IDs. Soft radii (6–20px). 4-based spacing, generous gutters.

DELIVER:
1. A full token system: color (background layers, text tiers, the COOL + WARM accent
   pair and their states, semantic + status), typography scale, spacing, radius, and
   a DEPTH model based on light/glow rather than shadow. Motion principles: the
   breathing warm "alive" bloom on live nodes, and warm light flowing along the
   cool lattice toward active nodes.
2. A logo/mark: a radial node-burst — cool teal nodes radiating from a WARM live core
   (a lattice + a living network igniting at center); lowercase "lattice" wordmark.
3. High-fidelity mockups: (a) the MESH/fleet view — machines as nodes on a glowing
   cool wireframe; live machines bloom warm, idle stay cool, warm light flows to
   active ones, plus an offline/wake state; (b) the WORKSPACE with the editor split —
   themed VS Code (warm cursor + active tab) on the left, a Claude chat (stream +
   composer) on the right, a left rail of projects→sessions and devices, a tab strip;
   (c) the "new session / placement" dialog showing ranked machines.
4. A matching VS Code color theme (true-black editor; warm cursor / active-tab /
   selection marking where you're working; cool focus; calm cool syntax with warm
   only on literals), so the embedded editor feels native.

Dark mode only. Weightless neon, no material shadows. Prioritize legible dense data +
calm focus. Keep the glowing-lattice + warm-life motif consistent across every surface.
```

---

## 11. Open knobs (say the word, I'll fold in)

- **Cool primary:** teal `#2DE2C0` vs leaning electric-blue `#38BDF8` as the lead.
- **Warm exact:** amber-gold `#F5A623` vs hotter `#FF8A3C` vs softer `#FFC24B`.
- **Display font:** Hanken Grotesk · General Sans · Mona Sans.
- **Mesh intensity:** subtle background wireframe vs a bold, ever-present warping lattice.
- **Warm scope:** strictly live-state only (purest), or also primary CTAs (warmer overall feel).

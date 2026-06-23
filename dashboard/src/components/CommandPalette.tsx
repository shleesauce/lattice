/* ⌘K command palette — the single fastest way through the mesh. Jump to a view,
   open a live session, open a project, focus or wake a machine, or reach into
   settings, all from the keyboard. Reads the same live data the rest of the app
   already holds (machines / projects / sessions) and routes intent back up. */
import { useEffect, useMemo, useRef, useState } from 'react'
import { useFocusTrap } from '../useFocusTrap'
import type { Machine } from '../lattice/adapt'
import type { Project, Session } from '../types'
import { Icon } from '../lattice/Icon'

export type View = 'fleet' | 'workspace'

interface Command {
  id: string
  group: string
  label: string
  hint?: string
  icon: string
  // Lowercased haystack for matching (label + any extra terms).
  terms: string
  run: () => void
}

interface Props {
  open: boolean
  onClose: () => void
  view: View
  machines: Machine[]
  projects: Project[]
  sessions: Session[]
  canWake: boolean
  onGoFleet: () => void
  onGoWorkspace: () => void
  onFocusMachine: (id: string) => void
  onWakeMachine: (m: Machine) => void
  onOpenSession: (id: string) => void
  onOpenProject: (path: string) => void
  onNewSession: () => void
  onNewWorkflow: () => void
  onNewProject: () => void
  onOpenSettings: () => void
}

// Order the groups appear in (when there's NO query). Anything not listed last.
const GROUP_ORDER = ['Navigate', 'Sessions', 'Projects', 'Machines', 'Actions']
const groupRank = (g: string) => {
  const i = GROUP_ORDER.indexOf(g)
  return i === -1 ? GROUP_ORDER.length : i
}

// Relevance score of a command against the query tokens (higher = better).
// Label hits beat secondary-term hits; prefix/word-boundary beat mid-substring;
// earlier matches beat later ones. The curated group order is only a tie-break,
// so typing a machine name surfaces "Focus <machine>" first instead of burying
// it under the fixed group order (F6).
function scoreCommand(cmd: Command, tokens: string[]): number {
  const label = cmd.label.toLowerCase()
  let total = 0
  for (const t of tokens) {
    const li = label.indexOf(t)
    let s: number
    if (label === t) s = 100
    else if (label.startsWith(t)) s = 80
    else if (label.includes(' ' + t)) s = 64
    else if (li >= 0) s = 46 - Math.min(li, 18)
    else s = cmd.terms.includes(t) ? 12 : 0
    total += s
  }
  return total * 10 - groupRank(cmd.group)
}

export function CommandPalette({
  open,
  onClose,
  view,
  machines,
  projects,
  sessions,
  canWake,
  onGoFleet,
  onGoWorkspace,
  onFocusMachine,
  onWakeMachine,
  onOpenSession,
  onOpenProject,
  onNewSession,
  onNewWorkflow,
  onNewProject,
  onOpenSettings,
}: Props) {
  const [q, setQ] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  // Contain Tab/Shift+Tab within the palette + restore focus on close. The
  // palette's own rAF focuses the input first; the trap only governs cycling.
  const trapRef = useFocusTrap(open)

  // Reset query + selection each time the palette opens; focus the input.
  useEffect(() => {
    if (!open) return
    setQ('')
    setActive(0)
    // Focus after paint so the field is ready.
    const t = requestAnimationFrame(() => inputRef.current?.focus())
    return () => cancelAnimationFrame(t)
  }, [open])

  // Build the full command set from live data. Recomputed only when inputs move.
  const commands = useMemo<Command[]>(() => {
    const out: Command[] = []
    const run = (fn: () => void) => () => {
      fn()
      onClose()
    }

    // ── Navigate ──
    if (view !== 'fleet')
      out.push({ id: 'nav-fleet', group: 'Navigate', label: 'Go to Fleet', hint: 'the live mesh map', icon: 'layers', terms: 'go to fleet mesh map devices machines', run: run(onGoFleet) })
    if (view !== 'workspace')
      out.push({ id: 'nav-ws', group: 'Navigate', label: 'Go to Workspace', hint: 'projects & sessions', icon: 'terminal', terms: 'go to workspace projects sessions claude terminal editor', run: run(onGoWorkspace) })

    // ── Sessions (active only — never the dead ones) ──
    for (const s of sessions) {
      if (s.status === 'exited' || s.archived || s.deletedAt) continue
      const m = machines.find((x) => x.agentId === s.agentId)
      const where = m?.label ?? s.agentId.slice(0, 8)
      out.push({
        id: `sess-${s.id}`,
        group: 'Sessions',
        label: s.title || s.kind,
        hint: `${s.kind} · ${where}`,
        icon: s.kind === 'editor' ? 'file-code' : s.kind === 'terminal' ? 'terminal' : 'sparkles',
        terms: `${s.title ?? ''} ${s.kind} ${where} session`.toLowerCase(),
        run: run(() => onOpenSession(s.id)),
      })
    }

    // ── Projects ──
    for (const p of projects) {
      out.push({
        id: `proj-${p.path}`,
        group: 'Projects',
        label: p.name,
        hint: 'open project',
        icon: 'folder',
        terms: `${p.name} ${p.path} project open`.toLowerCase(),
        run: run(() => onOpenProject(p.path)),
      })
    }

    // ── Machines ──
    for (const m of machines) {
      if (m.offline && m.mac && canWake) {
        out.push({
          id: `wake-${m.id}`,
          group: 'Machines',
          label: `Wake ${m.label}`,
          hint: 'offline · send wake-on-LAN',
          icon: 'power',
          terms: `wake ${m.label} ${m.hostname} offline power on`.toLowerCase(),
          run: run(() => onWakeMachine(m)),
        })
      }
      out.push({
        id: `mach-${m.id}`,
        group: 'Machines',
        label: `Focus ${m.label}`,
        hint: m.offline ? 'offline' : m.locLabel,
        icon: m.os === 'windows' ? 'monitor' : m.kind === 'smartphone' ? 'smartphone' : 'server',
        terms: `focus ${m.label} ${m.hostname} ${m.os} machine device`.toLowerCase(),
        run: run(() => onFocusMachine(m.id)),
      })
    }

    // ── Actions ──
    out.push({ id: 'act-new-session', group: 'Actions', label: 'New session…', hint: 'pick any project & place it', icon: 'sparkles', terms: 'new session start claude terminal editor project pick run', run: run(onNewSession) })
    out.push({ id: 'act-new-workflow', group: 'Actions', label: 'Implement issue / Review PR…', hint: 'paste a GitHub issue or PR URL', icon: 'git-branch', terms: 'workflow implement issue review pr github url worktree template', run: run(onNewWorkflow) })
    out.push({ id: 'act-new-project', group: 'Actions', label: 'Begin a new project…', hint: 'scaffold & register', icon: 'plus', terms: 'begin new project create scaffold onboard wizard', run: run(onNewProject) })
    out.push({ id: 'act-settings', group: 'Actions', label: 'Open Settings', hint: 'primary machine, about', icon: 'settings', terms: 'settings preferences primary machine about config', run: run(onOpenSettings) })

    return out
  }, [view, machines, projects, sessions, canWake, onClose, onGoFleet, onGoWorkspace, onFocusMachine, onWakeMachine, onOpenSession, onOpenProject, onNewSession, onNewWorkflow, onNewProject, onOpenSettings])

  // Filter: every whitespace-separated token must appear (AND substring match).
  // With a query, rank by relevance (best match first); empty query keeps the
  // natural build order so the grouped view below reads in curated order.
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    if (!needle) return commands
    const tokens = needle.split(/\s+/)
    return commands
      .filter((c) => {
        const hay = `${c.label.toLowerCase()} ${c.terms}`
        return tokens.every((t) => hay.includes(t))
      })
      .map((c) => ({ c, s: scoreCommand(c, tokens) }))
      .sort((a, b) => b.s - a.s)
      .map((x) => x.c)
  }, [q, commands])

  // Clamp the active index whenever the result set changes.
  useEffect(() => {
    setActive((i) => (filtered.length === 0 ? 0 : Math.min(i, filtered.length - 1)))
  }, [filtered.length])

  // Render list. Each row keeps its flat index into `filtered` so arrow-key
  // navigation stays linear. With a query, `filtered` is already ranked best-
  // first, so show ONE flat "Best matches" list (no group reshuffling — that
  // would fight the ranking). Empty query → the curated grouped view.
  const hasQuery = q.trim().length > 0
  const groups = useMemo<[string, { cmd: Command; index: number }[]][]>(() => {
    if (hasQuery) {
      return [['Best matches', filtered.map((cmd, index) => ({ cmd, index }))]]
    }
    const by = new Map<string, { cmd: Command; index: number }[]>()
    filtered.forEach((cmd, index) => {
      const arr = by.get(cmd.group) ?? []
      arr.push({ cmd, index })
      by.set(cmd.group, arr)
    })
    return [...by.entries()].sort((a, b) => groupRank(a[0]) - groupRank(b[0]))
  }, [filtered, hasQuery])

  // Keep the active row scrolled into view.
  useEffect(() => {
    if (!open) return
    const el = listRef.current?.querySelector<HTMLElement>(`[data-idx="${active}"]`)
    el?.scrollIntoView({ block: 'nearest' })
  }, [active, open])

  if (!open) return null

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((i) => (filtered.length ? (i + 1) % filtered.length : 0))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => (filtered.length ? (i - 1 + filtered.length) % filtered.length : 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      filtered[active]?.run()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }

  return (
    <div className="cmdk-scrim" onMouseDown={onClose}>
      <div ref={trapRef} tabIndex={-1} className="cmdk" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Command palette">
        <div className="cmdk-head">
          <Icon name="search" size={16} color="var(--fg-3)" />
          <input
            ref={inputRef}
            className="cmdk-input"
            placeholder="Jump to a session, project, or machine…"
            value={q}
            onChange={(e) => {
              setQ(e.target.value)
              setActive(0)
            }}
            onKeyDown={onKeyDown}
            spellCheck={false}
            autoComplete="off"
          />
          <span className="cmdk-esc">esc</span>
        </div>

        <div className="cmdk-list term-scroll" ref={listRef}>
          {filtered.length === 0 ? (
            <div className="cmdk-empty">No matches for “{q.trim()}”</div>
          ) : (
            groups.map(([group, items]) => (
              <div key={group} className="cmdk-group">
                <div className="cmdk-group-h">{group}</div>
                {items.map(({ cmd, index }) => (
                  <button
                    key={cmd.id}
                    type="button"
                    data-idx={index}
                    className={`cmdk-row ${index === active ? 'on' : ''}`}
                    onMouseMove={() => setActive(index)}
                    onClick={() => cmd.run()}
                  >
                    <span className="cmdk-ic">
                      <Icon name={cmd.icon} size={15} />
                    </span>
                    <span className="cmdk-label">{cmd.label}</span>
                    {cmd.hint && <span className="cmdk-hint">{cmd.hint}</span>}
                  </button>
                ))}
              </div>
            ))
          )}
        </div>

        <div className="cmdk-foot">
          <span><kbd>↑</kbd><kbd>↓</kbd> navigate</span>
          <span><kbd>↵</kbd> open</span>
          <span><kbd>esc</kbd> close</span>
        </div>
      </div>
    </div>
  )
}

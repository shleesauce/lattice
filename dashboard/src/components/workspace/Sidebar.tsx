import { useMemo, useState } from 'react'
import type { LoadState } from '../../useWorkspace'
import type { Project, Session, SessionKind } from '../../types'
import { statusDotClass, statusPulses } from './sessionMeta'

interface Props {
  projects: Project[]
  sessions: Session[]
  projectsState: LoadState
  activeSessionId: string | null
  collapsed: boolean
  onToggleCollapse: () => void
  onSelectSession: (id: string) => void
  onNewSession: (project: Project) => void
}

// Left rail: collapsible Projects → Sessions tree. Projects come from
// /api/projects; each expands to its sessions (filtered by projectPath).
export function Sidebar({
  projects,
  sessions,
  projectsState,
  activeSessionId,
  collapsed,
  onToggleCollapse,
  onSelectSession,
  onNewSession,
}: Props) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [query, setQuery] = useState('')

  // Default-expand any project that currently has sessions, plus the active one.
  const sessionsByProject = useMemo(() => {
    const m = new Map<string, Session[]>()
    for (const s of sessions) {
      const arr = m.get(s.projectPath) ?? []
      arr.push(s)
      m.set(s.projectPath, arr)
    }
    return m
  }, [sessions])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return projects
    return projects.filter((p) => p.name.toLowerCase().includes(q))
  }, [projects, query])

  const isOpen = (path: string) =>
    expanded[path] ?? ((sessionsByProject.get(path)?.length ?? 0) > 0)

  if (collapsed) {
    return (
      <aside className="flex w-12 shrink-0 flex-col items-center border-r border-zinc-800 bg-zinc-950/60 py-3">
        <button
          type="button"
          onClick={onToggleCollapse}
          title="expand sidebar"
          className="grid h-8 w-8 place-items-center rounded-md text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300"
        >
          <PanelIcon />
        </button>
      </aside>
    )
  }

  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-zinc-800 bg-zinc-950/60">
      <div className="flex items-center gap-2 border-b border-zinc-800 px-3 py-2.5">
        <span className="font-display text-xs font-semibold uppercase tracking-[0.18em] text-zinc-400">projects</span>
        <button
          type="button"
          onClick={onToggleCollapse}
          title="collapse sidebar"
          className="ml-auto grid h-6 w-6 place-items-center rounded text-zinc-600 hover:bg-zinc-900 hover:text-zinc-300"
        >
          <PanelIcon />
        </button>
      </div>

      <div className="px-2.5 py-2">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="filter projects…"
          className="w-full rounded-md border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 font-mono text-[11px] text-zinc-200 placeholder:text-zinc-600 focus:border-emerald-500/50 focus:outline-none"
        />
      </div>

      <nav className="term-scroll min-h-0 flex-1 overflow-y-auto px-1.5 pb-3">
        {projectsState === 'loading' && <SidebarSkeleton />}
        {projectsState === 'error' && (
          <p className="px-2 py-6 text-center font-mono text-[11px] text-red-400/80">// projects unavailable</p>
        )}
        {projectsState === 'ready' && filtered.length === 0 && (
          <p className="px-2 py-6 text-center font-mono text-[11px] text-zinc-600">// no projects</p>
        )}
        {filtered.map((p) => {
          const ps = sessionsByProject.get(p.path) ?? []
          const open = isOpen(p.path)
          return (
            <div key={p.path} className="mb-0.5">
              <div className="group flex items-center rounded-md hover:bg-zinc-900/70">
                <button
                  type="button"
                  onClick={() => setExpanded((e) => ({ ...e, [p.path]: !open }))}
                  className="flex min-w-0 flex-1 items-center gap-1.5 px-2 py-1.5 text-left"
                >
                  <Caret open={open} />
                  <FolderIcon active={ps.length > 0} />
                  <span className="min-w-0 flex-1 truncate font-display text-[13px] text-zinc-200">{p.name}</span>
                  {ps.length > 0 && (
                    <span className="shrink-0 rounded bg-zinc-800 px-1.5 font-mono text-[10px] text-zinc-400">
                      {ps.length}
                    </span>
                  )}
                </button>
                <button
                  type="button"
                  onClick={() => onNewSession(p)}
                  title="new session"
                  className="mr-1 grid h-6 w-6 shrink-0 place-items-center rounded text-zinc-600 opacity-0 transition-opacity hover:bg-zinc-800 hover:text-emerald-300 group-hover:opacity-100"
                >
                  <PlusIcon />
                </button>
              </div>

              {open && (
                <ul className="ml-3 border-l border-zinc-800 pl-1.5">
                  {ps.length === 0 ? (
                    <li>
                      <button
                        type="button"
                        onClick={() => onNewSession(p)}
                        className="flex w-full items-center gap-1.5 px-2 py-1.5 text-left font-mono text-[11px] text-zinc-600 hover:text-emerald-300"
                      >
                        <PlusIcon /> new session
                      </button>
                    </li>
                  ) : (
                    ps.map((s) => (
                      <SessionRow
                        key={s.id}
                        session={s}
                        active={s.id === activeSessionId}
                        onSelect={() => onSelectSession(s.id)}
                      />
                    ))
                  )}
                </ul>
              )}
            </div>
          )
        })}
      </nav>
    </aside>
  )
}

function SessionRow({
  session,
  active,
  onSelect,
}: {
  session: Session
  active: boolean
  onSelect: () => void
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors ${
          active ? 'bg-emerald-500/10 text-emerald-200' : 'text-zinc-400 hover:bg-zinc-900/70 hover:text-zinc-200'
        }`}
      >
        <StatusDot status={session.status} />
        <KindGlyph kind={session.kind} />
        <span className="min-w-0 flex-1 truncate font-mono text-[11px]">
          {session.title || (session.kind === 'claude' ? 'claude' : 'terminal')}
        </span>
      </button>
    </li>
  )
}

function StatusDot({ status }: { status: Session['status'] }) {
  const cls = statusDotClass(status)
  return (
    <span className="relative flex h-2 w-2 shrink-0 items-center justify-center" title={status}>
      {statusPulses(status) && <span className={`absolute inline-flex h-full w-full rounded-full opacity-70 animate-breathe ${cls}`} />}
      <span className={`relative inline-flex h-1.5 w-1.5 rounded-full ${cls}`} />
    </span>
  )
}

function KindGlyph({ kind }: { kind: SessionKind }) {
  if (kind === 'claude') {
    return (
      <svg viewBox="0 0 24 24" className="h-3 w-3 shrink-0 text-emerald-400/80" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
        <path d="M12 4v16M4 12h16" strokeLinecap="round" />
      </svg>
    )
  }
  return (
    <svg viewBox="0 0 24 24" className="h-3 w-3 shrink-0 text-zinc-500" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M5 7l4 4-4 4M12 16h7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function Caret({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={`h-3 w-3 shrink-0 text-zinc-600 transition-transform ${open ? 'rotate-90' : ''}`}
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      aria-hidden
    >
      <path d="M9 6l6 6-6 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function FolderIcon({ active }: { active: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={`h-3.5 w-3.5 shrink-0 ${active ? 'text-emerald-400/80' : 'text-zinc-500'}`} fill="currentColor" aria-hidden>
      <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  )
}

function PlusIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="M12 5v14m-7-7h14" strokeLinecap="round" />
    </svg>
  )
}

function PanelIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M9 4v16" />
    </svg>
  )
}

function SidebarSkeleton() {
  return (
    <div className="space-y-1 px-1 py-1">
      {Array.from({ length: 7 }).map((_, i) => (
        <div key={i} className="flex items-center gap-2 px-2 py-1.5">
          <span className="h-3.5 w-3.5 animate-pulse rounded bg-zinc-800" />
          <span className="h-3 animate-pulse rounded bg-zinc-800/70" style={{ width: `${50 + (i % 4) * 12}%` }} />
        </div>
      ))}
    </div>
  )
}

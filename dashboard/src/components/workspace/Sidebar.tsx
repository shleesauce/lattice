/// <reference types="vite/client" />
import { useMemo, useState } from 'react'
import type { LoadState } from '../../useWorkspace'
import type { Agent, Project, Session, SessionKind, SessionStatus } from '../../types'

export type SidebarMode = 'active' | 'archived' | 'trash'

interface Props {
  projects: Project[]
  sessions: Session[]
  agents: Agent[]
  projectsState: LoadState
  activeSessionId: string | null
  collapsed: boolean
  mode: SidebarMode
  onMode: (m: SidebarMode) => void
  onToggleCollapse: () => void
  onSelectSession: (id: string) => void
  onNewSession: (project: Project) => void
  onNewDeviceSession: (agent: Agent) => void
  onBeginNewProject: () => void
  onOpenEditor: (project: Project) => void
  onOpenDeviceEditor: (agent: Agent) => void
  onArchiveSession: (id: string, archived: boolean) => void
  onTrashSession: (id: string) => void
  onRestoreTrash: (id: string) => void
  onDeleteForever: (session: Session) => void
  onEmptyTrash: () => void
  editorAvailable: boolean
}

const TRASH_TTL_DAYS = 30

function sessionDotClass(status: SessionStatus): string {
  switch (status) {
    case 'live':      return 'dot live'
    case 'starting':  return 'dot starting'
    case 'detached':  return 'dot detached'
    case 'orphaned':  return 'dot orphaned'
    case 'exited':    return 'dot exited'
  }
}

function deviceDotClass(online: boolean, hasSessions: boolean): string {
  if (!online)      return 'dot exited'
  if (hasSessions)  return 'dot live'
  return 'dot idle'
}

function deviceMeta(agent: Agent, sessionCount: number): string {
  if (!agent.online) return 'offline'
  if (sessionCount > 0) return `${sessionCount}●`
  return 'idle'
}

function trashDaysLeft(deletedAt?: string): number {
  if (!deletedAt) return TRASH_TTL_DAYS
  const t = Date.parse(deletedAt)
  if (Number.isNaN(t)) return TRASH_TTL_DAYS
  const elapsedDays = (Date.now() - t) / 86_400_000
  return Math.max(0, Math.ceil(TRASH_TTL_DAYS - elapsedDays))
}

// Left rail: a mode switcher (Active / Archived / Trash) over the same rail
// chrome. Active shows the Projects→Sessions tree + Devices; Archived and Trash
// show a flat, clearly-labelled list of those sessions with the right actions.
export function Sidebar({
  projects,
  sessions,
  agents,
  projectsState,
  activeSessionId,
  collapsed,
  mode,
  onMode,
  onToggleCollapse,
  onSelectSession,
  onNewSession,
  onNewDeviceSession,
  onBeginNewProject,
  onOpenEditor,
  onOpenDeviceEditor,
  onArchiveSession,
  onTrashSession,
  onRestoreTrash,
  onDeleteForever,
  onEmptyTrash,
  editorAvailable,
}: Props) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [query, setQuery] = useState('')

  // Partition sessions: trash wins (deletedAt set), then archived, then active.
  const activeSessions = useMemo(() => sessions.filter((s) => !s.deletedAt && !s.archived), [sessions])
  const archivedSessions = useMemo(() => sessions.filter((s) => !s.deletedAt && s.archived), [sessions])
  const trashedSessions = useMemo(() => sessions.filter((s) => !!s.deletedAt), [sessions])

  const counts = {
    active: activeSessions.length,
    archived: archivedSessions.length,
    trash: trashedSessions.length,
  }

  const projectName = (path: string) => projects.find((p) => p.path === path)?.name ?? path.split('/').pop() ?? path
  const agentName = (id: string) => agents.find((a) => a.id === id)?.hostname ?? id.slice(0, 8)
  const subtitle = (s: Session) => (s.scope === 'device' ? agentName(s.agentId) : projectName(s.projectPath))

  // ── Collapsed rail ──────────────────────────────────────────────────────────
  if (collapsed) {
    return (
      <aside className="rail" style={{ width: 48, alignItems: 'center', paddingTop: 12 }}>
        <button
          type="button"
          onClick={onToggleCollapse}
          title="expand sidebar"
          style={{ padding: '6px', borderRadius: 8, color: 'var(--fg-3)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
        >
          <PanelIcon />
        </button>
      </aside>
    )
  }

  return (
    <aside className="rail">
      <div className="rail-head">
        <span className="net" style={{ marginLeft: 0, marginRight: 'auto' }}>
          <span className="dot live" style={{ width: 6, height: 6 }} />
          mesh
        </span>
        <button
          type="button"
          onClick={onToggleCollapse}
          title="collapse sidebar"
          style={{ padding: 4, borderRadius: 6, color: 'var(--fg-3)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
        >
          <PanelIcon />
        </button>
      </div>

      {/* Mode switcher: Active · Archived · Trash */}
      <div className="ws-modes">
        <ModeTab label="Active" count={counts.active} on={mode === 'active'} onClick={() => onMode('active')} />
        <ModeTab label="Archived" count={counts.archived} on={mode === 'archived'} onClick={() => onMode('archived')} />
        <ModeTab label="Trash" count={counts.trash} on={mode === 'trash'} onClick={() => onMode('trash')} />
      </div>

      {mode === 'active' ? (
        <ActiveTree
          projects={projects}
          activeSessions={activeSessions}
          agents={agents}
          projectsState={projectsState}
          activeSessionId={activeSessionId}
          query={query}
          setQuery={setQuery}
          expanded={expanded}
          setExpanded={setExpanded}
          editorAvailable={editorAvailable}
          onSelectSession={onSelectSession}
          onNewSession={onNewSession}
          onNewDeviceSession={onNewDeviceSession}
          onBeginNewProject={onBeginNewProject}
          onOpenEditor={onOpenEditor}
          onOpenDeviceEditor={onOpenDeviceEditor}
          onArchiveSession={onArchiveSession}
          onTrashSession={onTrashSession}
        />
      ) : (
        <div className="rail-scroll">
          {/* Clear banner: this is NOT the active workspace */}
          <div className={`ws-banner ${mode}`}>
            {mode === 'archived' ? (
              <>
                <ArchiveIcon />
                <span>Archived sessions — hidden from the workspace, kept until you delete them.</span>
              </>
            ) : (
              <>
                <TrashIcon />
                <span>Trash — sessions are permanently deleted {TRASH_TTL_DAYS} days after they land here.</span>
              </>
            )}
          </div>

          {mode === 'trash' && trashedSessions.length > 0 && (
            <div style={{ padding: '4px 14px 8px' }}>
              <button
                type="button"
                className="btn btn-danger"
                style={{ width: '100%', justifyContent: 'center' }}
                onClick={onEmptyTrash}
              >
                <TrashIcon />
                Empty Trash ({trashedSessions.length})
              </button>
            </div>
          )}

          {(mode === 'archived' ? archivedSessions : trashedSessions).length === 0 ? (
            <div style={{ padding: '14px 18px', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-3)' }}>
              {mode === 'archived' ? '// nothing archived' : '// trash is empty'}
            </div>
          ) : (
            (mode === 'archived' ? archivedSessions : trashedSessions).map((s) => (
              <FlatSessionRow
                key={s.id}
                session={s}
                subtitle={subtitle(s)}
                mode={mode}
                daysLeft={mode === 'trash' ? trashDaysLeft(s.deletedAt) : undefined}
                onSelect={() => onSelectSession(s.id)}
                onUnarchive={() => onArchiveSession(s.id, false)}
                onRestore={() => onRestoreTrash(s.id)}
                onTrash={() => onTrashSession(s.id)}
                onDeleteForever={() => onDeleteForever(s)}
              />
            ))
          )}
        </div>
      )}

      {mode === 'active' && (
        <div className="rail-foot">
          <button
            type="button"
            className="btn btn-run"
            style={{ width: '100%', justifyContent: 'center' }}
            onClick={() => {
              const first = projects[0]
              if (first) onNewSession(first)
              else onBeginNewProject()
            }}
          >
            <PlusIcon size={14} />
            New session
          </button>
        </div>
      )}
    </aside>
  )
}

// ── Active mode: the Projects → Sessions tree + Devices ──────────────────────

function ActiveTree({
  projects,
  activeSessions,
  agents,
  projectsState,
  activeSessionId,
  query,
  setQuery,
  expanded,
  setExpanded,
  editorAvailable,
  onSelectSession,
  onNewSession,
  onNewDeviceSession,
  onBeginNewProject,
  onOpenEditor,
  onOpenDeviceEditor,
  onArchiveSession,
  onTrashSession,
}: {
  projects: Project[]
  activeSessions: Session[]
  agents: Agent[]
  projectsState: LoadState
  activeSessionId: string | null
  query: string
  setQuery: (s: string) => void
  expanded: Record<string, boolean>
  setExpanded: React.Dispatch<React.SetStateAction<Record<string, boolean>>>
  editorAvailable: boolean
  onSelectSession: (id: string) => void
  onNewSession: (project: Project) => void
  onNewDeviceSession: (agent: Agent) => void
  onBeginNewProject: () => void
  onOpenEditor: (project: Project) => void
  onOpenDeviceEditor: (agent: Agent) => void
  onArchiveSession: (id: string, archived: boolean) => void
  onTrashSession: (id: string) => void
}) {
  const projectSessions = useMemo(() => activeSessions.filter((s) => s.scope !== 'device'), [activeSessions])

  const sessionsByProject = useMemo(() => {
    const m = new Map<string, Session[]>()
    for (const s of projectSessions) {
      const arr = m.get(s.projectPath) ?? []
      arr.push(s)
      m.set(s.projectPath, arr)
    }
    return m
  }, [projectSessions])

  const sessionsByAgent = useMemo(() => {
    const m = new Map<string, Session[]>()
    for (const s of activeSessions) {
      if (s.scope !== 'device') continue
      const arr = m.get(s.agentId) ?? []
      arr.push(s)
      m.set(s.agentId, arr)
    }
    return m
  }, [activeSessions])

  const orderedAgents = useMemo(
    () =>
      [...agents].sort((a, b) => {
        if (a.online !== b.online) return a.online ? -1 : 1
        return (a.hostname || a.id).localeCompare(b.hostname || b.id)
      }),
    [agents],
  )

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return projects
    return projects.filter((p) => p.name.toLowerCase().includes(q))
  }, [projects, query])

  const isOpen = (path: string) => expanded[path] ?? ((sessionsByProject.get(path)?.length ?? 0) > 0)
  const toggle = (path: string, current: boolean) => setExpanded((e) => ({ ...e, [path]: !current }))

  return (
    <div className="rail-scroll">
      <div className="rail-sec">
        Projects
        <span className="ct">{projects.length}</span>
        <button
          type="button"
          onClick={onBeginNewProject}
          title="new project"
          style={{ marginLeft: 4, display: 'inline-flex', alignItems: 'center', gap: 3, color: 'var(--fg-3)', fontSize: 10, borderRadius: 5, padding: '2px 5px' }}
          className="btn btn-ghost"
        >
          <PlusIcon size={10} /> new
        </button>
      </div>

      <div style={{ padding: '4px 16px 8px' }}>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="filter…"
          style={{
            width: '100%',
            background: 'var(--void)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: '5px 10px',
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--fg-1)',
            outline: 'none',
          }}
        />
      </div>

      {projectsState === 'loading' && <RailSkeleton />}
      {projectsState === 'error' && (
        <div style={{ padding: '12px 18px', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--st-danger)' }}>
          // projects unavailable
        </div>
      )}
      {projectsState === 'ready' && filtered.length === 0 && (
        <div style={{ padding: '12px 18px', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-3)' }}>
          // no projects
        </div>
      )}

      {filtered.map((p) => {
        const ps = sessionsByProject.get(p.path) ?? []
        const open = isOpen(p.path)
        const hasActive = ps.some((s) => s.id === activeSessionId)
        return (
          <div key={p.path}>
            <div
              className={`prow proj${hasActive ? ' active' : ''}`}
              onClick={() => (editorAvailable ? onOpenEditor(p) : toggle(p.path, open))}
              style={{ userSelect: 'none' }}
            >
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  toggle(p.path, open)
                }}
                title={open ? 'collapse' : 'expand'}
                style={{ display: 'flex', alignItems: 'center', color: 'var(--fg-3)', background: 'none', padding: 0 }}
              >
                <Caret open={open} />
              </button>
              <FolderIcon active={ps.length > 0} />
              <span className="nm">{p.name}</span>
              {ps.length > 0 && (
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-3)', marginLeft: 'auto', paddingRight: 2 }}>
                  {ps.length}
                </span>
              )}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  onNewSession(p)
                }}
                title="new session"
                className="btn btn-ghost"
                style={{ padding: '2px 4px', opacity: 0, fontSize: 10, borderRadius: 5 }}
                onMouseEnter={(e) => (e.currentTarget.style.opacity = '1')}
                onMouseLeave={(e) => (e.currentTarget.style.opacity = '0')}
              >
                <PlusIcon size={11} />
              </button>
            </div>

            {open && (
              <div style={{ marginLeft: 8, borderLeft: '1px solid var(--border)', marginBottom: 2 }}>
                {ps.length === 0 ? (
                  <button
                    type="button"
                    onClick={() => onNewSession(p)}
                    className="srow"
                    style={{ width: '100%', textAlign: 'left', paddingLeft: 18 }}
                  >
                    <PlusIcon size={10} />
                    <span className="nm" style={{ color: 'var(--fg-3)', fontSize: 11 }}>
                      new session
                    </span>
                  </button>
                ) : (
                  ps.map((s) => (
                    <ProjectSessionRow
                      key={s.id}
                      session={s}
                      active={s.id === activeSessionId}
                      onSelect={() => onSelectSession(s.id)}
                      onArchive={() => onArchiveSession(s.id, true)}
                      onTrash={() => onTrashSession(s.id)}
                    />
                  ))
                )}
              </div>
            )}
          </div>
        )
      })}

      <div className="rail-sec" style={{ marginTop: 8 }}>
        Devices
        <span className="ct">{agents.filter((a) => a.online).length} woven</span>
      </div>

      {orderedAgents.length === 0 ? (
        <div style={{ padding: '8px 18px', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-3)' }}>
          // no devices
        </div>
      ) : (
        orderedAgents.map((a) => {
          const ds = sessionsByAgent.get(a.id) ?? []
          const hasEditor = a.capabilities?.codeServerInstalled ?? false
          const metaLabel = deviceMeta(a, ds.length)
          const dotCls = deviceDotClass(a.online, ds.length > 0)
          return (
            <div
              key={a.id}
              className={`mrow${a.online ? '' : ' off'}`}
              onClick={() => {
                if (!a.online) return
                if (hasEditor) onOpenDeviceEditor(a)
                else onNewDeviceSession(a)
              }}
              title={a.online ? undefined : 'offline'}
            >
              <span className={dotCls} />
              <span className="name">{a.hostname || a.name || a.id.slice(0, 8)}</span>
              {!a.online ? (
                <button
                  type="button"
                  className="wake-mini"
                  title="wake machine"
                  onClick={(e) => {
                    e.stopPropagation()
                    onNewDeviceSession(a)
                  }}
                >
                  <PowerIcon />
                </button>
              ) : (
                <span className="meta" style={ds.length > 0 ? { color: 'var(--green)' } : undefined}>
                  {metaLabel}
                </span>
              )}
            </div>
          )
        })
      )}
    </div>
  )
}

// ── Sub-components ──────────────────────────────────────────────────────────

function ModeTab({ label, count, on, onClick }: { label: string; count: number; on: boolean; onClick: () => void }) {
  return (
    <button type="button" className={`ws-mode${on ? ' on' : ''}`} onClick={onClick}>
      {label}
      {count > 0 && <span className="ws-mode-ct">{count}</span>}
    </button>
  )
}

function ProjectSessionRow({
  session,
  active,
  onSelect,
  onArchive,
  onTrash,
}: {
  session: Session
  active: boolean
  onSelect: () => void
  onArchive: () => void
  onTrash: () => void
}) {
  return (
    <div className={`srow${active ? ' on' : ''}`} onClick={onSelect} style={{ paddingLeft: 18 }}>
      <span className={sessionDotClass(session.status)} />
      <KindGlyph kind={session.kind} />
      <span className="nm">{session.title || session.kind}</span>
      <span className="srow-actions">
        <ActBtn title="Archive session" onClick={onArchive}><ArchiveIcon /></ActBtn>
        <ActBtn title="Delete (move to Trash)" danger onClick={onTrash}><TrashIcon /></ActBtn>
      </span>
    </div>
  )
}

function FlatSessionRow({
  session,
  subtitle,
  mode,
  daysLeft,
  onSelect,
  onUnarchive,
  onRestore,
  onTrash,
  onDeleteForever,
}: {
  session: Session
  subtitle: string
  mode: SidebarMode
  daysLeft?: number
  onSelect: () => void
  onUnarchive: () => void
  onRestore: () => void
  onTrash: () => void
  onDeleteForever: () => void
}) {
  return (
    <div className="srow" style={{ paddingLeft: 16, opacity: 0.85 }} onClick={onSelect}>
      <span className={sessionDotClass(session.status)} />
      <KindGlyph kind={session.kind} />
      <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', lineHeight: 1.25 }}>
        <span className="nm" style={{ flex: 'none' }}>{session.title || session.kind}</span>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 9.5, color: 'var(--fg-3)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {subtitle}
          {mode === 'trash' && daysLeft != null && ` · ${daysLeft}d left`}
        </span>
      </div>
      <span className="srow-actions">
        {mode === 'archived' ? (
          <>
            <ActBtn title="Unarchive (restore to workspace)" onClick={onUnarchive}><RestoreIcon /></ActBtn>
            <ActBtn title="Delete (move to Trash)" danger onClick={onTrash}><TrashIcon /></ActBtn>
          </>
        ) : (
          <>
            <ActBtn title="Restore from Trash" onClick={onRestore}><RestoreIcon /></ActBtn>
            <ActBtn title="Delete forever" danger onClick={onDeleteForever}><TrashIcon /></ActBtn>
          </>
        )}
      </span>
    </div>
  )
}

function ActBtn({ title, danger, onClick, children }: { title: string; danger?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      className={`srow-act${danger ? ' danger' : ''}`}
      title={title}
      onClick={(e) => {
        e.stopPropagation()
        onClick()
      }}
    >
      {children}
    </button>
  )
}

function ArchiveIcon() {
  return (
    <svg viewBox="0 0 24 24" style={{ width: 13, height: 13 }} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <rect x="3" y="4" width="18" height="4" rx="1" />
      <path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8" />
      <path d="M10 12h4" />
    </svg>
  )
}

function RestoreIcon() {
  return (
    <svg viewBox="0 0 24 24" style={{ width: 13, height: 13 }} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M3 7v6h6" />
      <path d="M3 13a9 9 0 1 0 3-7.7L3 8" />
    </svg>
  )
}

function TrashIcon() {
  return (
    <svg viewBox="0 0 24 24" style={{ width: 13, height: 13 }} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m2 0v14a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V6" />
      <path d="M10 11v6M14 11v6" />
    </svg>
  )
}

function KindGlyph({ kind }: { kind: SessionKind }) {
  if (kind === 'claude') {
    return (
      <svg viewBox="0 0 24 24" style={{ width: 12, height: 12, flexShrink: 0, color: 'var(--teal)', opacity: 0.85 }} fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
        <path d="M12 4v16M4 12h16" strokeLinecap="round" />
      </svg>
    )
  }
  if (kind === 'editor') {
    return (
      <svg viewBox="0 0 24 24" style={{ width: 12, height: 12, flexShrink: 0, color: 'var(--blue)', opacity: 0.85 }} fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
        <path d="M8 7l-4 5 4 5M16 7l4 5-4 5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    )
  }
  return (
    <svg viewBox="0 0 24 24" style={{ width: 12, height: 12, flexShrink: 0, color: 'var(--fg-3)' }} fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M5 7l4 4-4 4M12 16h7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function Caret({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      style={{ width: 12, height: 12, flexShrink: 0, transition: 'transform var(--dur) var(--ease)', transform: open ? 'rotate(90deg)' : 'rotate(0deg)' }}
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
    <svg viewBox="0 0 24 24" style={{ width: 13, height: 13, flexShrink: 0, color: active ? 'var(--teal)' : 'var(--fg-3)', opacity: active ? 0.85 : 1 }} fill="currentColor" aria-hidden>
      <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  )
}

function PlusIcon({ size = 14 }: { size?: number }) {
  return (
    <svg viewBox="0 0 24 24" style={{ width: size, height: size, flexShrink: 0 }} fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="M12 5v14m-7-7h14" strokeLinecap="round" />
    </svg>
  )
}

function PanelIcon() {
  return (
    <svg viewBox="0 0 24 24" style={{ width: 16, height: 16 }} fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M9 4v16" />
    </svg>
  )
}

function PowerIcon() {
  return (
    <svg viewBox="0 0 24 24" style={{ width: 13, height: 13 }} fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M12 2v6M6.8 6.8a8 8 0 1 0 10.4 0" strokeLinecap="round" />
    </svg>
  )
}

function RailSkeleton() {
  return (
    <div style={{ padding: '4px 8px', display: 'flex', flexDirection: 'column', gap: 4 }}>
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          style={{ height: 28, borderRadius: 9, background: 'var(--surface)', opacity: 0.6, animation: 'pulseStart 1.4s ease infinite', width: `${55 + (i % 4) * 10}%`, marginLeft: 10 }}
        />
      ))}
    </div>
  )
}

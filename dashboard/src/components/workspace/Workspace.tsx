import { useCallback, useEffect, useState } from 'react'
import type { Agent, Session, SessionWithPlacement } from '../../types'
import { useWorkspace } from '../../useWorkspace'
import { deleteSession, resumeSession } from '../../api'
import { Sidebar } from './Sidebar'
import { SessionPane } from './SessionPane'
import { NewSessionDialog } from './NewSessionDialog'
import type { NewSessionTarget } from './NewSessionDialog'
import { statusDotClass, statusPulses } from './sessionMeta'

interface Props {
  agents: Agent[]
}

// The workspace shell: Projects→Sessions rail, open-session tabs, and the active
// session pane. Sidebar status comes from useWorkspace polling /api/sessions.
export function Workspace({ agents }: Props) {
  const ws = useWorkspace()
  const [openIds, setOpenIds] = useState<string[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState(false)
  const [newTarget, setNewTarget] = useState<NewSessionTarget | null>(null)
  // Below md the sidebar overlays the pane instead of sitting beside it.
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  const sessionById = (id: string): Session | undefined => ws.sessions.find((s) => s.id === id)

  const openSession = useCallback((id: string) => {
    setOpenIds((prev) => (prev.includes(id) ? prev : [...prev, id]))
    setActiveId(id)
  }, [])

  const closeTab = useCallback(
    (id: string) => {
      setOpenIds((prev) => {
        const next = prev.filter((x) => x !== id)
        setActiveId((cur) => (cur === id ? next[next.length - 1] ?? null : cur))
        return next
      })
    },
    [],
  )

  // Drop tabs whose session disappeared from the backend.
  useEffect(() => {
    setOpenIds((prev) => prev.filter((id) => ws.sessions.some((s) => s.id === id)))
  }, [ws.sessions])

  const onCreated = useCallback(
    (res: SessionWithPlacement) => {
      ws.upsertSession(res.session)
      void ws.refreshSessions()
      setNewTarget(null)
      setMobileNavOpen(false)
      openSession(res.session.id)
    },
    [ws, openSession],
  )

  const selectFromNav = useCallback(
    (id: string) => {
      openSession(id)
      setMobileNavOpen(false)
    },
    [openSession],
  )

  const onPin = useCallback(
    async (sessionId: string, agentId: string) => {
      const s = sessionById(sessionId)
      if (!s) return
      try {
        const res = await resumeSession(sessionId, { pinAgentId: agentId })
        ws.upsertSession(res.session)
        void ws.refreshSessions()
      } catch {
        /* surfaced via polling; keep UI responsive */
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [ws],
  )

  const onCloseSession = useCallback(
    async (id: string) => {
      closeTab(id)
      try {
        await deleteSession(id)
      } finally {
        ws.removeSession(id)
        void ws.refreshSessions()
      }
    },
    [closeTab, ws],
  )

  const activeSession = activeId ? sessionById(activeId) : undefined

  return (
    <div className="relative flex min-h-0 flex-1 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/40">
      {/* md+ : sidebar sits inline. below md : it's an overlay drawer. */}
      <div className="hidden md:flex">
        <Sidebar
          projects={ws.projects}
          sessions={ws.sessions}
          agents={agents}
          projectsState={ws.projectsState}
          activeSessionId={activeId}
          collapsed={collapsed}
          onToggleCollapse={() => setCollapsed((c) => !c)}
          onSelectSession={openSession}
          onNewSession={(p) => setNewTarget({ kind: 'project', project: p })}
          onNewDeviceSession={(a) => setNewTarget({ kind: 'device', agent: a })}
        />
      </div>

      {mobileNavOpen && (
        <div className="absolute inset-0 z-40 flex md:hidden">
          <Sidebar
            projects={ws.projects}
            sessions={ws.sessions}
            agents={agents}
            projectsState={ws.projectsState}
            activeSessionId={activeId}
            collapsed={false}
            onToggleCollapse={() => setMobileNavOpen(false)}
            onSelectSession={selectFromNav}
            onNewSession={(p) => {
              setNewTarget({ kind: 'project', project: p })
              setMobileNavOpen(false)
            }}
            onNewDeviceSession={(a) => {
              setNewTarget({ kind: 'device', agent: a })
              setMobileNavOpen(false)
            }}
          />
          <button
            type="button"
            aria-label="close projects"
            onClick={() => setMobileNavOpen(false)}
            className="flex-1 bg-black/50"
          />
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        {/* mobile-only bar to open the projects drawer */}
        <button
          type="button"
          onClick={() => setMobileNavOpen(true)}
          className="flex items-center gap-2 border-b border-zinc-800 bg-zinc-950/60 px-3 py-2 font-display text-xs font-semibold uppercase tracking-wider text-zinc-400 md:hidden"
        >
          <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
            <path d="M4 6h16M4 12h16M4 18h16" strokeLinecap="round" />
          </svg>
          projects
        </button>

        {openIds.length > 0 && (
          <TabStrip
            openIds={openIds}
            activeId={activeId}
            sessions={ws.sessions}
            onActivate={setActiveId}
            onClose={closeTab}
          />
        )}

        <div className="min-h-0 flex-1">
          {activeSession ? (
            <SessionPane
              key={activeSession.id}
              session={activeSession}
              agents={agents}
              onClose={() => onCloseSession(activeSession.id)}
              onPin={(agentId) => onPin(activeSession.id, agentId)}
            />
          ) : (
            <WorkspaceEmpty />
          )}
        </div>
      </div>

      {newTarget && (
        <NewSessionDialog
          target={newTarget}
          agents={agents}
          onClose={() => setNewTarget(null)}
          onCreated={onCreated}
        />
      )}
    </div>
  )
}

function TabStrip({
  openIds,
  activeId,
  sessions,
  onActivate,
  onClose,
}: {
  openIds: string[]
  activeId: string | null
  sessions: Session[]
  onActivate: (id: string) => void
  onClose: (id: string) => void
}) {
  return (
    <div className="term-scroll flex shrink-0 items-stretch gap-px overflow-x-auto border-b border-zinc-800 bg-zinc-950/70">
      {openIds.map((id) => {
        const s = sessions.find((x) => x.id === id)
        if (!s) return null
        const active = id === activeId
        const cls = statusDotClass(s.status)
        return (
          <div
            key={id}
            className={`group flex shrink-0 items-center gap-2 border-r border-zinc-800 px-3 py-2 ${
              active ? 'bg-zinc-900' : 'bg-transparent hover:bg-zinc-900/50'
            }`}
          >
            <button type="button" onClick={() => onActivate(id)} className="flex items-center gap-2">
              <span className="relative flex h-1.5 w-1.5 items-center justify-center">
                {statusPulses(s.status) && <span className={`absolute inline-flex h-full w-full rounded-full opacity-70 animate-breathe ${cls}`} />}
                <span className={`relative inline-flex h-1.5 w-1.5 rounded-full ${cls}`} />
              </span>
              <span className={`max-w-[12rem] truncate font-mono text-[11px] ${active ? 'text-zinc-100' : 'text-zinc-400'}`}>
                {s.title || (s.kind === 'claude' ? 'claude' : 'terminal')}
              </span>
            </button>
            <button
              type="button"
              onClick={() => onClose(id)}
              title="close tab"
              className="grid h-4 w-4 place-items-center rounded text-zinc-600 opacity-0 hover:bg-zinc-800 hover:text-zinc-300 group-hover:opacity-100"
            >
              <svg viewBox="0 0 24 24" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="2.4" aria-hidden>
                <path d="M6 6l12 12M18 6 6 18" strokeLinecap="round" />
              </svg>
            </button>
          </div>
        )
      })}
    </div>
  )
}

function WorkspaceEmpty() {
  return (
    <div className="grid h-full place-items-center px-6 text-center">
      <div className="max-w-sm">
        <div className="mx-auto grid h-14 w-14 place-items-center rounded-2xl border border-emerald-500/30 bg-emerald-500/10">
          <svg viewBox="0 0 24 24" className="h-7 w-7 text-emerald-400" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden>
            <path d="M12 2 22 7v10L12 22 2 17V7z" strokeLinejoin="round" />
            <path d="M2 7l10 5 10-5M12 12v10" strokeLinejoin="round" />
          </svg>
        </div>
        <h2 className="mt-5 font-display text-lg font-semibold text-zinc-100">Open a session to begin</h2>
        <p className="mt-1.5 font-mono text-[12px] leading-relaxed text-zinc-500">
          pick a project on the left, then start a Claude or terminal session.
          <br />
          the mesh auto-places it on the best machine — you can always override.
          <br />
          …or pick a device to work on that machine directly.
        </p>
      </div>
    </div>
  )
}

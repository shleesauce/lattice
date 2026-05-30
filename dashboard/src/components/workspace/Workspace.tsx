import { useCallback, useEffect, useMemo, useState } from 'react'
import type { Agent, CreateProjectResult, Project, Session, SessionWithPlacement } from '../../types'
import { useWorkspace } from '../../useWorkspace'
import { deleteSession, resumeSession } from '../../api'
import { Sidebar } from './Sidebar'
import { SessionPane } from './SessionPane'
import { ProjectFilesPanel } from './ProjectFilesPanel'
import { NewSessionDialog } from './NewSessionDialog'
import type { NewSessionTarget } from './NewSessionDialog'
import { NewProjectWizard } from './NewProjectWizard'
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
  const [wizardOpen, setWizardOpen] = useState(false)
  // Below md the sidebar overlays the pane instead of sitting beside it.
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  // Right-rail explorer target. Project rows pass the real project; device rows
  // pass a synthetic Project ({ name: hostname, path: '' }) that lists the
  // agent's home dir. `filesAgentId` overrides the browse agent for devices.
  const [filesProject, setFilesProject] = useState<Project | null>(null)
  const [filesAgentId, setFilesAgentId] = useState<string | null>(null)
  // Stable key for the active-files highlight in the sidebar.
  const [filesKey, setFilesKey] = useState<string | null>(null)
  // Collapse hides the rail while keeping the selection so it can be reopened.
  const [filesCollapsed, setFilesCollapsed] = useState(false)

  // The hub's co-located agent maps 1:1 to project paths; fall back to any
  // online agent if the local one isn't reporting yet.
  const localAgent = useMemo(
    () => agents.find((a) => a.local && a.online) ?? agents.find((a) => a.online) ?? null,
    [agents],
  )

  // For project browsing use the local agent; device browsing pins to that
  // device. Closes the rail if the chosen agent drops offline.
  const browseAgentId = filesAgentId ?? localAgent?.id ?? null
  const browseAgentOnline = browseAgentId ? agents.some((a) => a.id === browseAgentId && a.online) : false

  const openProjectFiles = useCallback((p: Project) => {
    setFilesProject(p)
    setFilesAgentId(null)
    setFilesKey(p.path)
    setFilesCollapsed(false)
  }, [])

  const openDeviceFiles = useCallback((a: Agent) => {
    setFilesProject({ name: a.hostname || a.name || a.id.slice(0, 8), path: '' })
    setFilesAgentId(a.id)
    setFilesKey(`device:${a.id}`)
    setFilesCollapsed(false)
  }, [])

  const closeFiles = useCallback(() => {
    setFilesProject(null)
    setFilesAgentId(null)
    setFilesKey(null)
  }, [])

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

  const onProjectCreated = useCallback(
    (res: CreateProjectResult) => {
      void ws.refreshProjects()
      if (res.session) {
        ws.upsertSession(res.session)
        void ws.refreshSessions()
        openSession(res.session.id)
        setMobileNavOpen(false)
      }
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
          activeFilesKey={filesKey}
          collapsed={collapsed}
          onToggleCollapse={() => setCollapsed((c) => !c)}
          onSelectSession={openSession}
          onNewSession={(p) => setNewTarget({ kind: 'project', project: p })}
          onNewDeviceSession={(a) => setNewTarget({ kind: 'device', agent: a })}
          onBeginNewProject={() => setWizardOpen(true)}
          onOpenProjectFiles={openProjectFiles}
          onOpenDeviceFiles={openDeviceFiles}
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
            activeFilesKey={filesKey}
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
            onBeginNewProject={() => {
              setWizardOpen(true)
              setMobileNavOpen(false)
            }}
            onOpenProjectFiles={(p) => {
              openProjectFiles(p)
              setMobileNavOpen(false)
            }}
            onOpenDeviceFiles={(a) => {
              openDeviceFiles(a)
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

      {/* Right rail (md+): persistent file explorer driven by project/device
          selection. Renders only when a target is chosen and a browse agent is
          online. Below md it overlays the pane like the projects drawer. */}
      {filesProject && browseAgentId && browseAgentOnline && !filesCollapsed && (
        <>
          <div className="hidden w-[clamp(320px,30vw,420px)] shrink-0 border-l border-zinc-800 md:block">
            <ProjectFilesPanel
              key={`${browseAgentId}:${filesProject.path}`}
              project={filesProject}
              agentId={browseAgentId}
              onClose={() => setFilesCollapsed(true)}
            />
          </div>

          <div className="absolute inset-0 z-40 flex md:hidden">
            <button
              type="button"
              aria-label="close files"
              onClick={() => setFilesCollapsed(true)}
              className="flex-1 bg-black/50"
            />
            <div className="w-[min(88vw,420px)] border-l border-zinc-800 bg-zinc-950">
              <ProjectFilesPanel
                key={`m:${browseAgentId}:${filesProject.path}`}
                project={filesProject}
                agentId={browseAgentId}
                onClose={() => setFilesCollapsed(true)}
              />
            </div>
          </div>
        </>
      )}

      {/* Collapsed-but-selected: a thin reopen tab on the right edge (md+). */}
      {filesProject && filesCollapsed && (
        <button
          type="button"
          onClick={() => setFilesCollapsed(false)}
          title="show files panel"
          className="absolute right-0 top-1/2 z-30 hidden -translate-y-1/2 items-center gap-1.5 rounded-l-md border border-r-0 border-zinc-800 bg-zinc-950/90 px-1.5 py-3 text-zinc-400 transition-colors hover:text-emerald-300 md:flex"
        >
          <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
            <path d="M15 6l-6 6 6 6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span className="font-display text-[10px] font-semibold uppercase tracking-[0.16em] [writing-mode:vertical-rl]">
            files
          </span>
        </button>
      )}

      {!browseAgentOnline && filesProject && !filesCollapsed && (
        <div className="hidden w-[clamp(320px,30vw,420px)] shrink-0 flex-col border-l border-zinc-800 bg-zinc-950/60 md:flex">
          <div className="flex items-center gap-2 border-b border-zinc-800 px-3 py-2.5">
            <span className="min-w-0 flex-1 truncate font-display text-[13px] font-semibold text-zinc-100">
              {filesProject.name}
            </span>
            <button
              type="button"
              onClick={closeFiles}
              title="close files panel"
              className="grid h-7 w-7 shrink-0 place-items-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"
            >
              <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
                <path d="M6 6l12 12M18 6 6 18" strokeLinecap="round" />
              </svg>
            </button>
          </div>
          <div className="grid flex-1 place-items-center px-6 text-center">
            <p className="font-mono text-[11px] leading-relaxed text-zinc-500">
              // no local agent online
              <br />
              to browse files
            </p>
          </div>
        </div>
      )}

      {newTarget && (
        <NewSessionDialog
          target={newTarget}
          agents={agents}
          onClose={() => setNewTarget(null)}
          onCreated={onCreated}
        />
      )}

      {wizardOpen && (
        <NewProjectWizard
          projects={ws.projects}
          onClose={() => setWizardOpen(false)}
          onCreated={onProjectCreated}
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

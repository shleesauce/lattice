import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { canHostClaude, type Agent, type CreateProjectResult, type Project, type Session, type SessionWithPlacement } from '../../types'
import type { WorkspaceState } from '../../useWorkspace'
import {
  createSession,
  deleteSessionForever,
  emptyTrash,
  resumeSession,
  setSessionArchived,
  setSessionDeleted,
  trashSession,
} from '../../api'
import { usePersisted } from '../../usePersisted'
import { parseHubError } from '../../lib/hubError'
import { Sidebar, type SidebarMode } from './Sidebar'
import { DockPanel, type DockView } from './DockPanel'
import { Icon } from '../../lattice/Icon'
import { ConfirmDialog } from './ConfirmDialog'
import { SessionPane } from './SessionPane'
import { NewSessionDialog } from './NewSessionDialog'
import type { NewSessionTarget } from './NewSessionDialog'
import { NewProjectWizard } from './NewProjectWizard'
import { statusDotClass, statusPulses } from './sessionMeta'

// Intents pushed from the ⌘K palette in App. Workspace owns the open-tab/active
// state, so the palette routes through this rather than lifting that state up.
export type WorkspaceIntent =
  | { kind: 'open-session'; sessionId: string; nonce: number }
  | { kind: 'open-project'; projectPath: string; nonce: number }
  | { kind: 'new-project'; nonce: number }

interface Props {
  agents: Agent[]
  // Shared server-state (projects / sessions / socket), owned once in App so
  // Fleet, the command palette, and the new-session dialog see the same list
  // Workspace mutates — no split-brain between two useWorkspace() instances.
  ws: WorkspaceState
  intent?: WorkspaceIntent | null
  onIntentConsumed?: () => void
  // Surface failures the user would otherwise never see (placement rejected,
  // create failed). Falls back to a no-op so the component stays standalone.
  onNotify?: (text: string, kind?: 'info' | 'error') => void
}

// The workspace shell: Projects→Sessions rail, open-session tabs, and the active
// session pane. Sidebar status comes from useWorkspace polling /api/sessions.
export function Workspace({ agents, ws, intent, onIntentConsumed, onNotify }: Props) {
  // Stable notifier so action callbacks can surface failures without churning
  // their dependency arrays each render.
  const notifyRef = useRef(onNotify)
  notifyRef.current = onNotify
  const notify = useCallback((text: string, kind: 'info' | 'error' = 'info') => {
    notifyRef.current?.(text, kind)
  }, [])
  // Persisted across refresh so your open tabs / active session come back.
  const [openIds, setOpenIds] = usePersisted<string[]>('lattice.ws.openIds', [])
  const [activeId, setActiveId] = usePersisted<string | null>('lattice.ws.activeId', null)
  const [collapsed, setCollapsed] = usePersisted<boolean>('lattice.ws.collapsed', false)
  // Right-side dock (Files / Terminal / Preview / Git), persisted across refresh.
  const [dockOpen, setDockOpen] = usePersisted<boolean>('lattice.ws.dockOpen', false)
  const [dockView, setDockView] = usePersisted<DockView>('lattice.ws.dockView', 'files')
  const [dockWidth, setDockWidth] = usePersisted<number>('lattice.ws.dockWidth', 380)
  const [newTarget, setNewTarget] = useState<NewSessionTarget | null>(null)
  const [wizardOpen, setWizardOpen] = useState(false)
  const [mode, setMode] = usePersisted<SidebarMode>('lattice.ws.mode', 'active')
  const [confirmKill, setConfirmKill] = useState<Session | null>(null)
  const [confirmEmpty, setConfirmEmpty] = useState(false)
  // Below md the sidebar overlays the pane instead of sitting beside it.
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  // The hub's co-located agent maps 1:1 to project paths; fall back to any
  // online agent if the local one isn't reporting yet.
  const localAgent = useMemo(
    () => agents.find((a) => a.local && a.online) ?? agents.find((a) => a.online) ?? null,
    [agents],
  )

  const sessionById = (id: string): Session | undefined => ws.sessions.find((s) => s.id === id)

  const openSession = useCallback((id: string) => {
    setOpenIds((prev) => (prev.includes(id) ? prev : [...prev, id]))
    setActiveId(id)
  }, [setOpenIds, setActiveId])

  const closeTab = useCallback(
    (id: string) => {
      setOpenIds((prev) => {
        const next = prev.filter((x) => x !== id)
        setActiveId((cur) => (cur === id ? next[next.length - 1] ?? null : cur))
        return next
      })
    },
    [setOpenIds, setActiveId],
  )

  // Drop tabs whose session disappeared from the backend, and keep activeId on a
  // real open tab. Guarded on `ready` so restored-from-localStorage tabs aren't
  // wiped during the initial empty-sessions load (they reattach once sessions land).
  useEffect(() => {
    if (ws.sessionsState !== 'ready') return
    const alive = (id: string) => ws.sessions.some((s) => s.id === id)
    setOpenIds((prev) => {
      const next = prev.filter(alive)
      if (next.length !== prev.length) {
        setActiveId((cur) => (cur && next.includes(cur) ? cur : next[next.length - 1] ?? null))
      }
      return next.length === prev.length ? prev : next
    })
  }, [ws.sessions, ws.sessionsState, setOpenIds, setActiveId])

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


  // The project "+" launches a single Claude session directly (no chooser, no
  // editor pair) — the Claude-desktop model: one click, one Claude session.
  const onNewClaude = useCallback(
    async (p: Project) => {
      try {
        const res = await createSession({
          kind: 'claude',
          scope: 'project',
          projectPath: p.path,
          title: p.name,
          userAgentId: localAgent?.id,
        })
        onCreated(res)
      } catch (e) {
        notify(`Couldn't start ${p.name} — ${parseHubError(e, 'the mesh rejected the session')}`, 'error')
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [localAgent, onCreated],
  )

  // One-click "Open Editor" for a device: reuse an existing device-scoped
  // editor session if one is live, otherwise create one.
  const onOpenDeviceEditor = useCallback(
    async (a: Agent) => {
      const existing = ws.sessions.find(
        (s) => s.kind === 'editor' && s.scope === 'device' && s.agentId === a.id && s.status !== 'exited',
      )
      if (existing) {
        openSession(existing.id)
        setMobileNavOpen(false)
        return
      }
      try {
        const res = await createSession({
          kind: 'editor',
          scope: 'device',
          pinAgentId: a.id,
          userAgentId: a.id,
          title: a.hostname || a.name || a.id.slice(0, 8),
        })
        onCreated(res)
      } catch (e) {
        notify(`Couldn't open the editor on ${a.hostname || a.name} — ${parseHubError(e, 'create failed')}`, 'error')
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [ws.sessions, openSession, onCreated],
  )

  const onProjectCreated = useCallback(
    (res: CreateProjectResult) => {
      void ws.refreshProjects()
      // Scaffolding warnings (registry write skipped, KB stub failed, …) are
      // real but non-fatal — surface them instead of silently dropping them.
      if (res.warnings?.length) notify(`Project created with notes: ${res.warnings.join(' · ')}`, 'error')
      if (res.session) {
        ws.upsertSession(res.session)
        void ws.refreshSessions()
        openSession(res.session.id)
        setMobileNavOpen(false)
      }
    },
    [ws, openSession, notify],
  )

  const selectFromNav = useCallback(
    (id: string) => {
      openSession(id)
      setMobileNavOpen(false)
    },
    [openSession],
  )

  // Reconnect an orphaned session on its OWN machine (resume — keeps context).
  const onReconnect = useCallback(
    async (sessionId: string, agentId: string) => {
      const s = sessionById(sessionId)
      if (!s) return
      try {
        const res = await resumeSession(sessionId, { pinAgentId: agentId })
        ws.upsertSession(res.session)
        void ws.refreshSessions()
      } catch (e) {
        notify(`Couldn't reconnect the session — ${parseHubError(e, 'resume failed')}`, 'error')
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [ws],
  )

  // Switch a session to a different machine. A session is pinned for life (D32),
  // so "switching" = start a fresh session on the chosen machine and drop the old
  // one. Intended for use BEFORE you've started the conversation; the new session
  // begins clean on the new box. If create fails the old session is left intact.
  const onPickMachine = useCallback(
    async (sessionId: string, agentId: string) => {
      const s = sessionById(sessionId)
      if (!s || s.agentId === agentId) return
      try {
        const res = await createSession({
          kind: s.kind,
          scope: 'project',
          projectPath: s.projectPath,
          title: s.title,
          pinAgentId: agentId,
          userAgentId: localAgent?.id,
        })
        // Open the new session FIRST, then retire the old one. Use trash (not
        // delete-forever): it ends the old process AND is recoverable, so a
        // mis-click on a session that already has a conversation never destroys
        // it permanently. Clean up local state immediately to avoid a flash of
        // the dead tab while the next poll catches up.
        ws.upsertSession(res.session)
        openSession(res.session.id)
        setMobileNavOpen(false)
        await trashSession(s.id)
        closeTab(s.id)
        ws.removeSession(s.id)
        void ws.refreshSessions()
      } catch (e) {
        // create failed (e.g. machine ineligible): old session left intact.
        notify(`Couldn't move the session — ${parseHubError(e, 'that machine can\'t host it')}`, 'error')
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [ws, localAgent, openSession, closeTab],
  )

  // Closing a tab (the X) just removes the tab — it does NOT end or delete the
  // session. Lifecycle changes go through archive / trash / delete below.
  const onCloseSession = useCallback((id: string) => closeTab(id), [closeTab])

  // Archive (hide, keep) or unarchive a session. Archiving drops its open tab.
  const onArchiveSession = useCallback(
    async (id: string, archived: boolean) => {
      if (archived) closeTab(id)
      try {
        const updated = await setSessionArchived(id, archived)
        ws.upsertSession(updated)
      } catch (e) {
        notify(`Couldn't ${archived ? 'archive' : 'unarchive'} the session — ${parseHubError(e, 'try again')}`, 'error')
      } finally {
        void ws.refreshSessions()
      }
    },
    [closeTab, ws, notify],
  )

  // Delete = move to Trash: ends the process, hides it, recoverable for 30 days.
  const onTrashSession = useCallback(
    async (id: string) => {
      closeTab(id)
      try {
        await trashSession(id)
      } catch (e) {
        notify(`Couldn't move the session to Trash — ${parseHubError(e, 'try again')}`, 'error')
      } finally {
        void ws.refreshSessions()
      }
    },
    [closeTab, ws, notify],
  )

  // Restore a session out of Trash back into the active workspace.
  const onRestoreTrash = useCallback(
    async (id: string) => {
      try {
        const updated = await setSessionDeleted(id, false)
        ws.upsertSession(updated)
      } catch (e) {
        notify(`Couldn't restore the session — ${parseHubError(e, 'try again')}`, 'error')
      } finally {
        void ws.refreshSessions()
      }
    },
    [ws, notify],
  )

  // Permanently delete (from Trash) — confirmed via the in-app dialog.
  const onDeleteForever = useCallback(
    async (id: string) => {
      closeTab(id)
      try {
        await deleteSessionForever(id)
      } catch (e) {
        notify(`Couldn't delete the session — ${parseHubError(e, 'try again')}`, 'error')
      } finally {
        ws.removeSession(id)
        void ws.refreshSessions()
      }
    },
    [closeTab, ws, notify],
  )

  // Empty Trash — permanently delete every trashed session (confirmed in-app).
  const onEmptyTrash = useCallback(async () => {
    try {
      await emptyTrash()
    } catch (e) {
      notify(`Couldn't empty Trash — ${parseHubError(e, 'try again')}`, 'error')
    } finally {
      void ws.refreshSessions()
    }
  }, [ws, notify])

  // Consume a palette intent once (keyed by nonce). Open-project reuses a live
  // session for that path if one exists, else starts a Claude session there.
  // The Workspace mounts fresh when you cross over from Fleet, so its session /
  // project lists arrive async — gate on `ready` and re-run when data lands
  // rather than consuming the intent against an empty list.
  const lastIntentNonce = useRef<number>(0)
  useEffect(() => {
    if (!intent || intent.nonce === lastIntentNonce.current) return
    if (intent.kind === 'open-session') {
      if (ws.sessionsState !== 'ready') return
      if (ws.sessions.some((s) => s.id === intent.sessionId)) openSession(intent.sessionId)
    } else if (intent.kind === 'open-project') {
      if (ws.sessionsState !== 'ready' || ws.projectsState !== 'ready') return
      const live = ws.sessions.find(
        (s) => s.projectPath === intent.projectPath && s.status !== 'exited' && !s.archived && !s.deletedAt,
      )
      if (live) openSession(live.id)
      else {
        const p = ws.projects.find((x) => x.path === intent.projectPath)
        if (p) void onNewClaude(p)
      }
    } else if (intent.kind === 'new-project') {
      setWizardOpen(true)
    }
    lastIntentNonce.current = intent.nonce
    onIntentConsumed?.()
  }, [intent, ws.sessions, ws.projects, ws.sessionsState, ws.projectsState, openSession, onNewClaude, onIntentConsumed])

  const activeSession = activeId ? sessionById(activeId) : undefined

  // ─────────── D29: pair a Claude chat to the editor (chrome-first) ───────────
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents])

  // The project's live Claude session on the SAME machine as the editor (so the
  // AI edits the same files). Project editors match by projectPath; device
  // editors match by device scope. Undefined ⇒ none yet (the effect creates one).
  const pairedClaudeFor = useCallback(
    (ed: Session): Session | undefined =>
      ws.sessions.find(
        (s) =>
          s.kind === 'claude' &&
          s.status !== 'exited' &&
          s.agentId === ed.agentId &&
          (ed.scope === 'device'
            ? s.scope === 'device'
            : s.scope !== 'device' && s.projectPath === ed.projectPath),
      ),
    [ws.sessions],
  )

  // Auto create-or-reuse the paired Claude when an editor is active and its
  // machine has claude (D29: split open by default). Guarded so we never spawn
  // two for one editor.
  const pairingInFlight = useRef<Set<string>>(new Set())
  useEffect(() => {
    const ed = activeSession
    if (!ed || ed.kind !== 'editor') return
    // Only pair a Claude side-pane when the editor's machine can actually run claude
    // (installed AND authable — F14); otherwise we'd auto-spawn a dead blank claude.
    if (!canHostClaude(agentById.get(ed.agentId))) return
    if (pairedClaudeFor(ed)) return
    if (pairingInFlight.current.has(ed.id)) return
    pairingInFlight.current.add(ed.id)
    ;(async () => {
      try {
        const res = await createSession({
          kind: 'claude',
          scope: ed.scope,
          projectPath: ed.scope === 'device' ? undefined : ed.projectPath,
          pinAgentId: ed.agentId,
          userAgentId: ed.agentId,
          title: `${ed.title || 'project'} · ai`,
        })
        ws.upsertSession(res.session)
        void ws.refreshSessions()
      } catch {
        /* transient — the effect retries on the next sessions poll */
      } finally {
        pairingInFlight.current.delete(ed.id)
      }
    })()
  }, [activeSession, agentById, pairedClaudeFor, ws])

  const editorPaired =
    activeSession?.kind === 'editor'
      ? {
          pairedClaudeId: pairedClaudeFor(activeSession)?.id ?? null,
          editorAgentHasClaude: canHostClaude(agentById.get(activeSession.agentId)),
        }
      : { pairedClaudeId: null, editorAgentHasClaude: false }

  // Drag the dock's left edge to resize; clamped so the session pane keeps room.
  const paneRowRef = useRef<HTMLDivElement>(null)
  const startDockDrag = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault()
      const row = paneRowRef.current
      if (!row) return
      const onMove = (ev: MouseEvent) => {
        const rect = row.getBoundingClientRect()
        setDockWidth(Math.min(Math.max(rect.right - ev.clientX, 280), rect.width - 360))
      }
      const onUp = () => {
        window.removeEventListener('mousemove', onMove)
        window.removeEventListener('mouseup', onUp)
        document.body.style.userSelect = ''
      }
      document.body.style.userSelect = 'none'
      window.addEventListener('mousemove', onMove)
      window.addEventListener('mouseup', onUp)
    },
    [setDockWidth],
  )

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
          mode={mode}
          onMode={setMode}
          onToggleCollapse={() => setCollapsed((c) => !c)}
          onSelectSession={openSession}
          onNewSession={(p) => setNewTarget({ kind: 'project', project: p })}
          onNewClaude={onNewClaude}
          onNewDeviceSession={(a) => setNewTarget({ kind: 'device', agent: a })}
          onBeginNewProject={() => setWizardOpen(true)}
          onOpenDeviceEditor={onOpenDeviceEditor}
          onArchiveSession={onArchiveSession}
          onTrashSession={onTrashSession}
          onRestoreTrash={onRestoreTrash}
          onDeleteForever={(s) => setConfirmKill(s)}
          onEmptyTrash={() => setConfirmEmpty(true)}
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
            mode={mode}
            onMode={setMode}
            onToggleCollapse={() => setMobileNavOpen(false)}
            onSelectSession={selectFromNav}
            onNewSession={(p) => {
              setNewTarget({ kind: 'project', project: p })
              setMobileNavOpen(false)
            }}
            onNewClaude={(p) => {
              void onNewClaude(p)
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
            onOpenDeviceEditor={(a) => {
              void onOpenDeviceEditor(a)
              setMobileNavOpen(false)
            }}
            onArchiveSession={onArchiveSession}
            onTrashSession={onTrashSession}
            onRestoreTrash={onRestoreTrash}
            onDeleteForever={(s) => setConfirmKill(s)}
            onEmptyTrash={() => setConfirmEmpty(true)}
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
            dockOpen={dockOpen}
            onToggleDock={() => setDockOpen((o) => !o)}
          />
        )}

        <div ref={paneRowRef} className="flex min-h-0 flex-1">
          <div className="relative min-w-0 flex-1">
            {activeSession ? (
              <SessionPane
                key={activeSession.id}
                session={activeSession}
                agents={agents}
                onClose={() => onCloseSession(activeSession.id)}
                onReconnect={(agentId) => onReconnect(activeSession.id, agentId)}
                onPickMachine={(agentId) => onPickMachine(activeSession.id, agentId)}
                pairedClaudeId={editorPaired.pairedClaudeId}
                editorAgentHasClaude={editorPaired.editorAgentHasClaude}
              />
            ) : (
              <WorkspaceEmpty onNew={() => setNewTarget({ kind: 'project', project: null })} />
            )}
          </div>

          {activeSession && dockOpen && (
            <>
              <div className="dock-resize" onMouseDown={startDockDrag} title="drag to resize">
                <span className="dock-resize-grip" />
              </div>
              <div className="dock-wrap" style={{ width: dockWidth }}>
                <DockPanel
                  session={activeSession}
                  agents={agents}
                  view={dockView}
                  onView={setDockView}
                  onClose={() => setDockOpen(false)}
                />
              </div>
            </>
          )}
        </div>
      </div>

      {newTarget && (
        <NewSessionDialog
          target={newTarget}
          agents={agents}
          projects={ws.projects}
          projectsState={ws.projectsState}
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

      {confirmKill && (
        <ConfirmDialog
          title="Delete forever?"
          body={`"${confirmKill.title || confirmKill.kind}" will be permanently deleted. This can't be undone.`}
          confirmLabel="Delete forever"
          danger
          onConfirm={() => onDeleteForever(confirmKill.id)}
          onClose={() => setConfirmKill(null)}
        />
      )}

      {confirmEmpty && (
        <ConfirmDialog
          title="Empty Trash?"
          body="Every session in Trash will be permanently deleted. This can't be undone."
          confirmLabel="Empty Trash"
          danger
          onConfirm={onEmptyTrash}
          onClose={() => setConfirmEmpty(false)}
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
  dockOpen,
  onToggleDock,
}: {
  openIds: string[]
  activeId: string | null
  sessions: Session[]
  onActivate: (id: string) => void
  onClose: (id: string) => void
  dockOpen: boolean
  onToggleDock: () => void
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
                {s.title || s.kind}
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
      {/* Right-dock toggle — Claude-Code-desktop-style panel switch. */}
      <button
        type="button"
        onClick={onToggleDock}
        title={dockOpen ? 'hide panel' : 'show panel (Files · Terminal · Preview · Git)'}
        className={`ml-auto flex shrink-0 items-center gap-1.5 self-center rounded-md px-2.5 py-1 font-mono text-[11px] transition-colors ${
          dockOpen ? 'text-teal-300' : 'text-zinc-500 hover:text-zinc-200'
        }`}
        style={{ marginRight: 6 }}
      >
        <Icon name="panel-left" size={15} style={{ transform: 'scaleX(-1)' }} />
      </button>
    </div>
  )
}

function WorkspaceEmpty({ onNew }: { onNew: () => void }) {
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
        <div className="mt-3 flex flex-col gap-2.5 font-mono text-[12px] leading-[1.6] text-zinc-500">
          <p className="[text-wrap:balance]">Start a session in any of your projects. The mesh places it on the best machine, and you can always override.</p>
          <p className="[text-wrap:balance]">Or pick a device on the left to work on that machine directly.</p>
        </div>
        <button type="button" className="btn btn-primary" style={{ marginTop: 20 }} onClick={onNew}>
          New session
        </button>
        <p className="mt-5 font-mono text-[11px] text-zinc-600">
          press <kbd className="rounded border border-zinc-700 bg-zinc-800/60 px-1.5 py-0.5 text-zinc-400">⌘K</kbd> to jump anywhere
        </p>
      </div>
    </div>
  )
}

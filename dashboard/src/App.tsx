/* App root — unified fleet + workspace. Fleet view is the live mesh control
   room over /api/devices (agents + Tailscale + SSH: Macs, PCs, phones).
   Workspace view is the real session workspace (Claude / terminal / editor). */
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { useFleet, type ConnState } from './useFleet'
import { useDevices } from './useDevices'
import { useWorkspace } from './useWorkspace'
import { usePersisted } from './usePersisted'
import { getAuthStatus, getSetupStatus, wakeAgent } from './api'
import type { AuthStatus, SetupStatus } from './types'
import { FirstRunWizard } from './components/FirstRunWizard'
import { Login } from './components/Login'
import { devicesToMachines, isWoven, type Machine } from './lattice/adapt'
import { Fleet } from './lattice/Fleet'
import type { WorkspaceIntent } from './components/workspace/Workspace'
// The Workspace tree pulls xterm.js + the markdown stack (the bulk of the
// bundle). The default view is Fleet, so split it out and load on first visit.
const Workspace = lazy(() =>
  import('./components/workspace/Workspace').then((m) => ({ default: m.Workspace })),
)
import { NewSessionDialog, type NewSessionTarget } from './components/workspace/NewSessionDialog'
import { CommandPalette } from './components/CommandPalette'
import { SettingsPanel } from './components/SettingsPanel'
import { ManageMesh } from './components/ManageMesh'
import { Icon } from './lattice/Icon'
import { Dot } from './lattice/primitives'
import logoMark from './design/logo-mark.svg'

type View = 'fleet' | 'workspace'

// Root gate: the dashboard is locked behind the first-run wizard (until the hub
// is configured), then the login screen (when auth is required and this browser
// isn't authenticated). This component runs ONLY the setup + auth checks so the
// heavy Dashboard hooks below never run conditionally. Both fetches fail OPEN on a
// network error (assume configured / no auth required) so a transient blip can't
// lock the user out.
export default function App() {
  const [setup, setSetup] = useState<SetupStatus | undefined>(undefined) // undefined = loading
  const [auth, setAuth] = useState<AuthStatus | undefined>(undefined) // undefined = loading/not-yet-checked

  // Effect 1: setup status.
  useEffect(() => {
    getSetupStatus()
      .then(setSetup)
      .catch(() => setSetup({ needsSetup: false }))
  }, [])

  // Effect 2: auth status — only once setup is resolved AND not needed, so a
  // first-run hub never makes the extra call.
  useEffect(() => {
    if (!setup || setup.needsSetup) return
    getAuthStatus()
      .then(setAuth)
      .catch(() => setAuth({ authRequired: false, authenticated: false }))
  }, [setup])

  if (setup === undefined) return <SetupSplash />
  if (setup.needsSetup) return <FirstRunWizard status={setup} onDone={() => window.location.reload()} />
  if (auth === undefined) return <SetupSplash />
  if (auth.authRequired && !auth.authenticated) return <Login onAuthed={() => window.location.reload()} />
  return <Dashboard />
}

// Minimal centered splash while the setup status loads — same aesthetic as the
// Suspense fallback below.
function SetupSplash() {
  return (
    <div className="grid h-screen place-items-center" style={{ background: 'var(--base)' }}>
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 14 }}>
        <img src={logoMark} alt="" style={{ width: 40, height: 40, opacity: 0.9 }} />
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--fg-3)' }}>weaving…</span>
      </div>
    </div>
  )
}

function Dashboard() {
  const { agents, health, conn } = useFleet()
  const { devices, refetch: refetchDevices } = useDevices()
  const ws = useWorkspace()
  // Persisted across refresh so the app reopens where you left it.
  const [view, setView] = usePersisted<View>('lattice.view', 'fleet')
  const [selectedId, setSelectedId] = usePersisted<string | null>('lattice.fleet.selected', null)
  const [wakingIds, setWakingIds] = useState<Set<string>>(new Set())
  const [newTarget, setNewTarget] = useState<NewSessionTarget | null>(null)
  const [toast, setToast] = useState<{ text: string; kind: 'info' | 'error' } | null>(null)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [manageMeshOpen, setManageMeshOpen] = useState(false)
  // Intent bus → Workspace (open a session/project, launch the new-project wizard).
  // Workspace owns its own session/tab state, so the palette routes through this.
  const [wsIntent, setWsIntent] = useState<WorkspaceIntent | null>(null)
  const intentNonce = useRef(0)
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const wakeTimers = useRef<Set<ReturnType<typeof setTimeout>>>(new Set())

  // Clear any pending timers on unmount so they don't fire setState on a
  // torn-down component (logout / auth transition reloads the Dashboard).
  useEffect(
    () => () => {
      clearTimeout(toastTimer.current)
      wakeTimers.current.forEach(clearTimeout)
      wakeTimers.current.clear()
    },
    [],
  )

  const flash = (text: string, kind: 'info' | 'error' = 'info') => {
    setToast({ text, kind })
    clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(null), kind === 'error' ? 5000 : 3000)
  }

  // ⌘K / Ctrl-K toggles the command palette from anywhere.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault()
        setPaletteOpen((o) => !o)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // Route a palette action into the Workspace and switch to it. The nonce makes
  // repeated identical intents (e.g. open the same project twice) re-fire.
  // (A plain Omit<WorkspaceIntent,'nonce'> collapses the union to common keys.)
  const routeToWorkspace = (
    intent:
      | { kind: 'open-session'; sessionId: string }
      | { kind: 'open-project'; projectPath: string }
      | { kind: 'new-project' },
  ) => {
    setWsIntent({ ...intent, nonce: ++intentNonce.current } as WorkspaceIntent)
    setView('workspace')
  }

  // Unified device list → mesh machines, with in-flight wakes shown as "starting".
  const machines = useMemo(() => {
    const base = devicesToMachines(devices, ws.sessions)
    if (wakingIds.size === 0) return base
    return base.map((m) => (wakingIds.has(m.id) && m.offline ? { ...m, status: 'starting', offline: false } : m))
  }, [devices, ws.sessions, wakingIds])

  const onlineCount = machines.filter((m) => m.online).length
  // "Woven" counts only live-agent mesh nodes — not reachable-only phones/dead
  // agents (F2). onlineCount stays broader (any reachable host) for Settings.
  const wovenCount = machines.filter(isWoven).length
  const liveSessions = ws.sessions.filter((s) => s.status === 'live').length
  const senderId = useMemo(() => agents.find((a) => a.online)?.id ?? null, [agents])

  // Default-select the hub/local machine (or first online) once devices land.
  useEffect(() => {
    if (selectedId && machines.some((m) => m.id === selectedId)) return
    const first = machines.find((m) => m.locality === 0) ?? machines.find((m) => m.online) ?? machines[0]
    setSelectedId(first ? first.id : null)
  }, [machines, selectedId, setSelectedId])

  // Recent projects = those with an active session, else first few.
  const recentProjects = useMemo(() => {
    const active = ws.sessions.filter((s) => s.status !== 'exited' && s.projectPath)
    const names: string[] = []
    for (const s of active) {
      const p = ws.projects.find((x) => x.path === s.projectPath)
      const name = p?.name ?? s.projectPath.split('/').pop() ?? ''
      if (name && !names.includes(name)) names.push(name)
    }
    if (names.length === 0) ws.projects.slice(0, 4).forEach((p) => names.push(p.name))
    return names.slice(0, 5)
  }, [ws.sessions, ws.projects])

  const onWake = (m: Machine) => {
    if (!m.mac || !senderId) return
    setWakingIds((s) => new Set(s).add(m.id))
    flash(`Waking ${m.label} → routing power through the mesh`)
    void wakeAgent(senderId, m.mac).catch(() => {})
    const t = setTimeout(() => {
      wakeTimers.current.delete(t)
      setWakingIds((s) => {
        const n = new Set(s)
        n.delete(m.id)
        return n
      })
    }, 9000)
    wakeTimers.current.add(t)
  }

  // Start a session on a device. Only agent-backed machines can host a lattice
  // session; for others we surface their SSH reach instead.
  const onNewSession = (m: Machine) => {
    if (!m.hasAgent || !m.agentId) {
      if (m.sshAlias) flash(`${m.label}: ssh ${m.sshAlias}  (no lattice agent — install to run sessions here)`)
      else flash(`${m.label} has no lattice agent — install it to run sessions here`)
      return
    }
    const a = agents.find((x) => x.id === m.agentId)
    if (!a) return
    setNewTarget({ kind: 'device', agent: a })
  }

  return (
    <div className="flex h-screen min-h-0 flex-col" style={{ background: 'var(--base)' }}>
      <TopBar
        view={view}
        onView={setView}
        conn={conn}
        version={health?.version}
        alive={liveSessions}
        woven={wovenCount}
        onSearch={() => setPaletteOpen(true)}
        onSettings={() => setSettingsOpen(true)}
      />

      {conn === 'down' && (
        <div className="conn-banner">
          <Icon name="wifi-off" size={14} />
          Hub unreachable — reconnecting…
          <span className="conn-banner-sub">showing last-known state</span>
        </div>
      )}

      <main className="flex min-h-0 flex-1 flex-col">
        {view === 'fleet' ? (
          <Fleet
            machines={machines}
            selected={selectedId ?? ''}
            recentProjects={recentProjects}
            connLabel={conn === 'live' ? 'mesh' : conn === 'connecting' ? 'linking' : 'offline'}
            canWake={!!senderId}
            onManageMesh={() => setManageMeshOpen(true)}
            onSelect={setSelectedId}
            onWake={onWake}
            onNewSession={onNewSession}
            onOpenWorkspace={() => setView('workspace')}
            onOpenProject={(name) => {
              const p = ws.projects.find((x) => x.name === name)
              if (p) routeToWorkspace({ kind: 'open-project', projectPath: p.path })
              else setView('workspace')
            }}
            onOpenSession={(id) => routeToWorkspace({ kind: 'open-session', sessionId: id })}
          />
        ) : (
          <Suspense fallback={<WorkspaceLoading />}>
            <Workspace
              agents={agents}
              ws={ws}
              intent={wsIntent}
              onIntentConsumed={() => setWsIntent(null)}
              onNotify={flash}
            />
          </Suspense>
        )}
      </main>

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        view={view}
        machines={machines}
        projects={ws.projects}
        sessions={ws.sessions}
        canWake={!!senderId}
        onGoFleet={() => setView('fleet')}
        onGoWorkspace={() => setView('workspace')}
        onFocusMachine={(id) => {
          setSelectedId(id)
          setView('fleet')
        }}
        onWakeMachine={onWake}
        onOpenSession={(id) => routeToWorkspace({ kind: 'open-session', sessionId: id })}
        onOpenProject={(path) => routeToWorkspace({ kind: 'open-project', projectPath: path })}
        onNewSession={() => {
          setView('workspace')
          setNewTarget({ kind: 'project', project: null })
        }}
        onNewProject={() => routeToWorkspace({ kind: 'new-project' })}
        onOpenSettings={() => setSettingsOpen(true)}
      />

      {manageMeshOpen && (
        <ManageMesh
          devices={devices}
          onClose={() => setManageMeshOpen(false)}
          onChanged={() => void refetchDevices()}
        />
      )}

      {settingsOpen && (
        <SettingsPanel
          agents={agents}
          version={health?.version}
          onlineCount={onlineCount}
          totalCount={machines.length}
          onClose={() => setSettingsOpen(false)}
        />
      )}

      {newTarget && (
        <NewSessionDialog
          target={newTarget}
          agents={agents}
          projects={ws.projects}
          projectsState={ws.projectsState}
          onClose={() => setNewTarget(null)}
          onCreated={(res) => {
            setNewTarget(null)
            ws.upsertSession(res.session)
            void ws.refreshSessions()
            setView('workspace')
            flash(`Started ${res.session.title || res.session.kind}`)
          }}
        />
      )}

      {toast && (
        <div className={`toast${toast.kind === 'error' ? ' err' : ''}`}>
          <Dot status={toast.kind === 'error' ? 'danger' : 'live'} />
          {toast.text}
        </div>
      )}
    </div>
  )
}

// Suspense fallback while the lazy Workspace chunk (xterm + markdown) loads.
function WorkspaceLoading() {
  return (
    <div className="grid h-full place-items-center" style={{ background: 'var(--base)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, color: 'var(--fg-3)', fontFamily: 'var(--font-mono)', fontSize: 12 }}>
        <Icon name="refresh-cw" size={15} style={{ animation: 'spin 1s linear infinite' }} />
        weaving the workspace…
      </div>
    </div>
  )
}

function TopBar({
  view,
  onView,
  conn,
  version,
  alive,
  woven,
  onSearch,
  onSettings,
}: {
  view: View
  onView: (v: View) => void
  conn: ConnState
  version?: string
  alive: number
  woven: number
  onSearch: () => void
  onSettings: () => void
}) {
  const connInfo: Record<ConnState, { cls: string; text: string }> = {
    live: { cls: 'live', text: 'hub live' },
    connecting: { cls: 'starting', text: 'connecting' },
    down: { cls: 'danger', text: 'hub down' },
  }
  const c = connInfo[conn]
  return (
    <header className="topbar">
      <img src={logoMark} alt="" style={{ width: 24, height: 24 }} />
      <span style={{ fontSize: 17, fontWeight: 700, letterSpacing: '-.03em', color: 'var(--fg-1)' }}>lattice</span>

      <button
        type="button"
        className="seg"
        role="switch"
        aria-checked={view === 'workspace'}
        aria-label={`Switch to ${view === 'fleet' ? 'Workspace' : 'Fleet'} view`}
        title={`Switch to ${view === 'fleet' ? 'Workspace' : 'Fleet'}`}
        onClick={() => onView(view === 'fleet' ? 'workspace' : 'fleet')}
        style={{ marginLeft: 6 }}
      >
        <span className={view === 'fleet' ? 'on' : ''}>
          <Icon name="layers" /> Fleet
        </span>
        <span className={view === 'workspace' ? 'on' : ''}>
          <Icon name="terminal" /> Workspace
        </span>
      </button>

      <div className="tb-stat" style={{ marginLeft: 4 }}>
        <Dot status="live" />
        <span style={{ color: 'var(--green)' }}>{alive} alive</span>
        <span style={{ color: 'var(--fg-3)' }}>·</span>
        <span>{woven} woven</span>
      </div>

      <div className="tb-spacer" />

      <button type="button" className="tb-search" onClick={onSearch} title="Search the mesh (⌘K)">
        <Icon name="search" size={14} color="var(--fg-3)" />
        <span style={{ flex: 1, textAlign: 'left' }}>Search the mesh</span>
        <span className="mono" style={{ fontSize: 11, color: 'var(--fg-3)' }}>⌘K</span>
      </button>

      <div className="tb-stat" title={`${c.text}${version ? ` · lattice ${version}` : ' · version unknown'}`}>
        <span className={`dot ${c.cls}`} />
        <span style={{ fontSize: 11 }}>{c.text}</span>
      </div>

      <button type="button" className="iconbtn" title="settings" onClick={onSettings}>
        <Icon name="settings" size={17} />
      </button>
    </header>
  )
}

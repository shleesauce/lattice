/* App root — real fleet + workspace. Fleet view is the live mesh control room;
   Workspace view is the real session workspace (Claude / terminal / editor). */
import { useEffect, useMemo, useRef, useState } from 'react'
import { useFleet, type ConnState } from './useFleet'
import { useWorkspace } from './useWorkspace'
import { wakeAgent } from './api'
import { agentsToMachines } from './lattice/adapt'
import { Fleet } from './lattice/Fleet'
import { Workspace } from './components/workspace/Workspace'
import { NewSessionDialog, type NewSessionTarget } from './components/workspace/NewSessionDialog'
import { Icon } from './lattice/Icon'
import { Dot } from './lattice/primitives'
import logoMark from './design/logo-mark.svg'

type View = 'fleet' | 'workspace'

export default function App() {
  const { agents, health, conn } = useFleet()
  const ws = useWorkspace()
  const [view, setView] = useState<View>('fleet')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [wakingIds, setWakingIds] = useState<Set<string>>(new Set())
  const [newTarget, setNewTarget] = useState<NewSessionTarget | null>(null)
  const [toast, setToast] = useState<string | null>(null)
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const flash = (text: string) => {
    setToast(text)
    clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(null), 3000)
  }

  // Real machines for the mesh map, with any in-flight wakes shown as "starting".
  const machines = useMemo(() => {
    const base = agentsToMachines(agents, ws.sessions)
    if (wakingIds.size === 0) return base
    return base.map((m) => (wakingIds.has(m.id) && m.offline ? { ...m, status: 'starting', offline: false } : m))
  }, [agents, ws.sessions, wakingIds])

  const onlineCount = agents.filter((a) => a.online).length
  const liveSessions = ws.sessions.filter((s) => s.status === 'live').length
  const senderId = useMemo(() => agents.find((a) => a.online)?.id ?? null, [agents])

  // Default-select the hub/local machine (or first) once the fleet lands.
  useEffect(() => {
    if (selectedId && machines.some((m) => m.id === selectedId)) return
    const first = machines.find((m) => m.locality === 0) ?? machines.find((m) => m.online) ?? machines[0]
    setSelectedId(first ? first.id : null)
  }, [machines, selectedId])

  // Recent projects = those with an active session, else the first few.
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

  const onWake = (mId: string) => {
    const m = machines.find((x) => x.id === mId)
    if (!m || !m.mac || !senderId) return
    setWakingIds((s) => new Set(s).add(m.id))
    flash(`Waking ${m.label} → routing power through the mesh`)
    void wakeAgent(senderId, m.mac).catch(() => {})
    setTimeout(() => {
      setWakingIds((s) => {
        const n = new Set(s)
        n.delete(m.id)
        return n
      })
    }, 9000)
  }

  const openDeviceSession = (mId: string) => {
    const a = agents.find((x) => x.id === mId)
    if (!a) return
    if (!a.online) {
      onWake(mId)
      return
    }
    setNewTarget({ kind: 'device', agent: a })
  }

  return (
    <div className="flex h-screen min-h-0 flex-col" style={{ background: 'var(--base)' }}>
      <TopBar view={view} onView={setView} conn={conn} version={health?.version} alive={liveSessions} woven={onlineCount} />

      <main className="flex min-h-0 flex-1 flex-col">
        {view === 'fleet' ? (
          <Fleet
            machines={machines}
            selected={selectedId ?? ''}
            recentProjects={recentProjects}
            connLabel={conn === 'live' ? 'mesh' : conn === 'connecting' ? 'linking' : 'offline'}
            canWake={!!senderId}
            onSelect={setSelectedId}
            onWake={(m) => onWake(m.id)}
            onNewSession={(m) => openDeviceSession(m.id)}
            onOpenWorkspace={() => setView('workspace')}
          />
        ) : (
          <Workspace agents={agents} />
        )}
      </main>

      {newTarget && (
        <NewSessionDialog
          target={newTarget}
          agents={agents}
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
        <div className="toast">
          <Dot status="live" />
          {toast}
        </div>
      )}
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
}: {
  view: View
  onView: (v: View) => void
  conn: ConnState
  version?: string
  alive: number
  woven: number
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

      <div className="seg" style={{ marginLeft: 6 }}>
        <button type="button" className={view === 'fleet' ? 'on' : ''} onClick={() => onView('fleet')}>
          <Icon name="layers" /> Fleet
        </button>
        <button type="button" className={view === 'workspace' ? 'on' : ''} onClick={() => onView('workspace')}>
          <Icon name="terminal" /> Workspace
        </button>
      </div>

      <div className="tb-stat" style={{ marginLeft: 4 }}>
        <Dot status="live" />
        <span style={{ color: 'var(--green)' }}>{alive} alive</span>
        <span style={{ color: 'var(--fg-3)' }}>·</span>
        <span>{woven} woven</span>
      </div>

      <div className="tb-spacer" />

      <div className="tb-search">
        <Icon name="search" size={14} color="var(--fg-3)" />
        <span style={{ flex: 1 }}>Search the mesh</span>
        <span className="mono" style={{ fontSize: 11, color: 'var(--fg-3)' }}>⌘K</span>
      </div>

      <div className="tb-stat" title={version ? `v${version}` : undefined}>
        <span className={`dot ${c.cls}`} />
        <span style={{ fontSize: 11 }}>{c.text}</span>
      </div>

      <button type="button" className="iconbtn" title="settings">
        <Icon name="settings" size={17} />
      </button>
    </header>
  )
}

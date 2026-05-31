import { useCallback, useEffect, useMemo, useState } from 'react'
import { useFleet, type ConnState } from './useFleet'
import { FleetGrid } from './components/FleetGrid'
import { ConsolePanel } from './components/ConsolePanel'
import { AddDevice } from './components/AddDevice'
import { Workspace } from './components/workspace/Workspace'
import { wakeAgent } from './api'
import logoMark from './design/logo-mark.svg'

type View = 'workspace' | 'fleet'

export default function App() {
  const { agents, health, loading, error, conn, runs, registerRun } = useFleet()
  const [view, setView] = useState<View>('workspace')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)

  const onlineCount = agents.filter((a) => a.online).length

  // A sender for Wake-on-LAN: any online agent on the target's LAN broadcasts.
  const firstOnlineId = useMemo(() => agents.find((a) => a.online)?.id ?? null, [agents])

  const onWake = useCallback(
    async (mac: string) => {
      if (!firstOnlineId) return { ok: false, error: 'no online agent to broadcast from' }
      try {
        return await wakeAgent(firstOnlineId, mac)
      } catch (e) {
        return { ok: false, error: e instanceof Error ? e.message : 'wake failed' }
      }
    },
    [firstOnlineId],
  )

  // Auto-select the first online agent once the fleet lands.
  useEffect(() => {
    if (selectedId && agents.some((a) => a.id === selectedId)) return
    const first = agents.find((a) => a.online) ?? agents[0]
    setSelectedId(first ? first.id : null)
  }, [agents, selectedId])

  return (
    <div className="flex h-screen min-h-0 flex-col" style={{ background: 'var(--base)' }}>
      <TopBar
        view={view}
        onView={setView}
        conn={conn}
        version={health?.version}
        online={onlineCount}
        total={agents.length}
        onAddDevice={() => setAdding(true)}
      />

      {view === 'workspace' ? (
        <main className="flex min-h-0 flex-1 flex-col">
          <Workspace agents={agents} />
        </main>
      ) : (
        <main className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[1fr_minmax(340px,400px)]">
          <div className="min-w-0">
            <FleetGrid
              agents={agents}
              loading={loading}
              error={error}
              selectedId={selectedId}
              onSelect={setSelectedId}
              canWake={!!firstOnlineId}
              onWake={onWake}
            />
          </div>
          <div className="min-h-0" style={{ borderLeft: '1px solid var(--border)' }}>
            <ConsolePanel
              agents={agents}
              selectedId={selectedId}
              onSelect={setSelectedId}
              runs={runs}
              registerRun={registerRun}
            />
          </div>
        </main>
      )}

      {adding && <AddDevice onClose={() => setAdding(false)} />}
    </div>
  )
}

// TopBar — the design-system top strip: logo + Fleet/Workspace segmented toggle,
// live mesh stats, a search affordance, hub status, and settings/add icons.
function TopBar({
  view,
  onView,
  conn,
  version,
  online,
  total,
  onAddDevice,
}: {
  view: View
  onView: (v: View) => void
  conn: ConnState
  version?: string
  online: number
  total: number
  onAddDevice: () => void
}) {
  const connInfo: Record<ConnState, { cls: string; text: string }> = {
    live: { cls: 'live', text: 'hub live' },
    connecting: { cls: 'starting', text: 'connecting' },
    down: { cls: 'danger', text: 'hub down' },
  }
  const c = connInfo[conn]
  return (
    <div className="topbar">
      <img src={logoMark} alt="" style={{ width: 24, height: 24 }} />
      <span style={{ fontSize: 17, fontWeight: 700, letterSpacing: '-.03em', color: 'var(--fg-1)' }}>lattice</span>

      <div className="seg" style={{ marginLeft: 6 }}>
        <button type="button" className={view === 'fleet' ? 'on' : ''} onClick={() => onView('fleet')}>
          <LayersIcon /> Fleet
        </button>
        <button type="button" className={view === 'workspace' ? 'on' : ''} onClick={() => onView('workspace')}>
          <TermIcon /> Workspace
        </button>
      </div>

      <div className="tb-stat" style={{ marginLeft: 4 }}>
        <span className="dot live" />
        <span><b style={{ color: 'var(--fg-1)' }}>{online}</b> alive</span>
        <span style={{ opacity: 0.5 }}>·</span>
        <span><b style={{ color: 'var(--fg-1)' }}>{total}</b> woven</span>
      </div>

      <div className="tb-spacer" />

      <div className="tb-search">
        <SearchIcon />
        <span style={{ flex: 1 }}>Search the mesh</span>
        <span className="mono" style={{ fontSize: 11, opacity: 0.7 }}>⌘K</span>
      </div>

      <div className="tb-stat" title={version ? `v${version}` : undefined}>
        <span className={`dot ${c.cls}`} />
        <span style={{ fontSize: 11 }}>{c.text}</span>
      </div>

      <button type="button" className="iconbtn" title="add device" onClick={onAddDevice}>
        <PlusIcon />
      </button>
      <button type="button" className="iconbtn" title="settings">
        <GearIcon />
      </button>
    </div>
  )
}

function PlusIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M12 5v14m-7-7h14" strokeLinecap="round" />
    </svg>
  )
}
function LayersIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M12 3 3 8l9 5 9-5-9-5zM3 13l9 5 9-5M3 17l9 5 9-5" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}
function TermIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M5 7l4 4-4 4M12 16h7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
function SearchIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <circle cx="11" cy="11" r="7" /><path d="m20 20-3-3" strokeLinecap="round" />
    </svg>
  )
}
function GearIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

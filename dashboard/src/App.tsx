/* App root — view routing, fleet state, wake lifecycle, start-session, toast */
import { useRef, useState } from 'react'
import { Dot } from './lattice/primitives'
import { Rail } from './lattice/Rail'
import { TopBar } from './lattice/TopBar'
import { Fleet } from './lattice/Fleet'
import { Workspace } from './lattice/Workspace'
import { NewSession } from './lattice/NewSession'
import { FLEET } from './lattice/data'
import type { Machine } from './lattice/data'

function Toast({ text, kind }: { text: string; kind: string }) {
  return (
    <div className={`toast ${kind === 'wake' ? 'wake' : ''}`}>
      <Dot status={kind === 'wake' ? 'starting' : 'live'} />
      {text}
    </div>
  )
}

export default function App() {
  const [view, setView] = useState('fleet')
  const [selected, setSelected] = useState('garage-pc')
  const [activeFile, setActiveFile] = useState('mesh.ts')
  const [fleet, setFleet] = useState<Machine[]>(() => FLEET.map((m) => ({ ...m, sessions: [...m.sessions] })))
  const [modal, setModal] = useState(false)
  const [toast, setToast] = useState<{ text: string; kind: string } | null>(null)
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const flash = (text: string, kind: string) => {
    setToast({ text, kind })
    clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(null), 2800)
  }

  const wake = (id: string) => {
    const m = fleet.find((x) => x.id === id)
    if (!m) return
    setView('fleet')
    setSelected(id)
    setFleet((f) => f.map((x) => (x.id === id ? { ...x, offline: false, status: 'starting' } : x)))
    setToast({ text: `Waking ${m.label} → routing power through the mesh`, kind: 'wake' })
    clearTimeout(toastTimer.current)
    setTimeout(() => {
      setFleet((f) =>
        f.map((x) => (x.id === id ? { ...x, status: 'idle', cpu: 3, memUsed: 4, net: '0.3 GB/s', uptime: '0m' } : x)),
      )
      flash(`${m.label} is awake → woven into the mesh`, 'live')
    }, 2600)
  }

  const startSession = (machineId: string, name: string, _type: string) => {
    const m = fleet.find((x) => x.id === machineId)
    setFleet((f) =>
      f.map((x) =>
        x.id === machineId
          ? { ...x, status: 'live', cpu: Math.max(x.cpu, 24), sessions: [...x.sessions, { name, status: 'live', dur: '0m' }] }
          : x,
      ),
    )
    setModal(false)
    setSelected(machineId)
    setView('workspace')
    flash(`Started ${name} → ${m ? m.label : machineId}`, 'live')
  }

  return (
    <div className="app">
      <Rail
        view={view}
        fleet={fleet}
        selected={selected}
        activeFile={activeFile}
        onSelect={(id) => {
          setSelected(id)
          setView('fleet')
        }}
        onSelectFile={setActiveFile}
        onStart={() => setModal(true)}
        onWake={wake}
        onGoFleet={() => setView('fleet')}
      />
      <TopBar view={view} onView={setView} fleet={fleet} activeFile={activeFile} />
      <div className="stage">
        {view === 'fleet' ? (
          <Fleet
            fleet={fleet}
            selected={selected}
            onSelect={setSelected}
            onStart={() => setModal(true)}
            onWake={wake}
            onOpenWorkspace={() => setView('workspace')}
          />
        ) : (
          <Workspace activeFile={activeFile} onSelectFile={setActiveFile} onStart={() => setModal(true)} />
        )}
        {modal && <NewSession fleet={fleet} onClose={() => setModal(false)} onStart={startSession} />}
        {toast && <Toast text={toast.text} kind={toast.kind} />}
      </div>
    </div>
  )
}

import { useEffect, useState } from 'react'
import { useFleet, type ConnState } from './useFleet'
import { FleetGrid } from './components/FleetGrid'
import { CommandPanel } from './components/CommandPanel'

export default function App() {
  const { agents, health, loading, error, conn, runs, registerRun } = useFleet()
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const onlineCount = agents.filter((a) => a.online).length

  // Auto-select the first online agent once the fleet lands.
  useEffect(() => {
    if (selectedId && agents.some((a) => a.id === selectedId)) return
    const first = agents.find((a) => a.online) ?? agents[0]
    setSelectedId(first ? first.id : null)
  }, [agents, selectedId])

  return (
    <div className="lattice-bg min-h-full">
      <div className="mx-auto flex min-h-screen max-w-[1600px] flex-col px-4 py-5 sm:px-6 lg:px-8">
        <Header conn={conn} version={health?.version} online={onlineCount} total={agents.length} />

        <main className="mt-6 grid min-h-0 flex-1 grid-cols-1 gap-5 lg:grid-cols-[1fr_minmax(360px,420px)]">
          <div className="min-w-0">
            <SectionLabel>fleet</SectionLabel>
            <div className="mt-3">
              <FleetGrid
                agents={agents}
                loading={loading}
                error={error}
                selectedId={selectedId}
                onSelect={setSelectedId}
              />
            </div>
          </div>

          <div className="min-h-[420px] lg:min-h-0 lg:h-[calc(100vh-7rem)] lg:sticky lg:top-5">
            <CommandPanel
              agents={agents}
              selectedId={selectedId}
              onSelect={setSelectedId}
              runs={runs}
              registerRun={registerRun}
            />
          </div>
        </main>
      </div>
    </div>
  )
}

function Header({ conn, version, online, total }: { conn: ConnState; version?: string; online: number; total: number }) {
  return (
    <header className="flex flex-wrap items-center gap-x-6 gap-y-3 border-b border-zinc-800/80 pb-5">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 place-items-center rounded-lg border border-emerald-500/30 bg-emerald-500/10">
          <LatticeMark />
        </div>
        <div>
          <h1 className="font-display text-xl font-bold tracking-tight text-zinc-50">lattice</h1>
          <p className="font-mono text-[10px] uppercase tracking-[0.22em] text-zinc-500">fleet console</p>
        </div>
      </div>

      <div className="ml-auto flex items-center gap-5">
        <Stat value={online} label="online" tone="emerald" />
        <Stat value={total} label="agents" tone="zinc" />
        <HubStatus conn={conn} version={version} />
      </div>
    </header>
  )
}

function Stat({ value, label, tone }: { value: number; label: string; tone: 'emerald' | 'zinc' }) {
  return (
    <div className="text-right">
      <div className={`font-mono text-lg font-semibold tabular-nums ${tone === 'emerald' ? 'text-emerald-400' : 'text-zinc-200'}`}>
        {value}
      </div>
      <div className="font-mono text-[10px] uppercase tracking-wider text-zinc-500">{label}</div>
    </div>
  )
}

function HubStatus({ conn, version }: { conn: ConnState; version?: string }) {
  const map: Record<ConnState, { dot: string; pulse: boolean; text: string }> = {
    live: { dot: 'bg-emerald-400', pulse: true, text: 'hub live' },
    connecting: { dot: 'bg-amber-400', pulse: true, text: 'connecting' },
    down: { dot: 'bg-red-500', pulse: false, text: 'hub down' },
  }
  const s = map[conn]
  return (
    <div className="flex items-center gap-2 rounded-full border border-zinc-800 bg-zinc-900/70 px-3 py-1.5">
      <span className="relative flex h-2 w-2">
        {s.pulse && <span className={`absolute inline-flex h-full w-full rounded-full ${s.dot} opacity-70 animate-breathe`} />}
        <span className={`relative inline-flex h-2 w-2 rounded-full ${s.dot}`} />
      </span>
      <span className="font-mono text-[11px] text-zinc-300">{s.text}</span>
      {version && <span className="font-mono text-[10px] text-zinc-600">v{version}</span>}
    </div>
  )
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <h2 className="font-display text-xs font-semibold uppercase tracking-[0.18em] text-zinc-400">{children}</h2>
}

function LatticeMark() {
  return (
    <svg viewBox="0 0 24 24" className="h-5 w-5 text-emerald-400" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
      <path d="M12 2 22 7v10L12 22 2 17V7z" strokeLinejoin="round" />
      <path d="M2 7l10 5 10-5M12 12v10" strokeLinejoin="round" />
    </svg>
  )
}

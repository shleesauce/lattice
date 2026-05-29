import type { Agent, WakeResult } from '../types'
import { AgentCard } from './AgentCard'

interface Props {
  agents: Agent[]
  loading: boolean
  error: string | null
  selectedId: string | null
  onSelect: (id: string) => void
  canWake: boolean
  onWake: (mac: string) => Promise<WakeResult>
}

export function FleetGrid({ agents, loading, error, selectedId, onSelect, canWake, onWake }: Props) {
  if (loading) return <SkeletonGrid />
  if (error) return <ErrorState message={error} />
  if (agents.length === 0) return <EmptyState />

  const sorted = [...agents].sort((a, b) => {
    if (a.online !== b.online) return a.online ? -1 : 1
    return (a.name || a.hostname).localeCompare(b.name || b.hostname)
  })

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
      {sorted.map((a) => (
        <AgentCard
          key={a.id}
          agent={a}
          selected={a.id === selectedId}
          onSelect={onSelect}
          canWake={canWake}
          onWake={onWake}
        />
      ))}
    </div>
  )
}

function SkeletonGrid() {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="h-44 animate-pulse rounded-xl border border-zinc-800 bg-zinc-900/40" />
      ))}
    </div>
  )
}

function ErrorState({ message }: { message: string }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-xl border border-red-900/50 bg-red-950/20 px-6 py-16 text-center">
      <div className="font-display text-sm font-semibold text-red-300">fleet unreachable</div>
      <p className="mt-1 max-w-sm font-mono text-xs text-red-400/70">{message}</p>
      <p className="mt-3 font-mono text-[11px] text-zinc-600">retrying live connection…</p>
    </div>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-zinc-800 bg-zinc-900/30 px-6 py-20 text-center">
      <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-lg border border-zinc-800 bg-zinc-900">
        <span className="h-2 w-2 animate-breathe rounded-full bg-emerald-400/60" />
      </div>
      <div className="font-display text-base font-semibold text-zinc-200">No agents enrolled</div>
      <p className="mt-1 max-w-sm font-mono text-xs text-zinc-500">
        Install the lattice agent on a host and point it at this hub. It will appear here the moment it registers.
      </p>
    </div>
  )
}

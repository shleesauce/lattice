import type { Agent } from '../types'
import { humanBytes, humanUptime, pct, shortTime } from '../format'
import { Meter, OSGlyph } from './Glyphs'

interface Props {
  agent: Agent
  selected: boolean
  onSelect: (id: string) => void
}

export function AgentCard({ agent, selected, onSelect }: Props) {
  const online = agent.online
  return (
    <button
      type="button"
      onClick={() => onSelect(agent.id)}
      aria-pressed={selected}
      className={[
        'group relative flex flex-col gap-4 rounded-xl border p-4 text-left transition-all duration-200 animate-risein',
        'focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-400/60',
        online ? 'border-zinc-800 bg-zinc-900/60' : 'border-zinc-900 bg-zinc-950/60 opacity-60',
        selected
          ? 'border-emerald-500/70 bg-zinc-900 shadow-[0_0_0_1px_rgba(16,185,129,0.4),0_8px_30px_-12px_rgba(16,185,129,0.5)]'
          : 'hover:border-zinc-700 hover:bg-zinc-900',
      ].join(' ')}
    >
      {/* header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="text-zinc-400 group-hover:text-zinc-200">
            <OSGlyph os={agent.os} className="h-5 w-5" />
          </span>
          <div className="min-w-0">
            <div className="truncate font-display text-sm font-semibold text-zinc-100">
              {agent.name || agent.hostname}
            </div>
            <div className="truncate font-mono text-[11px] text-zinc-500">{agent.hostname}</div>
          </div>
        </div>
        <StatusDot online={online} />
      </div>

      {/* meta row */}
      <div className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-wider text-zinc-500">
        <span className="rounded bg-zinc-800/80 px-1.5 py-0.5 text-zinc-400">{agent.arch || '—'}</span>
        <span className="rounded bg-zinc-800/80 px-1.5 py-0.5 text-zinc-400">{agent.cpuCount || 0} cpu</span>
        <span className="ml-auto text-zinc-600">{online ? humanUptime(agent.uptimeSec) : `seen ${shortTime(agent.lastSeen)}`}</span>
      </div>

      {/* meters */}
      <div className="space-y-2.5">
        <MetricRow label="disk" value={pct(agent.diskUsedPct)} sub={`${humanBytes(agent.diskFree)} free`} meter={agent.diskUsedPct} />
        <MetricRow label="mem" value={pct(agent.memUsedPct)} sub={humanBytes(agent.memTotal)} meter={agent.memUsedPct} />
      </div>

      {/* footer */}
      <div className="flex items-center justify-between border-t border-zinc-800/70 pt-3 font-mono text-[10px] text-zinc-500">
        <span>
          load <span className="text-zinc-300">{agent.loadAvg1.toFixed(2)}</span>
        </span>
        <span className="text-zinc-600">v{agent.agentVersion || '0'}</span>
      </div>
    </button>
  )
}

function MetricRow({ label, value, sub, meter }: { label: string; value: string; sub: string; meter: number }) {
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between font-mono text-[10px]">
        <span className="uppercase tracking-wider text-zinc-500">{label}</span>
        <span className="text-zinc-300">
          {value} <span className="text-zinc-600">· {sub}</span>
        </span>
      </div>
      <Meter value={meter} />
    </div>
  )
}

function StatusDot({ online }: { online: boolean }) {
  return (
    <span className="relative flex h-2.5 w-2.5 shrink-0 items-center justify-center" title={online ? 'online' : 'offline'}>
      {online && <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-400/70 animate-breathe" />}
      <span className={`relative inline-flex h-2 w-2 rounded-full ${online ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
    </span>
  )
}

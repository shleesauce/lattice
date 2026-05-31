import { useEffect, useRef, useState } from 'react'
import { previewPlacement } from '../../api'
import type { Agent, PlacementCandidate, PlacementResult, Session } from '../../types'

interface Props {
  session: Session
  agents: Agent[]
  onPin: (agentId: string) => void
}

function agentLabel(agents: Agent[], id: string): string {
  const a = agents.find((x) => x.id === id)
  return a?.name || a?.hostname || id.slice(0, 8)
}

// The machine chip: shows where a session is placed + a tiny score, and opens a
// dropdown that previews the ranked candidates (POST /api/placement) so the user
// can pin/override (D19). Orphaned sessions surface a "Resume here" action.
export function MachineChip({ session, agents, onPin }: Props) {
  // Device sessions are pinned to one machine by definition — pinning elsewhere
  // is meaningless, so render a static chip with no placement dropdown.
  if (session.scope === 'device') {
    return (
      <span
        className="flex items-center gap-1.5 rounded-md border border-zinc-700 bg-zinc-950 px-2 py-1 font-mono text-[11px] text-zinc-300"
        title="device-local session"
      >
        <PinIcon />
        <ChipIcon />
        <span className="max-w-[10rem] truncate">{agentLabel(agents, session.agentId)}</span>
      </span>
    )
  }

  return <ProjectMachineChip session={session} agents={agents} onPin={onPin} />
}

function ProjectMachineChip({ session, agents, onPin }: Props) {
  const [open, setOpen] = useState(false)
  const [result, setResult] = useState<PlacementResult | null>(null)
  const [state, setState] = useState<'idle' | 'loading' | 'error'>('idle')
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const load = async () => {
    setState('loading')
    try {
      const r = await previewPlacement({ kind: session.kind, projectPath: session.projectPath })
      setResult(r)
      setState('idle')
    } catch {
      setState('error')
    }
  }

  const toggle = () => {
    const next = !open
    setOpen(next)
    if (next && !result) void load()
  }

  const chosenScore = result?.candidates.find((c) => c.agentId === session.agentId)?.score

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={toggle}
        className="flex items-center gap-1.5 rounded-md border border-zinc-700 bg-zinc-950 px-2 py-1 font-mono text-[11px] text-zinc-300 transition-colors hover:border-emerald-500/50 hover:text-emerald-300"
        title="placement"
      >
        {session.pinned && <PinIcon />}
        <ChipIcon />
        <span className="max-w-[10rem] truncate">{agentLabel(agents, session.agentId)}</span>
        {typeof chosenScore === 'number' && (
          <span className="text-emerald-400/80">{chosenScore.toFixed(0)}</span>
        )}
        <Chevron open={open} />
      </button>

      {open && (
        <div className="absolute right-0 z-30 mt-2 w-80 overflow-hidden rounded-lg border border-zinc-700 bg-zinc-900 shadow-[0_12px_40px_-12px_rgba(0,0,0,0.7)]">
          <div className="border-b border-zinc-800 px-3 py-2 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">
            placement candidates
          </div>
          {session.status === 'orphaned' && (
            <button
              type="button"
              onClick={() => {
                onPin(session.agentId)
                setOpen(false)
              }}
              className="flex w-full items-center gap-2 border-b border-zinc-800 bg-orange-500/[0.06] px-3 py-2 text-left font-display text-[12px] font-semibold text-orange-300 hover:bg-orange-500/10"
            >
              <ResumeIcon /> Resume on best machine
            </button>
          )}
          <div className="term-scroll max-h-72 overflow-y-auto">
            {state === 'loading' && <p className="px-3 py-6 text-center font-mono text-[11px] text-zinc-600">scoring…</p>}
            {state === 'error' && (
              <div className="px-3 py-5 text-center">
                <p className="font-mono text-[11px] text-red-400">placement preview unavailable</p>
                <button type="button" onClick={load} className="mt-2 font-mono text-[11px] text-zinc-400 underline">
                  retry
                </button>
              </div>
            )}
            {state === 'idle' && result && result.candidates.length === 0 && (
              <p className="px-3 py-6 text-center font-mono text-[11px] text-zinc-600">no candidates</p>
            )}
            {state === 'idle' &&
              result?.candidates.map((c) => (
                <CandidateRow
                  key={c.agentId}
                  candidate={c}
                  label={agentLabel(agents, c.agentId)}
                  current={c.agentId === session.agentId}
                  onPin={() => {
                    if (c.eligible) {
                      onPin(c.agentId)
                      setOpen(false)
                    }
                  }}
                />
              ))}
          </div>
        </div>
      )}
    </div>
  )
}

function CandidateRow({
  candidate,
  label,
  current,
  onPin,
}: {
  candidate: PlacementCandidate
  label: string
  current: boolean
  onPin: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const reasons = Object.entries(candidate.reasons ?? {})
  return (
    <div className={`border-b border-zinc-800/70 ${current ? 'bg-emerald-500/[0.05]' : ''}`}>
      <div className="flex items-center gap-2 px-3 py-2">
        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${candidate.eligible ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-zinc-200">{label}</span>
        {candidate.eligible ? (
          <span className="font-mono text-[12px] tabular-nums text-emerald-400">{candidate.score.toFixed(1)}</span>
        ) : (
          <span className="font-mono text-[10px] uppercase tracking-wide text-orange-400/80">excluded</span>
        )}
        {reasons.length > 0 && (
          <button type="button" onClick={() => setExpanded((e) => !e)} className="text-zinc-600 hover:text-zinc-400">
            <Chevron open={expanded} />
          </button>
        )}
        {!current && candidate.eligible && (
          <button
            type="button"
            onClick={onPin}
            className="rounded border border-zinc-700 px-1.5 py-0.5 font-mono text-[10px] text-zinc-300 hover:border-emerald-500/50 hover:text-emerald-300"
          >
            pin
          </button>
        )}
        {current && <span className="font-mono text-[10px] text-emerald-400/70">current</span>}
      </div>
      {expanded && (
        <div className="space-y-0.5 px-3 pb-2 pl-7">
          {candidate.excluded && (
            <p className="font-mono text-[10px] text-orange-400/90">{candidate.excluded}</p>
          )}
          {reasons.map(([k, v]) => (
            <div key={k} className="flex justify-between font-mono text-[10px]">
              <span className="text-zinc-600">{k}</span>
              <span className="text-zinc-400">{String(v)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function ChipIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3 w-3 text-zinc-500" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <rect x="6" y="6" width="12" height="12" rx="1.5" />
      <path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3" strokeLinecap="round" />
    </svg>
  )
}

function PinIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3 w-3 text-emerald-400" fill="currentColor" aria-hidden>
      <path d="M9 2h6l-1 6 3 3v2h-5v7l-1 2-1-2v-7H4v-2l3-3z" />
    </svg>
  )
}

function ResumeIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M4 4v6h6M20 20v-6h-6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M4 10a8 8 0 0 1 14-3M20 14a8 8 0 0 1-14 3" strokeLinecap="round" />
    </svg>
  )
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={`h-3 w-3 shrink-0 text-zinc-500 transition-transform ${open ? 'rotate-180' : ''}`}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden
    >
      <path d="M6 9l6 6 6-6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

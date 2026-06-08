import { useEffect, useRef, useState } from 'react'
import { canHostClaude, type Agent, type Session } from '../../types'

interface Props {
  session: Session
  agents: Agent[]
  // reconnect an orphaned session on its OWN machine (resume, keeps context)
  onReconnect: (agentId: string) => void
  // switch a fresh session to a different machine (restarts it there)
  onPickMachine: (agentId: string) => void
}

function agentLabel(agents: Agent[], id: string): string {
  const a = agents.find((x) => x.id === id)
  return a?.name || a?.hostname || id.slice(0, 8)
}

// The machine chip. A session never moves on its own (D32) — but you can pick
// which machine it runs on BEFORE you start a conversation. For a claude session
// the list is the machines that actually have claude installed and are online.
export function MachineChip({ session, agents, onReconnect, onPickMachine }: Props) {
  // Device sessions are bound to one machine by definition — static, no picker.
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

  return <ProjectMachineChip session={session} agents={agents} onReconnect={onReconnect} onPickMachine={onPickMachine} />
}

function ProjectMachineChip({ session, agents, onReconnect, onPickMachine }: Props) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const orphaned = session.status === 'orphaned'

  // Eligible targets: online + (for claude) can actually host claude — installed
  // AND authable, so the switcher never offers a box that yields a blank tab (F14).
  const eligible = agents.filter((a) => {
    if (!a.online) return false
    if (session.kind === 'claude') return canHostClaude(a)
    return true
  })

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 rounded-md border border-zinc-700 bg-zinc-950 px-2 py-1 font-mono text-[11px] text-zinc-300 transition-colors hover:border-emerald-500/50 hover:text-emerald-300"
        title="which machine this session runs on"
      >
        <PinIcon />
        <ChipIcon />
        <span className="max-w-[10rem] truncate">{agentLabel(agents, session.agentId)}</span>
        <Chevron open={open} />
      </button>

      {open && (
        <div className="absolute right-0 z-30 mt-2 w-64 overflow-hidden rounded-lg border border-zinc-700 bg-zinc-900 shadow-[0_12px_40px_-12px_rgba(0,0,0,0.7)]">
          <div className="border-b border-zinc-800 px-3 py-2 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">
            run on
          </div>
          {orphaned && (
            <button
              type="button"
              onClick={() => {
                onReconnect(session.agentId)
                setOpen(false)
              }}
              className="flex w-full items-center gap-2 border-b border-zinc-800 bg-orange-500/[0.06] px-3 py-2 text-left font-display text-[12px] font-semibold text-orange-300 hover:bg-orange-500/10"
            >
              <ResumeIcon /> reconnect on {agentLabel(agents, session.agentId)}
            </button>
          )}
          <div className="max-h-72 overflow-y-auto py-1">
            {eligible.length === 0 && (
              <p className="px-3 py-4 text-center font-mono text-[11px] text-zinc-600">no machines available</p>
            )}
            {eligible.map((a) => {
              const current = a.id === session.agentId
              return (
                <button
                  key={a.id}
                  type="button"
                  disabled={current}
                  onClick={() => {
                    if (!current) {
                      onPickMachine(a.id)
                      setOpen(false)
                    }
                  }}
                  className={`flex w-full items-center gap-2 px-3 py-2 text-left font-mono text-[12px] ${
                    current ? 'cursor-default text-emerald-300' : 'text-zinc-200 hover:bg-zinc-800'
                  }`}
                >
                  <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${a.online ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
                  <span className="min-w-0 flex-1 truncate">{a.name || a.hostname || a.id}</span>
                  {current ? (
                    <span className="font-mono text-[10px] text-emerald-400/70">current</span>
                  ) : (
                    <span className="font-mono text-[10px] text-zinc-500">switch →</span>
                  )}
                </button>
              )
            })}
          </div>
          <div className="border-t border-zinc-800 px-3 py-1.5 font-mono text-[10px] text-zinc-600">
            // switching restarts the session on that machine
          </div>
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

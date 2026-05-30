import { useEffect, useState } from 'react'
import type { Agent, Session } from '../../types'
import { SessionTerminal } from './SessionTerminal'
import { SessionClaude } from './SessionClaude'
import { MonacoPanel } from './MonacoPanel'
import { MachineChip } from './MachineChip'

type Tab = 'terminal' | 'claude'

interface Props {
  session: Session
  agents: Agent[]
  onClose: () => void
  onPin: (agentId: string) => void
}

// One open session: a tab bar (Terminal / Claude) + machine chip, with a
// collapsible Monaco file panel docked at the bottom. Both tabs stay mounted
// once opened so switching doesn't drop the live socket/scrollback.
export function SessionPane({ session, agents, onClose, onPin }: Props) {
  const [tab, setTab] = useState<Tab>(session.kind)
  const [mounted, setMounted] = useState<Record<Tab, boolean>>({
    terminal: session.kind === 'terminal',
    claude: session.kind === 'claude',
  })
  const [filesOpen, setFilesOpen] = useState(false)

  useEffect(() => {
    setMounted((m) => (m[tab] ? m : { ...m, [tab]: true }))
  }, [tab])

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* tab bar */}
      <div className="flex items-center gap-2 border-b border-zinc-800 bg-zinc-900/50 px-3 py-2">
        <div className="inline-flex rounded-md border border-zinc-800 bg-zinc-950 p-0.5">
          {(['claude', 'terminal'] as Tab[]).map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setTab(t)}
              className={`rounded px-3 py-1 font-display text-xs font-semibold uppercase tracking-wider transition-colors ${
                tab === t ? 'bg-emerald-500/15 text-emerald-300' : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {t}
            </button>
          ))}
        </div>

        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            onClick={() => setFilesOpen((f) => !f)}
            title="toggle files"
            className={`grid h-7 w-7 place-items-center rounded-md border transition-colors ${
              filesOpen
                ? 'border-emerald-500/50 bg-emerald-500/10 text-emerald-300'
                : 'border-zinc-700 text-zinc-400 hover:border-zinc-600 hover:text-zinc-200'
            }`}
          >
            <FilesIcon />
          </button>
          <MachineChip session={session} agents={agents} onPin={onPin} />
          <button
            type="button"
            onClick={onClose}
            title="close session tab"
            className="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-red-300"
          >
            <CloseIcon />
          </button>
        </div>
      </div>

      {/* body */}
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="relative min-h-0 flex-1">
          {mounted.terminal && (
            <div className={`absolute inset-0 ${tab === 'terminal' ? '' : 'hidden'}`}>
              <SessionTerminal sessionId={session.id} />
            </div>
          )}
          {mounted.claude && (
            <div className={`absolute inset-0 ${tab === 'claude' ? '' : 'hidden'}`}>
              <SessionClaude sessionId={session.id} />
            </div>
          )}
        </div>

        {filesOpen && (
          <div className="h-64 shrink-0 border-t border-zinc-800">
            <MonacoPanel agentId={session.agentId} rootPath={session.projectPath} />
          </div>
        )}
      </div>
    </div>
  )
}

function FilesIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
      <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" strokeLinejoin="round" />
    </svg>
  )
}

function CloseIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M6 6l12 12M18 6 6 18" strokeLinecap="round" />
    </svg>
  )
}

import { useEffect, useState } from 'react'
import type { Agent, Session, SessionKind } from '../../types'
import { SessionTerminal } from './SessionTerminal'
import { SessionClaude } from './SessionClaude'
import { SessionEditor } from './SessionEditor'
import { MachineChip } from './MachineChip'

type Tab = SessionKind

interface Props {
  session: Session
  agents: Agent[]
  onClose: () => void
  onPin: (agentId: string) => void
}

// Tabs shown per session kind. Editor sessions lead with the editor tab;
// claude/terminal sessions lead with their native tab. All tabs stay mounted
// once opened so switching doesn't drop live sockets or scrollback.
const TAB_ORDER: Record<SessionKind, Tab[]> = {
  claude: ['claude', 'terminal'],
  terminal: ['terminal', 'claude'],
  editor: ['editor', 'terminal', 'claude'],
}

// One open session: a tab bar + machine chip. All body panels stay mounted.
// File browsing lives in the persistent right-rail explorer.
export function SessionPane({ session, agents, onClose, onPin }: Props) {
  const [tab, setTab] = useState<Tab>(session.kind)
  const [mounted, setMounted] = useState<Record<Tab, boolean>>({
    terminal: session.kind === 'terminal',
    claude: session.kind === 'claude',
    editor: session.kind === 'editor',
  })

  useEffect(() => {
    setMounted((m) => (m[tab] ? m : { ...m, [tab]: true }))
  }, [tab])

  const tabs = TAB_ORDER[session.kind]

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* tab bar */}
      <div className="flex items-center gap-2 border-b border-zinc-800 bg-zinc-900/50 px-3 py-2">
        <div className="inline-flex rounded-md border border-zinc-800 bg-zinc-950 p-0.5">
          {tabs.map((t) => (
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
        {mounted.editor && (
          <div className={`absolute inset-0 ${tab === 'editor' ? '' : 'hidden'}`}>
            <SessionEditor sessionId={session.id} />
          </div>
        )}
      </div>
    </div>
  )
}

function CloseIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M6 6l12 12M18 6 6 18" strokeLinecap="round" />
    </svg>
  )
}

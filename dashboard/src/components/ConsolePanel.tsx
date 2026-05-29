import { useEffect, useState } from 'react'
import type { Agent, CommandRun } from '../types'
import { CommandPanel } from './CommandPanel'
import { TerminalView } from './TerminalView'
import { FileBrowser } from './FileBrowser'
import { OSGlyph } from './Glyphs'

type Tab = 'command' | 'terminal' | 'files'

interface Props {
  agents: Agent[]
  selectedId: string | null
  onSelect: (id: string) => void
  runs: Record<string, CommandRun>
  registerRun: (cmdId: string, agentId: string, command: string) => void
}

export function ConsolePanel({ agents, selectedId, onSelect, runs, registerRun }: Props) {
  const [tab, setTab] = useState<Tab>('command')
  const selected = agents.find((a) => a.id === selectedId) ?? null
  const online = !!selected?.online

  // Terminal needs a live agent; drop back to command if we lose one mid-session.
  useEffect(() => {
    if (tab === 'terminal' && !online) setTab('command')
  }, [tab, online])

  return (
    <section className="flex h-full min-h-0 flex-col rounded-xl border border-zinc-800 bg-zinc-900/60">
      <header className="flex flex-wrap items-center gap-2 border-b border-zinc-800 px-4 py-3">
        <Segmented tab={tab} onChange={setTab} terminalDisabled={!online} />
        <div className="ml-auto flex items-center gap-2">
          {selected && <OSGlyph os={selected.os} className="h-4 w-4 text-zinc-500" />}
          <select
            value={selectedId ?? ''}
            onChange={(e) => onSelect(e.target.value)}
            className="max-w-[14rem] rounded-md border border-zinc-700 bg-zinc-950 px-2.5 py-1.5 font-mono text-xs text-zinc-200 focus:border-emerald-500/60 focus:outline-none"
          >
            <option value="" disabled>
              select target…
            </option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {(a.name || a.hostname) + (a.online ? '' : '  (offline)')}
              </option>
            ))}
          </select>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 flex-col">
        {tab === 'command' && (
          <CommandPanel
            agents={agents}
            selectedId={selectedId}
            onSelect={onSelect}
            runs={runs}
            registerRun={registerRun}
            embedded
          />
        )}
        {tab === 'terminal' &&
          (selected ? (
            <TerminalView key={selected.id} agentId={selected.id} online={online} />
          ) : (
            <Placeholder text="select an agent to open a terminal" />
          ))}
        {tab === 'files' &&
          (selected ? (
            <FileBrowser key={selected.id} agentId={selected.id} online={online} />
          ) : (
            <Placeholder text="select an agent to browse files" />
          ))}
      </div>
    </section>
  )
}

function Segmented({ tab, onChange, terminalDisabled }: { tab: Tab; onChange: (t: Tab) => void; terminalDisabled: boolean }) {
  const items: { id: Tab; label: string; disabled?: boolean; title?: string }[] = [
    { id: 'command', label: 'command' },
    { id: 'terminal', label: 'terminal', disabled: terminalDisabled, title: terminalDisabled ? 'agent must be online' : undefined },
    { id: 'files', label: 'files' },
  ]
  return (
    <div className="inline-flex rounded-md border border-zinc-800 bg-zinc-950 p-0.5">
      {items.map((it) => {
        const active = tab === it.id
        return (
          <button
            key={it.id}
            type="button"
            disabled={it.disabled}
            title={it.title}
            onClick={() => onChange(it.id)}
            className={[
              'rounded px-3 py-1 font-display text-xs font-semibold uppercase tracking-wider transition-colors',
              active ? 'bg-emerald-500/15 text-emerald-300' : 'text-zinc-500 hover:text-zinc-300',
              it.disabled ? 'cursor-not-allowed opacity-40 hover:text-zinc-500' : '',
            ].join(' ')}
          >
            {it.label}
          </button>
        )
      })}
    </div>
  )
}

function Placeholder({ text }: { text: string }) {
  return (
    <div className="grid flex-1 place-items-center px-6 text-center">
      <p className="font-mono text-xs text-zinc-600">// {text}</p>
    </div>
  )
}

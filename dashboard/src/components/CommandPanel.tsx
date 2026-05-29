import { useEffect, useMemo, useRef, useState } from 'react'
import type { Agent, CommandRun } from '../types'
import { execCommand } from '../api'
import { OSGlyph } from './Glyphs'

interface Props {
  agents: Agent[]
  selectedId: string | null
  onSelect: (id: string) => void
  runs: Record<string, CommandRun>
  registerRun: (cmdId: string, agentId: string, command: string) => void
  // embedded: rendered inside ConsolePanel, which already owns the section
  // chrome (header + target selector). Skip our own to avoid double framing.
  embedded?: boolean
}

export function CommandPanel({ agents, selectedId, onSelect, runs, registerRun, embedded }: Props) {
  const [command, setCommand] = useState('')
  const [sending, setSending] = useState(false)
  const [sendError, setSendError] = useState<string | null>(null)

  const selected = agents.find((a) => a.id === selectedId) ?? null
  const disabled = !selected || !selected.online || sending || command.trim() === ''

  // Runs for the selected agent, newest first.
  const agentRuns = useMemo(
    () =>
      Object.values(runs)
        .filter((r) => r.agentId === selectedId)
        .sort((a, b) => b.startedAt - a.startedAt),
    [runs, selectedId],
  )

  const run = async () => {
    if (disabled || !selected) return
    setSending(true)
    setSendError(null)
    try {
      const cmdId = await execCommand(selected.id, command.trim())
      registerRun(cmdId, selected.id, command.trim())
      setCommand('')
    } catch (e) {
      setSendError(e instanceof Error ? e.message : 'dispatch failed')
    } finally {
      setSending(false)
    }
  }

  const body = (
    <>
      {/* input */}
      <div className="border-b border-zinc-800 px-4 py-3">
        <div className="flex items-stretch gap-2">
          <div className="flex flex-1 items-center gap-2 rounded-md border border-zinc-700 bg-zinc-950 px-2.5 focus-within:border-emerald-500/60">
            {selected && <OSGlyph os={selected.os} className="h-4 w-4 shrink-0 text-zinc-500" />}
            <span className="select-none font-mono text-sm text-emerald-500">$</span>
            <input
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && run()}
              placeholder={selected ? (selected.online ? 'run a command…' : 'agent offline') : 'select an agent first'}
              disabled={!selected || !selected.online}
              spellCheck={false}
              className="w-full bg-transparent py-2 font-mono text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none disabled:cursor-not-allowed"
            />
          </div>
          <button
            type="button"
            onClick={run}
            disabled={disabled}
            className="rounded-md bg-emerald-500 px-4 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:bg-zinc-800 disabled:text-zinc-600"
          >
            {sending ? '…' : 'Run'}
          </button>
        </div>
        {sendError && <p className="mt-2 font-mono text-[11px] text-red-400">{sendError}</p>}
      </div>

      {/* terminal */}
      <Terminal runs={agentRuns} hasSelection={!!selected} />
    </>
  )

  if (embedded) {
    return <div className="flex min-h-0 flex-1 flex-col">{body}</div>
  }

  return (
    <section className="flex h-full min-h-0 flex-col rounded-xl border border-zinc-800 bg-zinc-900/60">
      <header className="flex flex-wrap items-center gap-2 border-b border-zinc-800 px-4 py-3">
        <h2 className="font-display text-xs font-semibold uppercase tracking-[0.18em] text-zinc-400">command</h2>
        <div className="ml-auto">
          <select
            value={selectedId ?? ''}
            onChange={(e) => onSelect(e.target.value)}
            className="max-w-[16rem] rounded-md border border-zinc-700 bg-zinc-950 px-2.5 py-1.5 font-mono text-xs text-zinc-200 focus:border-emerald-500/60 focus:outline-none"
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
      {body}
    </section>
  )
}

function Terminal({ runs, hasSelection }: { runs: CommandRun[]; hasSelection: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const totalLines = runs.reduce((n, r) => n + r.lines.length, 0)

  useEffect(() => {
    const el = ref.current
    if (el) el.scrollTop = el.scrollHeight
  }, [totalLines, runs.length])

  return (
    <div ref={ref} className="term-scroll min-h-0 flex-1 overflow-y-auto bg-zinc-950 px-4 py-3 font-mono text-[12.5px] leading-relaxed">
      {!hasSelection ? (
        <p className="text-zinc-600">// select an agent to open a session</p>
      ) : runs.length === 0 ? (
        <p className="text-zinc-600">// no commands run yet</p>
      ) : (
        runs.map((r) => (
          <div key={r.cmdId} className="mb-4">
            <div className="flex items-center gap-2 text-zinc-500">
              <span className="text-emerald-500">$</span>
              <span className="text-zinc-300">{r.command}</span>
            </div>
            {r.lines.map((l, i) => (
              <pre key={i} className={`whitespace-pre-wrap break-words ${l.stream === 'stderr' ? 'text-red-400' : 'text-zinc-300'}`}>
                {l.data}
              </pre>
            ))}
            {r.finished ? (
              <div className={`mt-1 text-[11px] ${r.exitCode === 0 ? 'text-emerald-500/80' : 'text-red-400/90'}`}>
                {r.error ? `error: ${r.error}` : `exit ${r.exitCode}`}
              </div>
            ) : (
              <div className="mt-1 text-[11px] text-zinc-600">
                running<span className="animate-breathe">…</span>
              </div>
            )}
          </div>
        ))
      )}
    </div>
  )
}

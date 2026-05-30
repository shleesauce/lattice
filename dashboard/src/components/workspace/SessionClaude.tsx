import { useCallback, useEffect, useReducer, useRef, useState } from 'react'
import { useSessionSocket } from '../../useSessionSocket'
import {
  appendUserTurn,
  applyClaudeEvent,
  applyClaudeEvents,
  emptyClaudeState,
  resolvePermission,
  type ClaudeState,
} from '../../lib/claudeModel'
import type { ClaudeRaw } from '../../types'
import { ChatItemView } from './ChatItems'

interface Props {
  sessionId: string
}

type Action =
  | { t: 'replay'; events: ClaudeRaw[] }
  | { t: 'event'; raw: ClaudeRaw }
  | { t: 'send'; text: string }
  | { t: 'permission'; id: string; allowed: boolean }
  | { t: 'exit' }

function reducer(state: ClaudeState, a: Action): ClaudeState {
  switch (a.t) {
    case 'replay':
      return applyClaudeEvents(emptyClaudeState, a.events)
    case 'event':
      return applyClaudeEvent(state, a.raw)
    case 'send':
      return appendUserTurn(state, a.text)
    case 'permission':
      return resolvePermission(state, a.id, a.allowed)
    case 'exit':
      return { ...state, busy: false }
  }
}

// The Claude tab — a native chat over the stream-json event feed. Renders
// assistant markdown, tool-call/result cards, a live token-usage HUD, and a
// multiline composer. Feels like the Claude Code desktop app.
export function SessionClaude({ sessionId }: Props) {
  const [state, dispatch] = useReducer(reducer, emptyClaudeState)
  const scrollRef = useRef<HTMLDivElement>(null)
  const atBottomRef = useRef(true)

  const { phase, sendClaudeInput, sendPermission } = useSessionSocket(sessionId, 'claude', {
    claude: {
      onReplay: (events) => dispatch({ t: 'replay', events }),
      onEvent: (raw) => dispatch({ t: 'event', raw }),
      onExit: () => dispatch({ t: 'exit' }),
    },
  })

  // Stick to bottom on new content unless the user scrolled up.
  useEffect(() => {
    const el = scrollRef.current
    if (el && atBottomRef.current) el.scrollTop = el.scrollHeight
  }, [state.items])

  const onScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    atBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
  }, [])

  const send = useCallback(
    (text: string) => {
      const t = text.trim()
      if (!t) return
      sendClaudeInput(t)
      dispatch({ t: 'send', text: t })
      atBottomRef.current = true
    },
    [sendClaudeInput],
  )

  const onPermission = useCallback(
    (id: string, allow: boolean) => {
      sendPermission(id, allow)
      dispatch({ t: 'permission', id, allowed: allow })
    },
    [sendPermission],
  )

  return (
    <div className="flex h-full min-h-0 flex-col bg-zinc-950">
      <UsageHudBar state={state} phase={phase} />

      <div ref={scrollRef} onScroll={onScroll} className="term-scroll min-h-0 flex-1 overflow-y-auto px-4 py-5">
        <div className="mx-auto flex max-w-3xl flex-col gap-4">
          {state.items.length === 0 && phase !== 'connecting' && <EmptyChat />}
          {state.items.length === 0 && phase === 'connecting' && <ConnectingChat />}
          {state.items.map((it) => (
            <ChatItemView key={it.id} item={it} onPermission={onPermission} />
          ))}
          {state.busy && <ThinkingRow />}
        </div>
      </div>

      <Composer onSend={send} busy={state.busy} disabled={phase === 'exited'} />
    </div>
  )
}

function UsageHudBar({ state, phase }: { state: ClaudeState; phase: string }) {
  const u = state.usage
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-zinc-800 bg-zinc-900/40 px-4 py-1.5 font-mono text-[10px] uppercase tracking-wider text-zinc-500">
      <span className="flex items-center gap-1.5">
        <span className={`inline-block h-1.5 w-1.5 rounded-full ${phaseDot(phase)}`} />
        {state.model ?? 'claude'}
      </span>
      <Metric label="in" value={fmt(u.inputTokens)} />
      <Metric label="out" value={fmt(u.outputTokens)} />
      {u.cacheRead > 0 && <Metric label="cache" value={fmt(u.cacheRead)} />}
      <span className="ml-auto flex items-center gap-3">
        {u.numTurns > 0 && <Metric label="turns" value={String(u.numTurns)} />}
        <span className="text-emerald-400">${u.costUsd.toFixed(4)}</span>
      </span>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <span>
      <span className="text-zinc-600">{label}</span> <span className="tabular-nums text-zinc-300">{value}</span>
    </span>
  )
}

function Composer({
  onSend,
  busy,
  disabled,
}: {
  onSend: (text: string) => void
  busy: boolean
  disabled: boolean
}) {
  const [text, setText] = useState('')
  const taRef = useRef<HTMLTextAreaElement>(null)

  const grow = useCallback(() => {
    const ta = taRef.current
    if (!ta) return
    ta.style.height = 'auto'
    ta.style.height = `${Math.min(ta.scrollHeight, 200)}px`
  }, [])

  const submit = () => {
    if (disabled || !text.trim()) return
    onSend(text)
    setText('')
    requestAnimationFrame(grow)
  }

  return (
    <div className="border-t border-zinc-800 bg-zinc-900/40 px-4 py-3">
      <div className="mx-auto flex max-w-3xl items-end gap-2.5 rounded-xl border border-zinc-800 bg-zinc-950 px-3 py-2 focus-within:border-emerald-500/50">
        <textarea
          ref={taRef}
          rows={1}
          value={text}
          disabled={disabled}
          onChange={(e) => {
            setText(e.target.value)
            grow()
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
          placeholder={disabled ? 'session ended' : 'Message Claude…  (Enter to send · Shift+Enter for newline)'}
          className="term-scroll max-h-[200px] min-h-[24px] w-full resize-none bg-transparent font-display text-[13.5px] leading-relaxed text-zinc-100 placeholder:text-zinc-600 focus:outline-none disabled:opacity-50"
        />
        <button
          type="button"
          onClick={submit}
          disabled={disabled || !text.trim()}
          title="send"
          className="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-emerald-500 text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:bg-zinc-800 disabled:text-zinc-600"
        >
          {busy ? <Spinner /> : <SendIcon />}
        </button>
      </div>
    </div>
  )
}

function ThinkingRow() {
  return (
    <div className="ml-9 flex items-center gap-2 font-mono text-[11px] text-zinc-500">
      <span className="flex gap-1">
        <Dot delay="0ms" />
        <Dot delay="160ms" />
        <Dot delay="320ms" />
      </span>
      working…
    </div>
  )
}

function Dot({ delay }: { delay: string }) {
  return <span className="h-1.5 w-1.5 rounded-full bg-emerald-400/80 animate-breathe" style={{ animationDelay: delay }} />
}

function EmptyChat() {
  return (
    <div className="grid place-items-center py-20 text-center">
      <div className="max-w-sm">
        <div className="mx-auto grid h-12 w-12 place-items-center rounded-xl border border-emerald-500/30 bg-emerald-500/10">
          <svg viewBox="0 0 24 24" className="h-6 w-6 text-emerald-400" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
            <path d="M12 3v18M3 12h18" strokeLinecap="round" />
          </svg>
        </div>
        <p className="mt-4 font-display text-sm font-semibold text-zinc-300">Claude is live in this project</p>
        <p className="mt-1 font-mono text-[11px] text-zinc-600">send a message to begin the conversation</p>
      </div>
    </div>
  )
}

function ConnectingChat() {
  return (
    <div className="grid place-items-center py-20 text-center">
      <p className="font-mono text-xs text-zinc-600">attaching to session…</p>
    </div>
  )
}

function fmt(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

function phaseDot(phase: string): string {
  switch (phase) {
    case 'live':
      return 'bg-emerald-400'
    case 'connecting':
      return 'bg-amber-400 animate-breathe'
    case 'exited':
      return 'bg-zinc-600'
    default:
      return 'bg-red-500'
  }
}

function SendIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M5 12h14M13 6l6 6-6 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function Spinner() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4 animate-spin" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="M12 3a9 9 0 1 0 9 9" strokeLinecap="round" />
    </svg>
  )
}

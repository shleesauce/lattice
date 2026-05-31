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
    <div className="chat" style={{ height: '100%', minHeight: 0 }}>
      <ChatHeader phase={phase} model={state.model} usage={state.usage} />

      <div ref={scrollRef} onScroll={onScroll} className="chat-body">
        {state.items.length === 0 && phase !== 'connecting' && <EmptyChat />}
        {state.items.length === 0 && phase === 'connecting' && <ConnectingChat />}
        {state.items.map((it) => (
          <ChatItemView key={it.id} item={it} onPermission={onPermission} />
        ))}
        {state.busy && <ThinkingRow />}
      </div>

      <div className="chat-foot">
        <Composer onSend={send} busy={state.busy} disabled={phase === 'exited'} />
      </div>
    </div>
  )
}

// ---- Header ----

interface UsageHudLike {
  inputTokens: number
  outputTokens: number
  cacheRead: number
  costUsd: number
  numTurns: number
}

function ChatHeader({
  phase,
  model,
  usage,
}: {
  phase: string
  model?: string
  usage: UsageHudLike
}) {
  const u = usage
  return (
    <div style={{ display: 'flex', flexDirection: 'column', borderBottom: '1px solid var(--border)' }}>
      <div className="chat-h">
        <span className="av">
          <SparklesIcon />
        </span>
        <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minWidth: 0 }}>
          <span className="nm">Claude</span>
          <span className="on-node">
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
              <span
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: '999px',
                  background: phaseDotColor(phase),
                  flexShrink: 0,
                  display: 'inline-block',
                }}
              />
              {model ?? 'claude'} · {phaseLabel(phase)}
            </span>
          </span>
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
          {u.numTurns > 0 && (
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--teal)' }}>
              ${u.costUsd.toFixed(4)}
            </span>
          )}
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              color: 'var(--fg-3)',
              display: 'flex',
              gap: 8,
            }}
          >
            {u.inputTokens > 0 && <span>{fmt(u.inputTokens)}in</span>}
            {u.outputTokens > 0 && <span>{fmt(u.outputTokens)}out</span>}
          </span>
        </div>
      </div>
    </div>
  )
}

// ---- Composer ----

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
    ta.style.height = `${Math.min(ta.scrollHeight, 180)}px`
  }, [])

  const submit = () => {
    if (disabled || !text.trim()) return
    onSend(text)
    setText('')
    requestAnimationFrame(grow)
  }

  return (
    <div className="composer">
      <textarea
        ref={taRef}
        rows={2}
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
        placeholder={disabled ? 'session ended' : 'Ask Claude, or describe a change…'}
        style={{ maxHeight: 180 }}
      />
      <div className="composer-row">
        <div className="left">
          <button
            type="button"
            className="iconbtn"
            style={{ width: 28, height: 28 }}
            title="Attach file"
            aria-label="Attach file"
          >
            <FolderIconSmall />
          </button>
          <button
            type="button"
            className="iconbtn"
            style={{ width: 28, height: 28 }}
            title="Terminal context"
            aria-label="Terminal context"
          >
            <TerminalIconSmall />
          </button>
        </div>
        <button
          type="button"
          className="send"
          onClick={submit}
          disabled={disabled || !text.trim()}
          title="Send"
        >
          {busy ? <SpinnerSend /> : <SendIcon />}
        </button>
      </div>
    </div>
  )
}

// ---- Thinking ----

function ThinkingRow() {
  return (
    <div className="thinking">
      <span className="d" />
      <span className="d" style={{ animationDelay: '0.2s' }} />
      <span className="d" style={{ animationDelay: '0.4s' }} />
      thinking…
    </div>
  )
}

// ---- Empty / Connecting states ----

function EmptyChat() {
  return (
    <div style={{ display: 'grid', placeItems: 'center', padding: '60px 0', textAlign: 'center' }}>
      <div style={{ maxWidth: 260 }}>
        <div style={{
          margin: '0 auto 16px',
          width: 44,
          height: 44,
          borderRadius: 12,
          border: '1px solid color-mix(in oklch, var(--amber) 30%, transparent)',
          background: 'var(--fill-warm)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: 'var(--amber)',
        }}>
          <SparklesIcon />
        </div>
        <p style={{ margin: 0, fontFamily: 'var(--font-ui)', fontSize: 13, fontWeight: 600, color: 'var(--fg-1)' }}>
          Claude is live in this project
        </p>
        <p style={{ margin: '4px 0 0', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-3)' }}>
          send a message to begin
        </p>
      </div>
    </div>
  )
}

function ConnectingChat() {
  return (
    <div style={{ display: 'grid', placeItems: 'center', padding: '60px 0', textAlign: 'center' }}>
      <p style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-3)', margin: 0 }}>
        attaching to session…
      </p>
    </div>
  )
}

// ---- Helpers ----

function fmt(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

function phaseDotColor(phase: string): string {
  switch (phase) {
    case 'live':
      return 'var(--green)'
    case 'connecting':
      return 'var(--st-starting)'
    case 'exited':
      return 'var(--st-exited)'
    default:
      return 'var(--st-danger)'
  }
}

function phaseLabel(phase: string): string {
  switch (phase) {
    case 'live':
      return 'live'
    case 'connecting':
      return 'connecting'
    case 'exited':
      return 'session ended'
    default:
      return phase
  }
}

// ---- Icons ----

function SparklesIcon() {
  return (
    <svg viewBox="0 0 24 24" width={14} height={14} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M12 3l1.5 4.5L18 9l-4.5 1.5L12 15l-1.5-4.5L6 9l4.5-1.5z" />
      <path d="M19 3l.75 2.25L22 6l-2.25.75L19 9l-.75-2.25L16 6l2.25-.75z" />
      <path d="M5 17l.5 1.5L7 19l-1.5.5L5 21l-.5-1.5L3 19l1.5-.5z" />
    </svg>
  )
}

function SendIcon() {
  return (
    <svg viewBox="0 0 24 24" width={15} height={15} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <line x1="22" y1="2" x2="11" y2="13" />
      <polygon points="22 2 15 22 11 13 2 9 22 2" />
    </svg>
  )
}

function SpinnerSend() {
  return (
    <svg viewBox="0 0 24 24" width={15} height={15} fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden
      style={{ animation: 'spin 1s linear infinite' }}>
      <path d="M12 3a9 9 0 1 0 9 9" />
    </svg>
  )
}

function FolderIconSmall() {
  return (
    <svg viewBox="0 0 24 24" width={15} height={15} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
    </svg>
  )
}

function TerminalIconSmall() {
  return (
    <svg viewBox="0 0 24 24" width={15} height={15} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    </svg>
  )
}

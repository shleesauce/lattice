import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { sessionWsUrl } from '../../api'
import { decodeB64, encodeB64 } from '../../lib/b64'

export interface XtermSessionHandle {
  sendInput: (data: string) => void
}

interface Props {
  sessionId: string
  // Overrides the live-phase header label (e.g. "claude"). Defaults to the
  // generic terminal phase labels.
  liveLabel?: string
  // Override the socket URL (cols/rows passed in) — e.g. the dock's ad-hoc
  // /ws/terminal?agent=… PTY. Defaults to the persistent /ws/session socket.
  makeUrl?: (cols: number, rows: number) => string
  // Sent once after the socket opens (e.g. `cd <project>\n` for context).
  initialInput?: string
  // Hide the built-in status header (the dock supplies its own chrome).
  bare?: boolean
}

type Phase = 'connecting' | 'live' | 'exited' | 'error'

// The xterm "fleet console" theme — shared across every PTY-backed session.
const theme = {
  background: '#09090b',
  foreground: '#e4e4e7',
  cursor: '#34d399',
  cursorAccent: '#09090b',
  selectionBackground: '#27272a',
  black: '#18181b',
  red: '#f87171',
  green: '#34d399',
  yellow: '#fbbf24',
  blue: '#60a5fa',
  magenta: '#c084fc',
  cyan: '#22d3ee',
  white: '#e4e4e7',
  brightBlack: '#52525b',
  brightRed: '#fca5a5',
  brightGreen: '#6ee7b7',
  brightYellow: '#fcd34d',
  brightBlue: '#93c5fd',
  brightMagenta: '#d8b4fe',
  brightCyan: '#67e8f9',
  brightWhite: '#fafafa',
}

// Attaches xterm to the long-lived /ws/session socket. Used by both the Terminal
// and Claude tabs — since D35, an interactive `claude` runs in a PTY and speaks
// the identical replay/output/input frames as a terminal. Closing this does NOT
// kill the process — the hub keeps it alive and replays scrollback on reattach.
// We manage the socket directly here (rather than a hook) so xterm sizing and the
// first resize stay in lockstep.
export const XtermSession = forwardRef<XtermSessionHandle, Props>(function XtermSession(
  { sessionId, liveLabel, makeUrl, initialInput, bare }: Props,
  ref,
) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [phase, setPhase] = useState<Phase>('connecting')
  // Stable ref to the live WebSocket so imperative sendInput can reach it.
  const wsRef = useRef<WebSocket | null>(null)

  // Hold the latest makeUrl/initialInput in refs so the socket effect can read
  // them without listing them as deps — they may be recreated by the parent each
  // render, and the effect must open the socket ONCE per sessionId (not reopen on
  // every parent re-render, which would drop scrollback).
  const makeUrlRef = useRef(makeUrl)
  const initialInputRef = useRef(initialInput)
  makeUrlRef.current = makeUrl
  initialInputRef.current = initialInput

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    setPhase('connecting')

    // xterm + addons are created ONCE per sessionId. The socket is (re)opened by
    // connect() below — a dropped socket auto-reconnects without recreating the
    // terminal, so scrollback survives and the hub replays the session on reattach.
    const term = new Terminal({
      fontFamily: "'IBM Plex Mono', ui-monospace, monospace",
      fontSize: 12.5,
      lineHeight: 1.2,
      cursorBlink: true,
      theme,
      allowProposedApi: true,
      scrollback: 5000,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)

    const safeFit = () => {
      try {
        fit.fit()
      } catch {
        /* container not measurable yet */
      }
    }
    safeFit()

    const urlFor = makeUrlRef.current ?? ((c: number, r: number) => sessionWsUrl(sessionId, c, r))

    let disposed = false
    let exited = false
    let firstConnect = true
    let attempt = 0
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined

    // Only send a resize when the dimensions actually changed, debounced — so
    // dragging the panel split doesn't spam the PTY with resizes that make the
    // shell re-print its prompt (BUG-006). fit() still runs every observer tick so
    // the view stays fitted; only the network message is throttled.
    let lastCols = 0
    let lastRows = 0
    let resizeTimer: ReturnType<typeof setTimeout> | undefined
    const sendResize = () => {
      const ws = wsRef.current
      if (!ws || ws.readyState !== WebSocket.OPEN) return
      if (term.cols === lastCols && term.rows === lastRows) return
      lastCols = term.cols
      lastRows = term.rows
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }

    const scheduleReconnect = () => {
      if (disposed || reconnectTimer) return
      attempt += 1
      const delay = Math.min(1000 * 2 ** (attempt - 1), 15000) // 1s,2s,4s… capped 15s
      reconnectTimer = setTimeout(() => {
        reconnectTimer = undefined
        if (!disposed) connect()
      }, delay)
    }

    const connect = () => {
      if (disposed) return
      const ws = new WebSocket(urlFor(term.cols, term.rows))
      wsRef.current = ws

      ws.onopen = () => {
        if (disposed) return
        attempt = 0
        setPhase('live')
        safeFit()
        lastCols = term.cols
        lastRows = term.rows
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
        // Seed input (e.g. `cd <project>`) only on the FIRST attach — on a reconnect
        // the hub replays scrollback, so re-sending it would re-run the command.
        if (firstConnect && initialInputRef.current) {
          ws.send(JSON.stringify({ type: 'input', data: encodeB64(new TextEncoder().encode(initialInputRef.current)) }))
        }
        firstConnect = false
        term.focus()
      }

      ws.onmessage = (ev) => {
        let msg: { type: string; kind?: string; data?: string }
        try {
          msg = JSON.parse(ev.data as string)
        } catch {
          return
        }
        // Both terminal and claude sessions replay as base64 PTY bytes (D35), so do
        // not gate replay on kind — a claude replay arrives with kind="claude".
        if ((msg.type === 'replay' && msg.data) || (msg.type === 'output' && msg.data)) {
          term.write(decodeB64(msg.data))
        } else if (msg.type === 'exit') {
          exited = true
          setPhase('exited')
          term.write('\r\n\x1b[2m— session ended —\x1b[0m\r\n')
        }
      }

      ws.onclose = () => {
        if (disposed || exited) return
        wsRef.current = null
        // An unexpected drop (mobile network blip, sleep) auto-reconnects with
        // backoff; the "reattaching…" overlay is now truthful (it used to sit there
        // forever until a manual page refresh — BUG-002).
        setPhase('error')
        scheduleReconnect()
      }
      ws.onerror = () => {
        /* onclose fires next and owns the reconnect */
      }
    }

    connect()

    const onData = term.onData((data) => {
      const ws = wsRef.current
      if (!ws || ws.readyState !== WebSocket.OPEN) return
      ws.send(JSON.stringify({ type: 'input', data: encodeB64(new TextEncoder().encode(data)) }))
    })

    const ro = new ResizeObserver(() => {
      safeFit()
      if (resizeTimer) clearTimeout(resizeTimer)
      resizeTimer = setTimeout(sendResize, 150)
    })
    ro.observe(host)

    return () => {
      disposed = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      if (resizeTimer) clearTimeout(resizeTimer)
      ro.disconnect()
      onData.dispose()
      const ws = wsRef.current
      wsRef.current = null
      if (ws) {
        ws.onclose = null // don't let teardown trip the reconnect path
        ws.close()
      }
      term.dispose()
    }
  }, [sessionId])

  useImperativeHandle(ref, () => ({
    sendInput(data: string) {
      const ws = wsRef.current
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'input', data: encodeB64(new TextEncoder().encode(data)) }))
      }
    },
  }))

  return (
    <div className="flex h-full min-h-0 flex-col bg-zinc-950">
      {!bare && (
        <div className="flex items-center gap-2 border-b border-zinc-800 px-4 py-2 font-mono text-[10px] uppercase tracking-wider">
          <span className={phaseDot(phase)} />
          <span className="text-zinc-500">{phaseLabel(phase, liveLabel)}</span>
          <span className="ml-auto truncate text-zinc-600">{sessionId.slice(0, 8)}</span>
        </div>
      )}
      <div className="relative min-h-0 flex-1">
        {phase === 'error' && (
          <div className="absolute inset-0 z-10 grid place-items-center bg-zinc-950/85 px-6 text-center">
            <p className="font-mono text-xs text-red-400">session connection lost — reattaching…</p>
          </div>
        )}
        <div ref={hostRef} className="term-scroll absolute inset-0 px-3 py-2" />
      </div>
    </div>
  )
})

function phaseLabel(p: Phase, liveLabel?: string): string {
  switch (p) {
    case 'connecting':
      return 'attaching…'
    case 'live':
      return liveLabel ?? 'live'
    case 'exited':
      return 'session ended'
    case 'error':
      return 'reattaching'
  }
}

function phaseDot(p: Phase): string {
  const base = 'inline-block h-1.5 w-1.5 rounded-full '
  switch (p) {
    case 'live':
      return base + 'bg-emerald-400'
    case 'connecting':
      return base + 'bg-amber-400 animate-breathe'
    case 'error':
      return base + 'bg-red-500'
    case 'exited':
      return base + 'bg-zinc-600'
  }
}

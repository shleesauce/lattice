import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { sessionWsUrl } from '../../api'
import { decodeB64, encodeB64 } from '../../lib/b64'

interface Props {
  sessionId: string
  // Overrides the live-phase header label (e.g. "claude"). Defaults to the
  // generic terminal phase labels.
  liveLabel?: string
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
export function XtermSession({ sessionId, liveLabel }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [phase, setPhase] = useState<Phase>('connecting')

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    setPhase('connecting')

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

    const ws = new WebSocket(sessionWsUrl(sessionId, term.cols, term.rows))
    let closedByUs = false

    ws.onopen = () => {
      setPhase('live')
      safeFit()
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
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
        setPhase('exited')
        term.write('\r\n\x1b[2m— session ended —\x1b[0m\r\n')
      }
    }

    ws.onclose = () => {
      if (!closedByUs) setPhase((p) => (p === 'exited' ? p : 'error'))
    }
    ws.onerror = () => setPhase('error')

    const onData = term.onData((data) => {
      if (ws.readyState !== WebSocket.OPEN) return
      ws.send(JSON.stringify({ type: 'input', data: encodeB64(new TextEncoder().encode(data)) }))
    })

    const ro = new ResizeObserver(() => {
      safeFit()
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      }
    })
    ro.observe(host)

    return () => {
      closedByUs = true
      ro.disconnect()
      onData.dispose()
      ws.close()
      term.dispose()
    }
  }, [sessionId])

  return (
    <div className="flex h-full min-h-0 flex-col bg-zinc-950">
      <div className="flex items-center gap-2 border-b border-zinc-800 px-4 py-2 font-mono text-[10px] uppercase tracking-wider">
        <span className={phaseDot(phase)} />
        <span className="text-zinc-500">{phaseLabel(phase, liveLabel)}</span>
        <span className="ml-auto truncate text-zinc-600">{sessionId.slice(0, 8)}</span>
      </div>
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
}

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

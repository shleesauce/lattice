import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { terminalWsUrl } from '../api'

interface Props {
  agentId: string
  online: boolean
}

type Phase = 'connecting' | 'live' | 'ended' | 'error'

// b64 helpers for the JSON-framed terminal transport.
function encode(bytes: Uint8Array): string {
  let s = ''
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i])
  return btoa(s)
}
function decode(b64: string): Uint8Array {
  const s = atob(b64)
  const out = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
  return out
}

// The xterm "fleet console" theme — zinc/neutral-950 surface, emerald cursor.
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

export function TerminalView({ agentId, online }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [phase, setPhase] = useState<Phase>('connecting')

  // A fresh session per agent: remount via key in the parent keeps this clean.
  useEffect(() => {
    if (!online) {
      setPhase('error')
      return
    }
    const host = hostRef.current
    if (!host) return

    setPhase('connecting')

    const term = new Terminal({
      fontFamily: "'JetBrains Mono', ui-monospace, monospace",
      fontSize: 12.5,
      lineHeight: 1.2,
      cursorBlink: true,
      theme,
      allowProposedApi: true,
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

    const ws = new WebSocket(terminalWsUrl(agentId, term.cols, term.rows))
    let closedByUs = false

    ws.onopen = () => {
      setPhase('live')
      safeFit()
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      term.focus()
    }

    ws.onmessage = (ev) => {
      let msg: { type: string; data?: string }
      try {
        msg = JSON.parse(ev.data as string)
      } catch {
        return
      }
      if (msg.type === 'output' && msg.data) {
        term.write(decode(msg.data))
      } else if (msg.type === 'exit') {
        setPhase('ended')
        term.write('\r\n\x1b[2m— session ended —\x1b[0m\r\n')
      }
    }

    ws.onclose = () => {
      if (!closedByUs) setPhase((p) => (p === 'ended' ? p : 'ended'))
    }
    ws.onerror = () => setPhase('error')

    const onData = term.onData((data) => {
      if (ws.readyState !== WebSocket.OPEN) return
      ws.send(JSON.stringify({ type: 'input', data: encode(new TextEncoder().encode(data)) }))
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
  }, [agentId, online])

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b border-zinc-800 px-4 py-2 font-mono text-[10px] uppercase tracking-wider">
        <span className={phaseDot(phase)} />
        <span className="text-zinc-500">{phaseLabel(phase)}</span>
        <span className="ml-auto text-zinc-600">{agentId}</span>
      </div>
      <div className="relative min-h-0 flex-1 bg-zinc-950">
        {!online && (
          <div className="absolute inset-0 z-10 grid place-items-center bg-zinc-950/80 px-6 text-center">
            <p className="font-mono text-xs text-zinc-500">agent offline — terminal unavailable</p>
          </div>
        )}
        {phase === 'error' && online && (
          <div className="absolute inset-0 z-10 grid place-items-center bg-zinc-950/80 px-6 text-center">
            <p className="font-mono text-xs text-red-400">terminal connection failed</p>
          </div>
        )}
        <div ref={hostRef} className="term-scroll absolute inset-0 px-3 py-2" />
      </div>
    </div>
  )
}

function phaseLabel(p: Phase): string {
  switch (p) {
    case 'connecting':
      return 'opening session…'
    case 'live':
      return 'live'
    case 'ended':
      return 'session ended'
    case 'error':
      return 'error'
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
    case 'ended':
      return base + 'bg-zinc-600'
  }
}

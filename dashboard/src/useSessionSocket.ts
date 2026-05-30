import { useCallback, useEffect, useRef, useState } from 'react'
import { sessionWsUrl } from './api'
import type { ClaudeRaw, SessionInbound } from './types'

export type SocketPhase = 'connecting' | 'live' | 'exited' | 'error'

interface TerminalSink {
  // Called for replay scrollback (once) then each live output frame.
  onData: (bytes: Uint8Array) => void
  onExit?: () => void
}

interface ClaudeSink {
  // Replay batch, then live events one at a time.
  onReplay: (events: ClaudeRaw[]) => void
  onEvent: (raw: ClaudeRaw) => void
  onExit?: () => void
}

import { decodeB64, encodeB64 } from './lib/b64'

interface SessionSocket {
  phase: SocketPhase
  sendInput: (data: Uint8Array) => void
  sendResize: (cols: number, rows: number) => void
  sendClaudeInput: (text: string) => void
  sendPermission: (toolUseId: string, allow: boolean) => void
}

// One long-lived attach to /ws/session. The kind decides which sink receives
// frames; both replay then stream. Detach is implicit on socket close (the hub
// keeps the process alive — D18). We do NOT auto-reconnect aggressively: a single
// retry keeps reattach cheap without fighting an intentionally-closed session.
export function useSessionSocket(
  sessionId: string,
  kind: 'terminal' | 'claude',
  sinks: { terminal?: TerminalSink; claude?: ClaudeSink },
  initialCols = 80,
  initialRows = 24,
): SessionSocket {
  const [phase, setPhase] = useState<SocketPhase>('connecting')
  const wsRef = useRef<WebSocket | null>(null)
  const sinkRef = useRef(sinks)
  sinkRef.current = sinks

  useEffect(() => {
    let closedByUs = false
    let retried = false
    let retryTimer: ReturnType<typeof setTimeout> | null = null

    const connect = () => {
      setPhase('connecting')
      const ws = new WebSocket(sessionWsUrl(sessionId, initialCols, initialRows))
      wsRef.current = ws

      ws.onopen = () => setPhase('live')

      ws.onmessage = (ev) => {
        let msg: SessionInbound
        try {
          msg = JSON.parse(ev.data as string) as SessionInbound
        } catch {
          return
        }
        switch (msg.type) {
          case 'replay':
            if (msg.kind === 'terminal') sinkRef.current.terminal?.onData(decodeB64(msg.data))
            else sinkRef.current.claude?.onReplay(msg.events ?? [])
            break
          case 'output':
            sinkRef.current.terminal?.onData(decodeB64(msg.data))
            break
          case 'claude_event':
            sinkRef.current.claude?.onEvent(msg.raw)
            break
          case 'exit':
            setPhase('exited')
            sinkRef.current.terminal?.onExit?.()
            sinkRef.current.claude?.onExit?.()
            break
        }
      }

      ws.onerror = () => setPhase('error')

      ws.onclose = () => {
        wsRef.current = null
        if (closedByUs) return
        setPhase((p) => (p === 'exited' ? p : 'error'))
        if (!retried) {
          retried = true
          retryTimer = setTimeout(connect, 1500)
        }
      }
    }

    connect()

    return () => {
      closedByUs = true
      if (retryTimer) clearTimeout(retryTimer)
      wsRef.current?.close()
      wsRef.current = null
    }
    // initialCols/Rows only seed the first connect; live resizes go through sendResize.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, kind])

  const send = useCallback((obj: unknown) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj))
  }, [])

  const sendInput = useCallback(
    (data: Uint8Array) => send({ type: 'input', data: encodeB64(data) }),
    [send],
  )
  const sendResize = useCallback(
    (cols: number, rows: number) => send({ type: 'resize', cols, rows }),
    [send],
  )
  const sendClaudeInput = useCallback((text: string) => send({ type: 'claude_input', text }), [send])
  const sendPermission = useCallback(
    (toolUseId: string, allow: boolean) => send({ type: 'claude_permission', toolUseId, allow }),
    [send],
  )

  return { phase, sendInput, sendResize, sendClaudeInput, sendPermission }
}

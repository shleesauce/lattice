import { useCallback, useEffect, useRef, useState } from 'react'
import { dashboardWsUrl, fetchFleet, fetchHealth } from './api'
import type { Agent, CommandRun, DashboardEvent, Health } from './types'

export type ConnState = 'connecting' | 'live' | 'down'

interface FleetState {
  agents: Agent[]
  health: Health | null
  loading: boolean
  error: string | null
  conn: ConnState
  runs: Record<string, CommandRun>
  registerRun: (cmdId: string, agentId: string, command: string) => void
}

export function useFleet(): FleetState {
  const [agents, setAgents] = useState<Agent[]>([])
  const [health, setHealth] = useState<Health | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [conn, setConn] = useState<ConnState>('connecting')
  const [runs, setRuns] = useState<Record<string, CommandRun>>({})
  const wsRef = useRef<WebSocket | null>(null)
  const retryRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const registerRun = useCallback((cmdId: string, agentId: string, command: string) => {
    setRuns((prev) => ({
      ...prev,
      [cmdId]: {
        cmdId,
        agentId,
        command,
        lines: [],
        finished: false,
        exitCode: null,
        error: '',
        startedAt: Date.now(),
      },
    }))
  }, [])

  // Initial REST snapshot — independent of the WS so the grid paints fast.
  useEffect(() => {
    let cancelled = false
    Promise.all([fetchFleet(), fetchHealth().catch(() => null)])
      .then(([fleet, h]) => {
        if (cancelled) return
        setAgents(fleet)
        setHealth(h)
        setError(null)
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'failed to load fleet')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Live WS with auto-reconnect.
  useEffect(() => {
    let stopped = false

    const connect = () => {
      if (stopped) return
      setConn((c) => (c === 'live' ? c : 'connecting'))
      const ws = new WebSocket(dashboardWsUrl())
      wsRef.current = ws

      ws.onopen = () => setConn('live')

      ws.onmessage = (ev) => {
        let msg: DashboardEvent
        try {
          msg = JSON.parse(ev.data as string)
        } catch {
          return
        }
        if (msg.type === 'fleet') {
          setAgents(msg.agents ?? [])
        } else if (msg.type === 'output') {
          setRuns((prev) => {
            const run = prev[msg.cmdId]
            if (!run) return prev
            return {
              ...prev,
              [msg.cmdId]: { ...run, lines: [...run.lines, { stream: msg.stream, data: msg.data }] },
            }
          })
        } else if (msg.type === 'exit') {
          setRuns((prev) => {
            const run = prev[msg.cmdId]
            if (!run) return prev
            return {
              ...prev,
              [msg.cmdId]: { ...run, finished: true, exitCode: msg.exitCode, error: msg.error ?? '' },
            }
          })
        }
      }

      ws.onclose = () => {
        wsRef.current = null
        if (stopped) return
        setConn('down')
        retryRef.current = setTimeout(connect, 2000)
      }

      ws.onerror = () => ws.close()
    }

    connect()
    return () => {
      stopped = true
      if (retryRef.current) clearTimeout(retryRef.current)
      wsRef.current?.close()
    }
  }, [])

  return { agents, health, loading, error, conn, runs, registerRun }
}

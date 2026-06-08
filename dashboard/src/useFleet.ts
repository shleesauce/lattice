import { useCallback, useRef, useState } from 'react'
import { fetchFleet, fetchHealth } from './api'
import { useLiveResource } from './useLiveResource'
import type { Agent, DashboardEvent, Health } from './types'

export type { ConnState } from './useDashboardSocket'

interface FleetState {
  agents: Agent[]
  health: Health | null
  loading: boolean
  error: string | null
  conn: import('./useDashboardSocket').ConnState
}

export function useFleet(): FleetState {
  const [agents, setAgents] = useState<Agent[]>([])
  const [health, setHealth] = useState<Health | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const aliveRef = useRef(true)

  // REST snapshot — independent of the WS so the grid paints fast. Also re-run on
  // socket reconnect, since the hub only pushes on the next change.
  const refresh = useCallback(() => {
    return Promise.all([fetchFleet(), fetchHealth().catch(() => null)])
      .then(([fleet, h]) => {
        if (!aliveRef.current) return
        setAgents(fleet)
        setHealth(h)
        setError(null)
      })
      .catch((e: unknown) => {
        if (aliveRef.current) setError(e instanceof Error ? e.message : 'failed to load fleet')
      })
      .finally(() => {
        if (aliveRef.current) setLoading(false)
      })
  }, [])

  // Live updates over the shared dashboard socket carry the full agent list.
  const onMessage = useCallback((msg: DashboardEvent) => {
    if (msg.type === 'fleet') setAgents(msg.agents ?? [])
  }, [])

  const { conn } = useLiveResource({
    aliveRef,
    onMount: refresh,
    onReconnect: refresh,
    onEvent: onMessage,
  })

  return { agents, health, loading, error, conn }
}

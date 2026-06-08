import { useEffect } from 'react'
import { useDashboardReconnect, useDashboardSocket, type ConnState } from './useDashboardSocket'
import type { DashboardEvent } from './types'

interface LiveResourceOpts {
  // Alive guard owned by the caller (its fetch callbacks close over it). This
  // hook manages the true/false lifecycle so a late async resolve can't setState
  // after unmount (and resets to true on StrictMode remount).
  aliveRef: { current: boolean }
  // Initial load on mount.
  onMount: () => void
  // One-shot REST resync after the shared socket reconnects — the hub only pushes
  // on the next mutation, so without this the UI shows stale pre-drop data.
  onReconnect: () => void
  // Handle a frame from the single shared /ws/dashboard socket.
  onEvent: (msg: DashboardEvent) => void
  // Optional steady poll for presence/state the hub re-derives per request.
  poll?: { ms: number; fn: () => void }
}

// Shared lifecycle for the dashboard data hooks (useFleet / useDevices /
// useWorkspace). Each hook keeps its own state shape and fetchers and wires the
// common plumbing through here: the alive guard, initial load, optional poll, the
// single shared dashboard socket, and reconnect resync. Returns the shared
// connection state (useFleet surfaces it as the hub live/down banner).
export function useLiveResource(opts: LiveResourceOpts): { conn: ConnState } {
  const { aliveRef, onMount, onReconnect, onEvent, poll } = opts
  const pollMs = poll?.ms
  const pollFn = poll?.fn

  useEffect(() => {
    aliveRef.current = true
    onMount()
    const t = pollFn && pollMs ? setInterval(pollFn, pollMs) : undefined
    return () => {
      aliveRef.current = false
      if (t) clearInterval(t)
    }
    // Callbacks are expected stable (useCallback) at each call site.
  }, [aliveRef, onMount, pollFn, pollMs])

  const conn = useDashboardSocket(onEvent)
  useDashboardReconnect(onReconnect)
  return { conn }
}

import { useEffect, useState } from 'react'
import { dashboardWsUrl } from './api'
import type { DashboardEvent } from './types'

export type ConnState = 'connecting' | 'live' | 'down'

type Listener = (msg: DashboardEvent) => void
type ConnListener = (conn: ConnState) => void

// One process-wide connection to /ws/dashboard, shared by every consumer
// (useFleet / useWorkspace / useDevices). Previously each hook opened its own
// socket, so a single hub broadcast was parsed N times and only one of them
// reconnected on drop. This module owns a single socket + a single reconnect
// lifecycle and fans each parsed frame out to every subscriber.
type EpochListener = (epoch: number) => void

const listeners = new Set<Listener>()
const connListeners = new Set<ConnListener>()
const epochListeners = new Set<EpochListener>()
let socket: WebSocket | null = null
let conn: ConnState = 'connecting'
let retryTimer: ReturnType<typeof setTimeout> | null = null
// Bumped each time the socket goes 'live' AFTER having been 'down' — i.e. a true
// reconnect (not the very first open). Consumers watch this to fire a one-shot
// REST resync, because the hub only pushes on the NEXT mutation; without a resync
// the UI would show stale data from before the drop until something changes.
let reconnectEpoch = 0
let everDown = false

function setConn(next: ConnState) {
  if (conn === next) return
  const prev = conn
  conn = next
  if (next === 'down') everDown = true
  if (next === 'live' && everDown && prev !== 'live') {
    everDown = false
    reconnectEpoch += 1
    for (const l of epochListeners) l(reconnectEpoch)
  }
  for (const l of connListeners) l(conn)
}

function connect() {
  let ws: WebSocket
  try {
    ws = new WebSocket(dashboardWsUrl())
  } catch {
    setConn('down')
    retryTimer = setTimeout(connect, 2000)
    return
  }
  socket = ws
  setConn(conn === 'live' ? conn : 'connecting')

  ws.onopen = () => setConn('live')

  ws.onmessage = (ev) => {
    let msg: DashboardEvent
    try {
      msg = JSON.parse(ev.data as string)
    } catch {
      return
    }
    for (const l of listeners) l(msg)
  }

  ws.onclose = () => {
    if (socket === ws) socket = null
    setConn('down')
    // Keep retrying as long as someone still cares.
    if (listeners.size > 0) retryTimer = setTimeout(connect, 2000)
  }

  ws.onerror = () => ws.close()
}

function ensureOpen() {
  if (socket || retryTimer) return
  connect()
}

function teardownIfIdle() {
  if (listeners.size > 0) return
  if (retryTimer) {
    clearTimeout(retryTimer)
    retryTimer = null
  }
  if (socket) {
    const s = socket
    socket = null
    s.close()
  }
  setConn('connecting')
}

// Subscribe to incoming hub frames. Opens the shared socket on first subscriber
// and closes it once the last one unsubscribes.
export function subscribeDashboard(fn: Listener): () => void {
  listeners.add(fn)
  ensureOpen()
  return () => {
    listeners.delete(fn)
    teardownIfIdle()
  }
}

// React hook wrapper: subscribe to the shared socket and (optionally) track the
// shared connection state. Returns the current ConnState, which is what useFleet
// surfaces to the app for the "hub live / down" banner.
export function useDashboardSocket(onMessage: Listener): ConnState {
  const [connState, setConnState] = useState<ConnState>(conn)

  useEffect(() => {
    const off = subscribeDashboard(onMessage)
    return off
    // onMessage is expected to be stable (useCallback) at each call site.
  }, [onMessage])

  useEffect(() => {
    setConnState(conn)
    connListeners.add(setConnState)
    return () => {
      connListeners.delete(setConnState)
    }
  }, [])

  return connState
}

// Fire `onReconnect` once each time the shared socket comes back up after a drop
// (a hub restart, network blip, etc.). The hub only pushes on the next mutation,
// so consumers use this to trigger a one-shot REST resync and avoid showing stale
// pre-drop data. The callback is expected to be stable (useCallback).
export function useDashboardReconnect(onReconnect: () => void): void {
  useEffect(() => {
    const fn: EpochListener = () => onReconnect()
    epochListeners.add(fn)
    return () => {
      epochListeners.delete(fn)
    }
  }, [onReconnect])
}

import { useCallback, useRef, useState } from 'react'
import { fetchDevices } from './api'
import { useLiveResource } from './useLiveResource'
import type { DashboardEvent, Device } from './types'

// Polls /api/devices (the unified fleet: agents + Tailscale + SSH) and refreshes
// immediately whenever the shared dashboard WS announces a fleet change, so agent
// online/offline transitions reflect fast. Tailscale/SSH presence is poll-only
// (the hub re-derives it per request), so a steady poll keeps phones/PCs current.
const POLL_MS = 6000

export function useDevices(): { devices: Device[]; loading: boolean; refetch: () => Promise<void> } {
  const [devices, setDevices] = useState<Device[]>([])
  const [loading, setLoading] = useState(true)
  const aliveRef = useRef(true)

  const refresh = useCallback(() => {
    return fetchDevices()
      .then((d) => {
        if (aliveRef.current) setDevices(d)
      })
      .catch(() => {})
      .finally(() => {
        if (aliveRef.current) setLoading(false)
      })
  }, [])

  // Piggyback on the shared fleet WS as an instant refresh trigger (the frame
  // doesn't carry device data — the hub re-derives presence per request).
  const onMessage = useCallback(
    (m: DashboardEvent) => {
      if (m.type === 'fleet') void refresh()
    },
    [refresh],
  )

  useLiveResource({
    aliveRef,
    onMount: refresh,
    onReconnect: refresh,
    onEvent: onMessage,
    poll: { ms: POLL_MS, fn: refresh },
  })

  return { devices, loading, refetch: refresh }
}

import { useEffect, useState } from 'react'
import { dashboardWsUrl, fetchDevices } from './api'
import type { Device } from './types'

// Polls /api/devices (the unified fleet: agents + Tailscale + SSH) and refreshes
// immediately whenever the dashboard WS announces a fleet change, so agent
// online/offline transitions reflect fast. Tailscale/SSH presence is poll-only
// (the hub re-derives it per request), so a steady poll keeps phones/PCs current.
const POLL_MS = 6000

export function useDevices(): { devices: Device[]; loading: boolean } {
  const [devices, setDevices] = useState<Device[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    const refresh = () =>
      fetchDevices()
        .then((d) => {
          if (alive) setDevices(d)
        })
        .catch(() => {})
        .finally(() => {
          if (alive) setLoading(false)
        })

    refresh()
    const t = setInterval(refresh, POLL_MS)

    // Piggyback on the dashboard fleet WS as an instant refresh trigger.
    let ws: WebSocket | null = null
    try {
      ws = new WebSocket(dashboardWsUrl())
      ws.onmessage = (ev) => {
        try {
          const m = JSON.parse(ev.data as string)
          if (m.type === 'fleet') refresh()
        } catch {
          /* ignore */
        }
      }
    } catch {
      /* WS optional; polling covers it */
    }

    return () => {
      alive = false
      clearInterval(t)
      ws?.close()
    }
  }, [])

  return { devices, loading }
}

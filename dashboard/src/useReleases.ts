/* Single shared poll of the hub's release feed (/api/releases, v0.1.5). Lifted to
   App so one 5-min poll drives BOTH header surfaces — the version chip and the
   under-header "update available" banner — instead of each polling on its own.
   The hub caches GitHub for 30min, so a light client poll is free. */
import { useEffect, useState } from 'react'
import { fetchReleases } from './api'
import type { ReleasesResponse } from './types'

const RELEASE_POLL_MS = 5 * 60 * 1000

// `data` is the last successful payload (kept across a transient failure so the
// UI doesn't flap); `error` flags that the MOST RECENT check failed. The badge
// uses `error` to avoid implying "up to date" when we simply couldn't reach the
// feed — a stale `data` with `error: true` means "this is old, the recheck broke".
export interface ReleasesState {
  data: ReleasesResponse | null
  error: boolean
}

export function useReleases(): ReleasesState {
  const [state, setState] = useState<ReleasesState>({ data: null, error: false })
  useEffect(() => {
    let cancelled = false
    const load = () =>
      fetchReleases()
        .then((r) => !cancelled && setState({ data: r, error: false }))
        .catch(() => !cancelled && setState((prev) => ({ data: prev.data, error: true })))
    void load()
    const t = setInterval(load, RELEASE_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [])
  return state
}

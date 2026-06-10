/* Single shared poll of the hub's release feed (/api/releases, v0.1.5). Lifted to
   App so one 5-min poll drives BOTH header surfaces — the version chip and the
   under-header "update available" banner — instead of each polling on its own.
   The hub caches GitHub for 30min, so a light client poll is free. */
import { useEffect, useState } from 'react'
import { fetchReleases } from './api'
import type { ReleasesResponse } from './types'

const RELEASE_POLL_MS = 5 * 60 * 1000

export function useReleases(): ReleasesResponse | null {
  const [releases, setReleases] = useState<ReleasesResponse | null>(null)
  useEffect(() => {
    let cancelled = false
    const load = () =>
      fetchReleases()
        .then((r) => !cancelled && setReleases(r))
        .catch(() => {})
    void load()
    const t = setInterval(load, RELEASE_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [])
  return releases
}

import { useCallback, useEffect, useRef, useState } from 'react'
import { dashboardWsUrl, fetchProjects, fetchSessions } from './api'
import type { Project, Session } from './types'

export type LoadState = 'loading' | 'ready' | 'error'

interface WorkspaceState {
  projects: Project[]
  sessions: Session[]
  projectsState: LoadState
  sessionsState: LoadState
  error: string | null
  refreshSessions: () => Promise<void>
  refreshProjects: () => Promise<void>
  // Optimistically inject a freshly-created session so the UI opens it instantly.
  upsertSession: (s: Session) => void
  removeSession: (id: string) => void
}

const SESSION_POLL_MS = 4000

// The sidebar's source of truth. Projects rarely change (filesystem scan), so we
// fetch once; sessions change with placement/status, so we poll. If the backend
// isn't live yet, both degrade to empty + an error banner rather than crashing.
export function useWorkspace(): WorkspaceState {
  const [projects, setProjects] = useState<Project[]>([])
  const [sessions, setSessions] = useState<Session[]>([])
  const [projectsState, setProjectsState] = useState<LoadState>('loading')
  const [sessionsState, setSessionsState] = useState<LoadState>('loading')
  const [error, setError] = useState<string | null>(null)
  const aliveRef = useRef(true)

  const refreshProjects = useCallback(async () => {
    try {
      const p = await fetchProjects()
      if (!aliveRef.current) return
      setProjects(p)
      setProjectsState('ready')
    } catch (e) {
      if (!aliveRef.current) return
      setProjectsState('error')
      setError(e instanceof Error ? e.message : 'failed to load projects')
    }
  }, [])

  const refreshSessions = useCallback(async () => {
    try {
      const s = await fetchSessions()
      if (!aliveRef.current) return
      setSessions(s)
      setSessionsState('ready')
    } catch (e) {
      if (!aliveRef.current) return
      setSessionsState('error')
      setError(e instanceof Error ? e.message : 'failed to load sessions')
    }
  }, [])

  const upsertSession = useCallback((s: Session) => {
    setSessions((prev) => {
      const i = prev.findIndex((x) => x.id === s.id)
      if (i === -1) return [...prev, s]
      const next = [...prev]
      next[i] = s
      return next
    })
  }, [])

  const removeSession = useCallback((id: string) => {
    setSessions((prev) => prev.filter((x) => x.id !== id))
  }, [])

  useEffect(() => {
    aliveRef.current = true
    void refreshProjects()
    void refreshSessions()
    const t = setInterval(() => void refreshSessions(), SESSION_POLL_MS)

    // Real-time: the hub broadcasts a full session snapshot on every mutation
    // (create / archive / trash / restore / status change), so the list reflects
    // instantly instead of waiting for the next poll. Poll stays as a fallback.
    let ws: WebSocket | null = null
    try {
      ws = new WebSocket(dashboardWsUrl())
      ws.onmessage = (ev) => {
        if (!aliveRef.current) return
        try {
          const m = JSON.parse(ev.data as string)
          if (m.type === 'sessions' && Array.isArray(m.sessions)) {
            setSessions(m.sessions as Session[])
            setSessionsState('ready')
          }
        } catch {
          /* ignore non-JSON frames */
        }
      }
    } catch {
      /* WS optional — polling covers it */
    }

    return () => {
      aliveRef.current = false
      clearInterval(t)
      ws?.close()
    }
  }, [refreshProjects, refreshSessions])

  return {
    projects,
    sessions,
    projectsState,
    sessionsState,
    error,
    refreshSessions,
    refreshProjects,
    upsertSession,
    removeSession,
  }
}

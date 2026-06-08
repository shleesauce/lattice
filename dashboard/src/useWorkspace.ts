import { useCallback, useRef, useState } from 'react'
import { fetchProjects, fetchSessions } from './api'
import { useLiveResource } from './useLiveResource'
import type { DashboardEvent, Project, Session } from './types'

export type LoadState = 'loading' | 'ready' | 'error'

export interface WorkspaceState {
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

  // Mount loads both lists; reconnect resyncs both (the hub only pushes on the
  // next mutation, so without this the sidebar stays stale after a blip). The
  // steady poll re-pulls sessions only — projects rarely change.
  const resyncAll = useCallback(() => {
    void refreshProjects()
    void refreshSessions()
  }, [refreshProjects, refreshSessions])

  // Real-time: the hub broadcasts a full session snapshot on every mutation
  // (create / archive / trash / restore / status change) over the shared
  // dashboard socket, so the list reflects instantly instead of waiting for the
  // next poll.
  const onMessage = useCallback((m: DashboardEvent) => {
    if (!aliveRef.current) return
    if (m.type === 'sessions') {
      setSessions(m.sessions)
      setSessionsState('ready')
    }
  }, [])

  useLiveResource({
    aliveRef,
    onMount: resyncAll,
    onReconnect: resyncAll,
    onEvent: onMessage,
    poll: { ms: SESSION_POLL_MS, fn: refreshSessions },
  })

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

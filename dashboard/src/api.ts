import type {
  Agent,
  AuditEntry,
  CreateProjectRequest,
  CreateProjectResult,
  CreateSessionRequest,
  Enroll,
  FileListResult,
  Health,
  PlacementRequest,
  PlacementResult,
  Project,
  Session,
  SessionWithPlacement,
  Settings,
  WakeResult,
} from './types'

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    // Surface a parsed error message when the hub returns one.
    const body = await res.text().catch(() => '')
    throw new Error(body ? `${res.status}: ${body}` : `${res.status} ${res.statusText}`)
  }
  return res.json() as Promise<T>
}

export async function fetchFleet(): Promise<Agent[]> {
  const data = await json<{ agents: Agent[] | null }>(await fetch('/api/fleet'))
  return data.agents ?? []
}

export async function fetchHealth(): Promise<Health> {
  return json<Health>(await fetch('/api/health'))
}

export async function fetchEnroll(): Promise<Enroll> {
  return json<Enroll>(await fetch('/api/enroll'))
}

export async function execCommand(agentId: string, command: string): Promise<string> {
  const res = await fetch(`/api/agents/${encodeURIComponent(agentId)}/exec`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ command }),
  })
  const data = await json<{ cmdId: string }>(res)
  return data.cmdId
}

export async function fetchFiles(agentId: string, path: string): Promise<FileListResult> {
  const qs = new URLSearchParams({ path })
  const res = await fetch(`/api/agents/${encodeURIComponent(agentId)}/files?${qs}`)
  return json<FileListResult>(res)
}

export function downloadUrl(agentId: string, path: string): string {
  const qs = new URLSearchParams({ path })
  return `/api/agents/${encodeURIComponent(agentId)}/download?${qs}`
}

export async function wakeAgent(senderId: string, mac: string): Promise<WakeResult> {
  const res = await fetch(`/api/agents/${encodeURIComponent(senderId)}/wake`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mac }),
  })
  // Wake always returns a typed {ok,error} body, even on 4xx/5xx.
  const data = (await res.json().catch(() => ({}))) as WakeResult
  if (typeof data.ok !== 'boolean') throw new Error(`${res.status} ${res.statusText}`)
  return data
}

// In dev (vite :5173) the WS still targets location.host because vite proxies /ws to the hub.
export function dashboardWsUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/ws/dashboard`
}

export function terminalWsUrl(agentId: string, cols: number, rows: number): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const qs = new URLSearchParams({ agent: agentId, cols: String(cols), rows: String(rows) })
  return `${proto}://${location.host}/ws/terminal?${qs}`
}

// ───────────────────────────── Workspace (Phase 3) ─────────────────────────────

export async function fetchProjects(): Promise<Project[]> {
  const data = await json<{ projects: Project[] | null } | Project[]>(await fetch('/api/projects'))
  if (Array.isArray(data)) return data
  return data.projects ?? []
}

// Onboard a brand-new project. The hub scaffolds the folder, optionally
// registers it in AI-Hub, and (when launchClaude) returns a ready Session.
// On a 400 the hub returns a typed {error} body — json() throws it as
// `${status}: ${body}` so callers can parse the message inline.
export async function createProject(req: CreateProjectRequest): Promise<CreateProjectResult> {
  const res = await fetch('/api/projects', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  return json<CreateProjectResult>(res)
}

export async function fetchSessions(): Promise<Session[]> {
  const data = await json<{ sessions: Session[] | null } | Session[]>(await fetch('/api/sessions'))
  if (Array.isArray(data)) return data
  return data.sessions ?? []
}

export async function createSession(req: CreateSessionRequest): Promise<SessionWithPlacement> {
  // Device sessions carry no project path — the agent resolves its home dir.
  // Strip an empty/undefined projectPath so the hub treats it as device-local.
  const body: CreateSessionRequest = { kind: req.kind }
  if (req.scope) body.scope = req.scope
  if (req.projectPath) body.projectPath = req.projectPath
  if (req.title) body.title = req.title
  if (req.userAgentId) body.userAgentId = req.userAgentId
  if (req.pinAgentId) body.pinAgentId = req.pinAgentId
  const res = await fetch('/api/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return json<SessionWithPlacement>(res)
}

export async function deleteSession(id: string): Promise<void> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok && res.status !== 404) {
    const body = await res.text().catch(() => '')
    throw new Error(body || `${res.status} ${res.statusText}`)
  }
}

export async function resumeSession(
  id: string,
  opts: { userAgentId?: string; pinAgentId?: string } = {},
): Promise<SessionWithPlacement> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(id)}/resume`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(opts),
  })
  return json<SessionWithPlacement>(res)
}

export async function previewPlacement(req: PlacementRequest): Promise<PlacementResult> {
  const res = await fetch('/api/placement', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  return json<PlacementResult>(res)
}

export async function fetchAudit(sessionId: string): Promise<AuditEntry[]> {
  const qs = new URLSearchParams({ session: sessionId })
  const data = await json<{ entries: AuditEntry[] | null } | AuditEntry[]>(
    await fetch(`/api/audit?${qs}`),
  )
  if (Array.isArray(data)) return data
  return data.entries ?? []
}

export async function fetchSettings(): Promise<Settings> {
  return json<Settings>(await fetch('/api/settings'))
}

export async function saveSettings(s: Settings): Promise<Settings> {
  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(s),
  })
  return json<Settings>(res)
}

export function sessionWsUrl(sessionId: string, cols: number, rows: number): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const qs = new URLSearchParams({ session: sessionId, cols: String(cols), rows: String(rows) })
  return `${proto}://${location.host}/ws/session?${qs}`
}

import type {
  Agent,
  AuthStatus,
  CreateEnrollTokenResult,
  Device,
  EnrollToken,
  CreateProjectRequest,
  CreateProjectResult,
  CreateSessionRequest,
  FileListResult,
  Health,
  PlacementRequest,
  PlacementResult,
  Project,
  RootCheck,
  Session,
  SessionWithPlacement,
  Settings,
  SetupStatus,
  SetupSubmit,
  Transcript,
  WakeResult,
} from './types'

// A failed hub request, carrying the structured pieces so callers don't have to
// re-parse `.message`. parseHubError() prefers `.body` but still understands the
// legacy `${status}: ${body}` message shape for plain Errors thrown elsewhere.
// `.message` keeps that same shape for backwards compatibility.
export class HubError extends Error {
  readonly status: number
  readonly body: string
  constructor(status: number, statusText: string, body: string) {
    super(body ? `${status}: ${body}` : `${status} ${statusText}`)
    this.name = 'HubError'
    this.status = status
    this.body = body
  }
}

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    // Surface a parsed error message when the hub returns one.
    const body = await res.text().catch(() => '')
    throw new HubError(res.status, res.statusText, body)
  }
  return res.json() as Promise<T>
}

export async function fetchFleet(): Promise<Agent[]> {
  const data = await json<{ agents: Agent[] | null }>(await fetch('/api/fleet'))
  return data.agents ?? []
}

export async function fetchDevices(): Promise<Device[]> {
  const data = await json<{ devices: Device[] | null }>(await fetch('/api/devices'))
  return data.devices ?? []
}

export async function fetchHealth(): Promise<Health> {
  return json<Health>(await fetch('/api/health'))
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

// Build a same-origin WebSocket URL. In dev (vite :5173) it still targets
// location.host because vite proxies /ws to the hub.
function wsUrl(path: string, params?: Record<string, string>): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const qs = params ? `?${new URLSearchParams(params)}` : ''
  return `${proto}://${location.host}${path}${qs}`
}

export function dashboardWsUrl(): string {
  return wsUrl('/ws/dashboard')
}

export function terminalWsUrl(agentId: string, cols: number, rows: number): string {
  return wsUrl('/ws/terminal', { agent: agentId, cols: String(cols), rows: String(rows) })
}

// ───────────────────────────── Workspace (Phase 3) ─────────────────────────────

export async function fetchProjects(): Promise<Project[]> {
  const data = await json<{ projects: Project[] | null } | Project[]>(await fetch('/api/projects'))
  if (Array.isArray(data)) return data
  return data.projects ?? []
}

// Onboard a brand-new project. The hub scaffolds the folder, optionally
// registers it in the project registry, and (when launchClaude) returns a ready Session.
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

// Fetch a session's saved transcript (F16). Returns {found:false} (not an error)
// when no .jsonl is on disk yet — terminal/editor sessions, or a claude session
// whose transcript hasn't synced from its machine.
export async function fetchTranscript(id: string): Promise<Transcript> {
  return json<Transcript>(await fetch(`/api/sessions/${encodeURIComponent(id)}/transcript`))
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
  if (req.permissionMode) body.permissionMode = req.permissionMode
  const res = await fetch('/api/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return json<SessionWithPlacement>(res)
}

// Trash a session: ends the process and moves it to Trash (recoverable; the hub
// auto-purges after 30 days). This is the default DELETE.
export async function trashSession(id: string): Promise<void> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok && res.status !== 404) {
    const body = await res.text().catch(() => '')
    throw new Error(body || `${res.status} ${res.statusText}`)
  }
}

// Permanently delete a session ("Delete forever" from Trash): drops the row.
export async function deleteSessionForever(id: string): Promise<void> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(id)}?purge=1`, { method: 'DELETE' })
  if (!res.ok && res.status !== 404) {
    const body = await res.text().catch(() => '')
    throw new Error(body || `${res.status} ${res.statusText}`)
  }
}

// Empty Trash: permanently delete every trashed session at once.
export async function emptyTrash(): Promise<number> {
  const res = await fetch('/api/sessions/trash', { method: 'DELETE' })
  const data = await json<{ ok: boolean; purged: number }>(res)
  return data.purged
}

// Archive (hide, keep) or restore a session via PATCH. The row survives.
export async function setSessionArchived(id: string, archived: boolean): Promise<Session> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ archived }),
  })
  return json<Session>(res)
}

// Restore a session out of Trash (deleted=false). Re-trashing goes through
// trashSession (DELETE) instead.
export async function setSessionDeleted(id: string, deleted: boolean): Promise<Session> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ deleted }),
  })
  return json<Session>(res)
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

export async function fetchSettings(): Promise<Settings> {
  return json<Settings>(await fetch('/api/settings'))
}

// The hub settings endpoint writes one whitelisted key at a time ({key,value}).
// Set the primary coding machine (D32); pass "" to clear it.
export async function setPrimaryAgent(agentId: string): Promise<void> {
  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key: 'primary_agent', value: agentId }),
  })
  await json<{ ok: boolean }>(res)
}

// ───────────────────────── Manage mesh (Phase 4 / M3) ─────────────────

// Rename an agent-backed machine. Same-origin so the auth cookie rides along.
export async function renameAgent(id: string, name: string): Promise<{ ok: boolean; name: string }> {
  const res = await fetch(`/api/agents/${encodeURIComponent(id)}/rename`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  return json<{ ok: boolean; id: string; name: string }>(res)
}

// Remove an agent from the mesh. A box that still holds the shared join token can
// re-enroll and reappear — the UI warns about this.
export async function removeAgent(id: string): Promise<void> {
  const res = await fetch(`/api/agents/${encodeURIComponent(id)}/remove`, { method: 'POST' })
  await json<{ ok: boolean }>(res)
}

export async function listEnrollTokens(): Promise<EnrollToken[]> {
  const data = await json<{ tokens: EnrollToken[] | null }>(await fetch('/api/enroll/tokens'))
  return data.tokens ?? []
}

// Mint a join token + the per-OS install one-liners that carry it.
export async function createEnrollToken(label: string): Promise<CreateEnrollTokenResult> {
  const res = await fetch('/api/enroll/tokens', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label }),
  })
  return json<CreateEnrollTokenResult>(res)
}

export async function revokeEnrollToken(token: string): Promise<void> {
  const res = await fetch(`/api/enroll/tokens/${encodeURIComponent(token)}/revoke`, { method: 'POST' })
  await json<{ ok: boolean }>(res)
}

// ───────────────────────── First-run setup (Phase 2 / M3) ─────────────────

// Is the hub configured yet? Drives the dashboard's setup gate. When the hub
// already has a config this returns {needsSetup:false}; the App fails OPEN on a
// network error (treats the hub as configured) so a blip never locks the user out.
export async function getSetupStatus(): Promise<SetupStatus> {
  return json<SetupStatus>(await fetch('/api/setup/status'))
}

// Validate a projects-root path as the user types. Like wakeAgent, this returns a
// typed {ok,error} body even on a non-2xx response — so a rejected path surfaces
// {ok:false,error} inline rather than throwing and breaking the debounce.
export async function checkSetupRoot(path: string): Promise<RootCheck> {
  const res = await fetch('/api/setup/check-root', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  })
  const data = (await res.json().catch(() => ({}))) as RootCheck
  if (typeof data.ok !== 'boolean') throw new Error(`${res.status} ${res.statusText}`)
  return data
}

// Commit the first-run configuration. On a 400 the hub returns a typed {error}
// body — json() throws it as `${status}: ${body}` so the wizard can parse and show it.
export async function submitSetup(
  body: SetupSubmit,
): Promise<{ ok: boolean; meshName: string; projectsRoot: string }> {
  const res = await fetch('/api/setup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return json<{ ok: boolean; meshName: string; projectsRoot: string }>(res)
}

// ───────────────────────── Auth (Phase 3 / M3) ─────────────────

// Does the hub require login, and is this browser's session cookie authenticated?
// Drives the dashboard's auth gate. Same-origin so the session cookie rides along
// automatically. The App fails OPEN on a network error (treats auth as not
// required) so a transient blip can't lock the user out.
export async function getAuthStatus(): Promise<AuthStatus> {
  return json<AuthStatus>(await fetch('/api/auth/status'))
}

// Exchange the admin password for a session cookie (set HttpOnly by the hub).
// On 401/429/400 the hub returns a typed {error} body — json() throws it as
// `${status}: ${body}` so the Login page can parse and show the message inline.
export async function login(password: string): Promise<{ ok: boolean }> {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  return json<{ ok: boolean }>(res)
}

// Clear the session cookie. Harmless when auth is off.
export async function logout(): Promise<void> {
  await fetch('/api/auth/logout', { method: 'POST' })
}

export function sessionWsUrl(sessionId: string, cols: number, rows: number): string {
  return wsUrl('/ws/session', { session: sessionId, cols: String(cols), rows: String(rows) })
}

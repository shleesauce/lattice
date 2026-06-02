// Mirrors the locked hub Agent JSON contract exactly.
export interface Agent {
  id: string
  name: string
  hostname: string
  os: string
  arch: string
  agentVersion: string
  online: boolean
  // The hub's co-located agent (filesystem matches project paths exactly).
  local?: boolean
  lastSeen: string // RFC3339
  uptimeSec: number
  diskTotal: number
  diskFree: number
  memTotal: number
  diskUsedPct: number
  memUsedPct: number
  loadAvg1: number
  cpuCount: number
  macs?: string[]
  capabilities?: {
    claudeInstalled: boolean
    claudeVersion?: string
    nodeInstalled: boolean
    nodeVersion?: string
    codeServerInstalled: boolean
    codeServerVersion?: string
    wslAvailable?: boolean
  }
}

// Unified fleet device (mirrors hub Device — /api/devices). The superset of
// every known machine: lattice agents merged with Tailscale peers + SSH-config
// hosts, deduped per physical machine. Agent-backed devices (hasAgent) carry
// live telemetry and can run sessions; the rest are reachability-only (phones,
// machines without the agent) shown for presence + SSH.
export interface Device {
  id: string
  name: string
  host: string
  os: string // darwin | windows | android | ios | linux
  kind: string // monitor | server | smartphone
  status: string // online | reachable | exited(offline)
  online: boolean // host reachable (agent live OR tailscale/ssh)
  // agentLive is true only when the lattice agent itself is checked in with a
  // fresh heartbeat. A box whose agent died but whose host still answers
  // Tailscale is online=true, agentLive=false ⇒ render "reachable", not a
  // false-green node. Absent (older hub) ⇒ treat as online for back-compat.
  agentLive?: boolean
  local: boolean
  sources: string[] // agent | tailscale | ssh
  hasAgent: boolean
  agentId?: string
  arch?: string
  uptimeSec?: number
  memTotal?: number
  memUsedPct?: number
  diskUsedPct?: number
  loadAvg1?: number
  cpuCount?: number
  lastSeen?: string
  macs?: string[]
  capabilities?: Agent['capabilities']
  tailscaleIP?: string
  sshAlias?: string
  sshUser?: string
  sshHost?: string
}

// File browser (mirrors hub FileListResultPayload / FileEntry).
export interface FileEntry {
  name: string
  path: string
  isDir: boolean
  size: number
  modTime: string // RFC3339
}

export interface FileListResult {
  reqId: string
  path: string
  parent: string
  entries: FileEntry[]
  error?: string
}

export interface WakeResult {
  ok: boolean
  error?: string
}

export interface Health {
  ok: boolean
  version: string
  agents: number
}

// Enrollment payload (mirrors hub /api/enroll).
export interface Enroll {
  hubUrl: string
  token: string
  unix: string
  windows: string
}

// Dashboard-WS events (hub -> browser).
export type DashboardEvent =
  | { type: 'fleet'; agents: Agent[] }
  | { type: 'output'; agentId: string; cmdId: string; stream: 'stdout' | 'stderr'; data: string }
  | { type: 'exit'; agentId: string; cmdId: string; exitCode: number; error: string }

export interface OutputLine {
  stream: 'stdout' | 'stderr'
  data: string
}

export interface CommandRun {
  cmdId: string
  agentId: string
  command: string
  lines: OutputLine[]
  finished: boolean
  exitCode: number | null
  error: string
  startedAt: number
}

// ───────────────────────────── Workspace (Phase 3) ─────────────────────────────

export interface Project {
  name: string
  path: string
}

export type SessionKind = 'terminal' | 'claude' | 'editor'
export type SessionStatus = 'starting' | 'live' | 'detached' | 'orphaned' | 'exited'
export type SessionScope = 'project' | 'device'

// Mirrors the hub `sessions` row (D18). Device-scoped sessions have an empty
// projectPath and run in the pinned machine's home dir.
export interface Session {
  id: string
  projectPath: string
  scope: SessionScope
  kind: SessionKind
  agentId: string
  claudeSessionId?: string
  title?: string
  status: SessionStatus
  pinned: boolean
  archived?: boolean
  deletedAt?: string // RFC3339 when trashed; absent/empty otherwise
  createdAt: string // RFC3339
  lastActiveAt: string // RFC3339
}

// Placement scoring breakdown (D19). `reasons` is intentionally open — the hub
// owns the weight keys and we render them generically.
export interface PlacementReasons {
  [factor: string]: number | string | boolean
}

export interface PlacementCandidate {
  agentId: string
  score: number
  eligible: boolean
  reasons: PlacementReasons
  excluded?: string
}

export interface PlacementResult {
  chosen: string | null
  candidates: PlacementCandidate[]
}

export interface CreateSessionRequest {
  kind: SessionKind
  scope?: SessionScope
  projectPath?: string
  title?: string
  userAgentId?: string
  pinAgentId?: string
}

export interface SessionWithPlacement {
  session: Session
  placement: PlacementResult
}

export interface PlacementRequest {
  kind: SessionKind
  projectPath: string
  userAgentId?: string
  pinAgentId?: string
}

// ───────── Begin-new-project onboarding (POST /api/projects) ─────────
export interface CreateProjectEnvVar {
  key: string
  value: string
}

export interface CreateProjectRequest {
  officialName: string
  folderName: string
  description: string
  stack?: string
  port?: number
  connectors?: string[]
  agents?: string[]
  relatedProjects?: string[]
  envVars?: CreateProjectEnvVar[]
  register?: boolean
  launchClaude?: boolean
}

export interface CreateProjectResult {
  project: { name: string; path: string }
  session: Session | null
  registered: boolean
  warnings: string[]
}

export interface AuditEntry {
  id: string
  sessionId: string
  agentId: string
  eventType: string
  toolName?: string
  detail?: string
  at: string // RFC3339
}

export interface Settings {
  globalApproval?: boolean
  perMachineApproval?: Record<string, boolean>
  // primaryAgent (D32) is the default coding machine — the device picker for a
  // new project session pre-selects it (when eligible) so the common case is one click.
  primaryAgent?: string
}

// ───────── /ws/session wire frames (hub → browser) ─────────
// Since D35 a claude session is an interactive PTY, so it speaks the same
// terminal frames as a terminal session (replay/output/exit).
export type SessionInbound =
  | { type: 'replay'; kind: 'terminal'; data: string } // base64 scrollback
  | { type: 'output'; data: string } // base64 terminal frame
  | { type: 'exit' }

// ───────── /ws/session wire frames (browser → hub) ─────────
export type SessionOutbound =
  | { type: 'input'; data: string } // base64 keystrokes
  | { type: 'resize'; cols: number; rows: number }

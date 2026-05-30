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
}

// ───────── /ws/session wire frames (hub → browser) ─────────
export type SessionInbound =
  | { type: 'replay'; kind: 'terminal'; data: string } // base64 scrollback
  | { type: 'replay'; kind: 'claude'; events: ClaudeRaw[] }
  | { type: 'output'; data: string } // base64 terminal frame
  | { type: 'claude_event'; subtype?: string; raw: ClaudeRaw }
  | { type: 'exit' }

// ───────── /ws/session wire frames (browser → hub) ─────────
export type SessionOutbound =
  | { type: 'input'; data: string } // base64 keystrokes
  | { type: 'resize'; cols: number; rows: number }
  | { type: 'claude_input'; text: string }
  | { type: 'claude_permission'; toolUseId: string; allow: boolean }

// ───────── Claude Code stream-json events (rendered defensively) ─────────
export interface ClaudeContentBlock {
  type: string
  text?: string
  // tool_use
  id?: string
  name?: string
  input?: unknown
  // tool_result
  tool_use_id?: string
  content?: unknown
  is_error?: boolean
}

export interface ClaudeMessage {
  role?: string
  model?: string
  content?: ClaudeContentBlock[] | string
  usage?: ClaudeUsage
  stop_reason?: string
}

export interface ClaudeUsage {
  input_tokens?: number
  output_tokens?: number
  cache_creation_input_tokens?: number
  cache_read_input_tokens?: number
}

export interface ClaudeStreamDelta {
  type?: string
  text?: string
  partial_json?: string
}

export interface ClaudeStreamEvent {
  type?: string
  delta?: ClaudeStreamDelta
  content_block?: ClaudeContentBlock
  index?: number
}

// Top-level stream-json event. Fields are optional because we branch on `type`.
export interface ClaudeRaw {
  type: string
  subtype?: string
  session_id?: string
  model?: string
  message?: ClaudeMessage
  event?: ClaudeStreamEvent
  usage?: ClaudeUsage
  total_cost_usd?: number
  duration_ms?: number
  num_turns?: number
  is_error?: boolean
  result?: string
  // permission requests surfaced in approval mode
  tool_use_id?: string
  tool_name?: string
  [extra: string]: unknown
}

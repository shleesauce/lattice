// These interfaces hand-mirror the Go wire structs (internal/proto + internal/hub).
// There's no compiler linking them, so a drift guard — internal/hub/wirecontract_test.go,
// run by `go test ./...` in CI — fails the build if the field names here and on the
// Go side diverge. When it flags a mismatch, update BOTH sides.

// Mirrors proto.Capabilities — what an agent can run (drives placement + fleet UI).
export interface Capabilities {
  claudeInstalled: boolean
  claudeVersion?: string
  // Installed AND able to sign in here (F14). False on a background-service agent
  // (pm2/nohup, e.g. the hub host) that has the binary but no GUI login keychain
  // for claude's OAuth — placing a claude session there yields a dead blank tab.
  claudeAuthable?: boolean
  nodeInstalled: boolean
  nodeVersion?: string
  codeServerInstalled?: boolean
  codeServerVersion?: string
  wslAvailable?: boolean
  // Mesh-sync tooling, reported by the agent for the Integrations panel (Phase 4).
  syncthingInstalled?: boolean
  syncthingRunning?: boolean
}

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
  lanIPs?: string[]
  capabilities?: Capabilities
}

// canHostClaude reports whether an agent can actually RUN a Claude session — the
// binary is installed AND claude can sign in there (F14). Use this everywhere a
// claude session could be placed/switched, never a bare claudeInstalled check, so
// a non-authable box (the hub host under pm2) is never offered and never yields a
// blank tab. Callers that care about reachability still AND their own `online`
// check. The optional chaining keeps an older agent that predates the field
// (claudeAuthable === undefined) excluded — safe, since such a box can't be proven
// authable.
export function canHostClaude(agent?: { capabilities?: Capabilities } | null): boolean {
  return !!(agent?.capabilities?.claudeInstalled && agent?.capabilities?.claudeAuthable)
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
  lanIPs?: string[]
  capabilities?: Capabilities
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
  relay?: string // agent id the hub routed the magic packet through
  subnet?: string // the matched subnet (when routed on-subnet)
  onSubnet?: boolean // true when the relay shares the target's broadcast domain
  action?: string // power: the action that was issued (sleep | shutdown)
}

export interface Health {
  ok: boolean
  version: string
  agents: number
}

// Dashboard-WS events (hub -> browser).
export type DashboardEvent =
  | { type: 'fleet'; agents: Agent[] }
  | { type: 'sessions'; sessions: Session[] }

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
  notifyOnIdle?: boolean // claude: ping my phone when it waits/finishes (fire-and-forget)
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
  permissionMode?: string
  notifyOnIdle?: boolean
}

export interface SessionWithPlacement {
  session: Session
  placement: PlacementResult
}

export interface ReleaseInfo {
  version: string // tag, e.g. "v0.1.5"
  name: string
  body: string // markdown release notes
  publishedAt: string
  prerelease: boolean
  url: string
  current: boolean // == the running build
  newer: boolean // strictly newer than the running build
}

export interface ReleasesResponse {
  current: string
  latest: string
  updateAvailable: boolean
  releases: ReleaseInfo[]
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

// ───────────────────────── Enrollment tokens (Phase 4 / M3) ─────────────────
// A shared join token minted to enroll a new machine into the mesh. The hub
// returns ready-to-paste install one-liners (unix/windows) carrying the token.
export interface EnrollToken {
  token: string
  label: string
  createdAt: string // RFC3339
  revokedAt?: string // RFC3339; empty/absent when still active
  lastUsedAt?: string // RFC3339; empty/absent until first use
  agentId?: string // the agent that enrolled with this token, once one does
}

export interface CreateEnrollTokenResult {
  token: string
  label: string
  unix: string // ready-to-paste macOS/Linux install one-liner
  windows: string // ready-to-paste Windows install one-liner
  tailscaleUnix: string // install Tailscale + tailscale up (macOS/Linux)
  tailscaleWindows: string // install Tailscale + tailscale up (Windows)
}

export interface Settings {
  // primaryAgent (D32) is the default coding machine — the device picker for a
  // new project session pre-selects it (when eligible) so the common case is one click.
  primaryAgent?: string
}

// ───────────────────────── First-run setup (Phase 2 / M3) ─────────────────
// The hub reports whether it has been configured yet. When needsSetup, the
// dashboard is gated behind the FirstRunWizard until POST /api/setup succeeds.
export interface SetupStatus {
  needsSetup: boolean
  meshName?: string
  projectsRoot?: string
  hostname?: string
  suggestedRoot?: string
}

// Live validation of the projects-root path (POST /api/setup/check-root).
// Returns a typed body even on a bad path: ok:false carries the error to show
// inline, ok:true resolves the absolute path and notes whether it must be made.
export interface RootCheck {
  ok: boolean
  resolved?: string
  exists?: boolean
  willCreate?: boolean
  error?: string
}

export interface SetupSubmit {
  adminPassword: string
  meshName: string
  projectsRoot: string
}

// ───────────────────────── Auth (Phase 3 / M3) ─────────────────
// The hub reports whether login is required and whether this browser's session
// cookie is currently authenticated. When authRequired && !authenticated the
// dashboard is gated behind the Login screen until POST /api/auth/login succeeds.
export interface AuthStatus {
  authRequired: boolean
  authenticated: boolean
}

// ───────────────────────── Session transcript (F16 / fixes F15) ─────────────────
// The saved conversation read from Claude Code's on-disk .jsonl, shown for a
// claude session that is no longer a live PTY (exited/archived/trashed/orphaned).
// One assistant message fans out into several blocks (thinking / text / tool_use),
// each independently collapsible — so a long tool run never buries the prose.
export type TranscriptKind = 'text' | 'thinking' | 'tool_use' | 'tool_result' | 'image'

export interface TranscriptBlock {
  seq: number
  role: 'user' | 'assistant'
  kind: TranscriptKind
  text?: string
  toolName?: string
  toolInput?: unknown // raw tool_use input object
  toolUseId?: string // links tool_use ⇄ tool_result
  isError?: boolean
  truncated?: boolean
  sidechain?: boolean // sub-agent (Task) turn
  timestamp?: string
}

export interface TranscriptMeta {
  model?: string
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheCreationTokens: number
  messageCount: number
  firstAt?: string
  lastAt?: string
}

export interface Transcript {
  sessionId: string
  found: boolean
  reason?: string
  path?: string
  meta: TranscriptMeta
  blocks: TranscriptBlock[]
}

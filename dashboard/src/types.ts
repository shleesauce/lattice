// Mirrors the locked hub Agent JSON contract exactly.
export interface Agent {
  id: string
  name: string
  hostname: string
  os: string
  arch: string
  agentVersion: string
  online: boolean
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

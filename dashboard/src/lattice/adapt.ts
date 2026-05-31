/* Adapt the hub's real Agent + Session data into the mesh control-room shape
   (the "Machine" the FleetMap canvas and side panel render). Pure derivation —
   no mock data. */
import type { Agent, Session } from '../types'
import { humanUptime } from '../format'

export interface MachineSession {
  id: string
  name: string
  status: string
  dur: string
}

export interface Machine {
  id: string
  label: string
  hostname: string
  kind: string
  status: string // live | idle | detached | exited(offline)
  online: boolean
  offline: boolean
  cores: number
  cpu: number // 0–100, derived from loadAvg1 / cpuCount
  memUsed: number // GB
  memTotal: number // GB
  net: string
  locality: number // 0 this machine · 1 LAN · 2 remote
  locLabel: string
  uptime: string
  mac?: string
  hasClaude: boolean
  hasEditor: boolean
  sessions: MachineSession[]
  x: number
  y: number
}

const GiB = 1024 * 1024 * 1024

function kindFor(a: Agent): string {
  const n = `${a.name} ${a.hostname}`.toLowerCase()
  if (/iphone|ipad|phone|android|pixel/.test(n)) return 'smartphone'
  if (/mbp|macbook|book|air|laptop/.test(n)) return 'monitor'
  return 'server'
}

// Stable, pleasing scatter: a ring around centre, the local/hub node pulled in.
function layout(i: number, n: number): { x: number; y: number } {
  if (n === 1) return { x: 0.42, y: 0.46 }
  const golden = 2.399963229728653 // golden angle (rad) — even, non-clumping spread
  const a = i * golden + 0.6
  const r = 0.26 + 0.12 * ((i % 3) / 2) // vary radius so edges aren't all equal
  return { x: 0.46 + r * Math.cos(a), y: 0.48 + r * 0.82 * Math.sin(a) }
}

function durSince(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  return humanUptime(Math.max(0, (Date.now() - t) / 1000))
}

export function agentsToMachines(agents: Agent[], sessions: Session[]): Machine[] {
  // Hub/local first, then online, then offline; stable by name within a group.
  const ordered = [...agents].sort((a, b) => {
    if (!!a.local !== !!b.local) return a.local ? -1 : 1
    if (a.online !== b.online) return a.online ? -1 : 1
    return (a.name || a.hostname).localeCompare(b.name || b.hostname)
  })

  return ordered.map((a, i) => {
    const mine = sessions.filter((s) => s.agentId === a.id && s.status !== 'exited')
    const live = mine.filter((s) => s.status === 'live')
    const detached = mine.filter((s) => s.status === 'detached' || s.status === 'orphaned')
    const status = !a.online
      ? 'exited'
      : live.length > 0
        ? 'live'
        : detached.length > 0
          ? 'detached'
          : 'idle'
    const pos = layout(i, ordered.length)
    return {
      id: a.id,
      label: a.name || a.hostname,
      hostname: a.hostname,
      kind: kindFor(a),
      status,
      online: a.online,
      offline: !a.online,
      cores: a.cpuCount || 0,
      cpu: a.cpuCount ? Math.min(100, Math.max(0, Math.round((a.loadAvg1 / a.cpuCount) * 100))) : 0,
      memUsed: (a.memTotal * (a.memUsedPct / 100)) / GiB,
      memTotal: a.memTotal / GiB,
      net: '—',
      locality: a.local ? 0 : 1,
      locLabel: a.local ? 'this mac' : a.online ? 'lan' : 'offline',
      uptime: a.online ? humanUptime(a.uptimeSec) : '—',
      mac: a.macs?.[0],
      hasClaude: a.capabilities?.claudeInstalled ?? false,
      hasEditor: a.capabilities?.codeServerInstalled ?? false,
      sessions: mine.map((s) => ({
        id: s.id,
        name: s.title || s.kind,
        status: s.status,
        dur: durSince(s.createdAt),
      })),
      x: pos.x,
      y: pos.y,
    }
  })
}

export const STATUS_LABEL: Record<string, string> = {
  live: 'live', starting: 'waking', detached: 'detached',
  idle: 'idle', orphaned: 'orphaned', exited: 'offline',
}

/* Local placement-fit heuristic for the side panel's bar (the New-session
   dialog uses the hub's real /api/placement scoring). free RAM + headroom + locality. */
export function fitScore(m: Machine): number {
  if (m.offline) return 0
  const freeRAM = m.memTotal - m.memUsed
  const ramScore = Math.min(freeRAM / 64, 1) * 40
  const loadScore = (1 - Math.min(m.cpu / 100, 1)) * 35
  const locScore = (m.locality === 0 ? 1 : m.locality === 1 ? 0.7 : 0.3) * 25
  return Math.round(ramScore + loadScore + locScore)
}

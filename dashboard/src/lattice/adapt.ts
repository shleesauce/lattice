/* Adapt the hub's unified Device list (agents + Tailscale + SSH, /api/devices)
   plus live Sessions into the mesh control-room shape (the "Machine" the
   FleetMap canvas + side panel render). Pure derivation — no mock data. */
import type { Device, Session } from '../types'
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
  status: string // live | idle | detached | reachable | starting | exited(offline)
  online: boolean
  offline: boolean
  hasAgent: boolean
  agentId?: string
  cores: number
  cpu: number // 0–100, derived from loadAvg1 / cpuCount
  memUsed: number // GB
  memTotal: number // GB
  net: string
  locality: number // 0 this machine · 1 LAN · 2 remote
  locLabel: string
  uptime: string
  mac?: string
  os: string
  sources: string[] // agent | tailscale | ssh
  sshAlias?: string
  sshUser?: string
  sshHost?: string
  tailscaleIP?: string
  hasClaude: boolean
  hasEditor: boolean
  sessions: MachineSession[]
  x: number
  y: number
}

const GiB = 1024 * 1024 * 1024

// Stable, pleasing scatter: golden-angle ring around centre, local node pulled in.
function layout(i: number, n: number): { x: number; y: number } {
  if (n <= 1) return { x: 0.46, y: 0.48 }
  const golden = 2.399963229728653
  const a = i * golden + 0.6
  const r = 0.24 + 0.13 * ((i % 3) / 2)
  return { x: 0.47 + r * Math.cos(a), y: 0.49 + r * 0.82 * Math.sin(a) }
}

function durSince(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  return humanUptime(Math.max(0, (Date.now() - t) / 1000))
}

export function devicesToMachines(devices: Device[], sessions: Session[]): Machine[] {
  // Order: local/hub first, then online, then by name — so layout is stable.
  const ordered = [...devices].sort((a, b) => {
    if (a.local !== b.local) return a.local ? -1 : 1
    if (a.online !== b.online) return a.online ? -1 : 1
    if (a.hasAgent !== b.hasAgent) return a.hasAgent ? -1 : 1
    return a.name.localeCompare(b.name)
  })

  return ordered.map((d, i) => {
    // An agent box only counts as a runnable node when its agent is actually
    // checked in (agentLive). If the agent died but the host still answers
    // Tailscale/SSH (online=true, agentLive=false) it's reachable-only — never a
    // false-green idle node. agentLive may be absent on an older hub ⇒ fall back
    // to hasAgent so we don't regress.
    const agentLive = d.hasAgent && (d.agentLive ?? true)
    // Sessions only attach to a LIVE agent (matched by agentId).
    const mine = agentLive && d.agentId ? sessions.filter((s) => s.agentId === d.agentId && s.status !== 'exited') : []
    const live = mine.filter((s) => s.status === 'live')
    const detached = mine.filter((s) => s.status === 'detached' || s.status === 'orphaned')

    let status: string
    if (!d.online) status = 'exited'
    else if (live.length > 0) status = 'live'
    else if (agentLive) status = detached.length > 0 ? 'detached' : 'idle'
    else status = 'reachable' // reachable via tailscale/ssh; no live agent

    const memTotal = (d.memTotal ?? 0) / GiB
    const memUsed = (memTotal * (d.memUsedPct ?? 0)) / 100
    const pos = layout(i, ordered.length)

    return {
      id: d.id,
      label: d.name,
      hostname: d.host,
      kind: d.kind,
      status,
      online: d.online,
      offline: !d.online,
      hasAgent: d.hasAgent,
      agentId: d.agentId,
      cores: d.cpuCount ?? 0,
      cpu: d.cpuCount ? Math.min(100, Math.max(0, Math.round(((d.loadAvg1 ?? 0) / d.cpuCount) * 100))) : 0,
      memUsed,
      memTotal,
      net: '—',
      locality: d.local ? 0 : 1,
      locLabel: d.local ? 'this mac' : d.online ? (d.sources.includes('tailscale') ? 'tailnet' : 'lan') : 'offline',
      uptime: d.online && d.uptimeSec ? humanUptime(d.uptimeSec) : '—',
      mac: d.macs?.[0],
      os: d.os,
      sources: d.sources,
      sshAlias: d.sshAlias,
      sshUser: d.sshUser,
      sshHost: d.sshHost,
      tailscaleIP: d.tailscaleIP,
      hasClaude: d.capabilities?.claudeInstalled ?? false,
      hasEditor: d.capabilities?.codeServerInstalled ?? false,
      sessions: mine.map((s) => ({ id: s.id, name: s.title || s.kind, status: s.status, dur: durSince(s.createdAt) })),
      x: pos.x,
      y: pos.y,
    }
  })
}

export const STATUS_LABEL: Record<string, string> = {
  live: 'live', starting: 'waking', detached: 'detached',
  idle: 'idle', orphaned: 'orphaned', reachable: 'reachable', exited: 'offline',
}

/* Local placement-fit heuristic for the side panel's bar (the New-session
   dialog uses the hub's real /api/placement scoring). free RAM + headroom + locality. */
export function fitScore(m: Machine): number {
  if (m.offline || !m.hasAgent) return 0
  const freeRAM = m.memTotal - m.memUsed
  const ramScore = Math.min(freeRAM / 64, 1) * 40
  const loadScore = (1 - Math.min(m.cpu / 100, 1)) * 35
  const locScore = (m.locality === 0 ? 1 : m.locality === 1 ? 0.7 : 0.3) * 25
  return Math.round(ramScore + loadScore + locScore)
}

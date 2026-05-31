/* Lattice — fleet + project data, status labels, fit scoring.
   locality: 0 = this machine, 1 = same LAN, 2 = remote. */

export interface Session {
  name: string
  status: string
  dur: string
  node?: string
}

export interface Machine {
  id: string
  label: string
  kind: string
  status: string
  cores: number
  cpu: number
  memUsed: number
  memTotal: number
  net: string
  locality: number
  locLabel: string
  uptime: string
  sessions: Session[]
  x: number
  y: number
  offline?: boolean
}

export interface ProjectFile {
  n: string
  icon: string
  dir?: boolean
  ind: number
  on?: boolean
  warm?: boolean
}

export interface Project {
  id: string
  name: string
  node: string
  files: ProjectFile[]
  sessions: Session[]
}

export const FLEET: Machine[] = [
  {
    id: 'studio-mbp', label: 'studio-mbp', kind: 'monitor', status: 'live',
    cores: 16, cpu: 38, memUsed: 12.4, memTotal: 64, net: '41.2 GB/s',
    locality: 0, locLabel: 'this mac', uptime: '6d 4h',
    sessions: [{ name: 'build-watcher', status: 'live', dur: '2h 14m' }],
    x: 0.30, y: 0.34,
  },
  {
    id: 'mac-mini', label: 'mac-mini', kind: 'server', status: 'idle',
    cores: 18, cpu: 4, memUsed: 6, memTotal: 32, net: '0.2 GB/s',
    locality: 1, locLabel: 'lan · 0.4ms', uptime: '21d 2h',
    sessions: [], x: 0.62, y: 0.24,
  },
  {
    id: 'macbook-air', label: 'macbook-air', kind: 'monitor', status: 'detached',
    cores: 8, cpu: 12, memUsed: 9.1, memTotal: 16, net: '1.4 GB/s',
    locality: 1, locLabel: 'lan · 1.2ms', uptime: '2d 9h',
    sessions: [{ name: 'notes-sync', status: 'detached', dur: '18m' }],
    x: 0.52, y: 0.66,
  },
  {
    id: 'garage-pc', label: 'garage-pc', kind: 'server', status: 'exited',
    cores: 24, cpu: 0, memUsed: 0, memTotal: 128, net: '—',
    locality: 1, locLabel: 'lan · last 4m ago', uptime: '—',
    sessions: [], x: 0.82, y: 0.58, offline: true,
  },
  {
    id: 'phone', label: 'iphone', kind: 'smartphone', status: 'detached',
    cores: 6, cpu: 2, memUsed: 1.2, memTotal: 8, net: '0.1 GB/s',
    locality: 2, locLabel: 'remote · 38ms', uptime: '—',
    sessions: [], x: 0.20, y: 0.74,
  },
]

export const STATUS_LABEL: Record<string, string> = {
  live: 'live', starting: 'waking', detached: 'detached',
  idle: 'idle', orphaned: 'orphaned', exited: 'offline',
}

/* fit score 0–100: free RAM (40) + headroom/low-load (35) + locality (25).
   offline machines score 0 and are not placeable until woken. */
export function fitScore(m: Machine): number {
  if (m.offline) return 0
  const freeRAM = m.memTotal - m.memUsed
  const ramScore = Math.min(freeRAM / 64, 1) * 40 // 64GB free ≈ full marks
  const loadScore = (1 - Math.min(m.cpu / 100, 1)) * 35 // lower load = better
  const locScore = (m.locality === 0 ? 1 : m.locality === 1 ? 0.7 : 0.3) * 25
  return Math.round(ramScore + loadScore + locScore)
}

/* rank placeable machines best-first; offline pushed to the bottom */
export function rankByFit(fleet: Machine[]): Machine[] {
  return [...fleet].sort((a, b) => {
    if (!!a.offline !== !!b.offline) return a.offline ? 1 : -1
    return fitScore(b) - fitScore(a)
  })
}

export const PROJECTS: Project[] = [
  {
    id: 'lattice-core', name: 'lattice-core', node: 'studio-mbp',
    files: [
      { n: 'src', icon: 'chevron-down', dir: true, ind: 0 },
      { n: 'mesh.ts', icon: 'file-code', ind: 1, on: true, warm: true },
      { n: 'placer.ts', icon: 'file-code', ind: 1 },
      { n: 'fleet.ts', icon: 'file-code', ind: 1 },
      { n: 'components', icon: 'chevron-right', dir: true, ind: 1 },
      { n: 'package.json', icon: 'file-code', ind: 0 },
      { n: 'README.md', icon: 'file-code', ind: 0 },
    ],
    sessions: [
      { name: 'build-watcher', status: 'live', node: 'studio-mbp', dur: '2h 14m' },
      { name: 'mesh-repl', status: 'detached', node: 'mac-mini', dur: '41m' },
    ],
  },
  {
    id: 'site', name: 'site', node: 'mac-mini',
    files: [], sessions: [{ name: 'next-dev', status: 'idle', node: 'mac-mini', dur: '—' }],
  },
]

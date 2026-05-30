import type { SessionStatus } from '../../types'

export function statusDotClass(status: SessionStatus): string {
  switch (status) {
    case 'live':
      return 'bg-emerald-400'
    case 'starting':
      return 'bg-amber-400'
    case 'detached':
      return 'bg-sky-400'
    case 'orphaned':
      return 'bg-orange-500'
    case 'exited':
      return 'bg-zinc-600'
  }
}

export function statusLabel(status: SessionStatus): string {
  return status
}

export function statusPulses(status: SessionStatus): boolean {
  return status === 'live' || status === 'starting'
}

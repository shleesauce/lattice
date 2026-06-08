import type { Session, SessionStatus } from '../../types'

// showsTranscript decides whether a session pane renders its SAVED transcript
// (F16) instead of the live xterm. True only for a claude session that is no
// longer a reattachable live PTY: archived, trashed, exited, or orphaned (its
// agent is gone, so the live socket would just spin "reattaching…" forever — the
// blank-tab bug, F15). A `starting`/`live`/`detached` claude keeps the xterm so
// you can reattach and keep working; terminal/editor sessions have no transcript.
export function showsTranscript(s: Session): boolean {
  if (s.kind !== 'claude') return false
  if (s.archived || s.deletedAt) return true
  return s.status === 'exited' || s.status === 'orphaned'
}

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

export function statusPulses(status: SessionStatus): boolean {
  return status === 'live' || status === 'starting'
}

import { useRef } from 'react'
import { XtermSession, type XtermSessionHandle } from './XtermSession'
import { SessionTelemetryBar } from './SessionTelemetryBar'

interface Props {
  sessionId: string
}

// Shift+Tab byte sequence — cycles Claude's permission mode in the TUI footer.
const SHIFT_TAB = '\x1b[Z'

// Since D35 (June 2026 billing change) the Claude workspace session is an
// interactive `claude` running in a PTY on the hub — not a headless JSON-stream
// chat. It speaks the same replay/output/input frames as a terminal, so it
// renders through the identical xterm path, with a "claude" header label.
export function SessionClaude({ sessionId }: Props) {
  const xtermRef = useRef<XtermSessionHandle>(null)

  const cyclePermissions = () => {
    xtermRef.current?.sendInput(SHIFT_TAB)
  }

  return (
    <div className="relative flex h-full min-h-0 flex-col">
      <XtermSession ref={xtermRef} sessionId={sessionId} liveLabel="claude" />
      <SessionTelemetryBar sessionId={sessionId} />
      <button
        type="button"
        onClick={cyclePermissions}
        title="Cycle Claude's permission mode (Shift+Tab)"
        className="absolute bottom-3 right-3 z-10 flex items-center gap-1.5 rounded-md border border-zinc-700 bg-zinc-900/90 px-2.5 py-1 font-mono text-[10px] text-zinc-400 backdrop-blur-sm transition-colors hover:border-zinc-600 hover:text-zinc-200"
      >
        <ShiftTabIcon />
        Permissions
      </button>
    </div>
  )
}

function ShiftTabIcon() {
  return (
    <svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
      <path d="M3 8h8M7 5l4 3-4 3" strokeLinecap="round" strokeLinejoin="round" />
      <line x1="2" y1="4" x2="2" y2="12" strokeLinecap="round" />
    </svg>
  )
}

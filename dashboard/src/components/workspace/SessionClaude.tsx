import { XtermSession } from './XtermSession'

interface Props {
  sessionId: string
}

// Since D35 (June 2026 billing change) the Claude workspace session is an
// interactive `claude` running in a PTY on the hub — not a headless JSON-stream
// chat. It speaks the same replay/output/input frames as a terminal, so it
// renders through the identical xterm path, with a "claude" header label.
export function SessionClaude({ sessionId }: Props) {
  return <XtermSession sessionId={sessionId} liveLabel="claude" />
}

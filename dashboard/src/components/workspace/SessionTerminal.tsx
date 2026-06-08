import { XtermSession } from './XtermSession'

interface Props {
  sessionId: string
}

// A device/project terminal session, rendered through the shared xterm path.
export function SessionTerminal({ sessionId }: Props) {
  return <XtermSession sessionId={sessionId} />
}

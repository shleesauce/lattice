import { useState } from 'react'

interface Props {
  sessionId: string
}

// Embeds code-server at /editor/{sessionId}/ in a full-size iframe.
// The trailing slash on the src is REQUIRED — code-server redirects without it.
// sandbox is intentionally omitted: code-server needs full scripting + same-origin.
export function SessionEditor({ sessionId }: Props) {
  const [loaded, setLoaded] = useState(false)

  return (
    <div className="relative h-full w-full bg-[#1e1e1e]">
      {!loaded && (
        <div className="absolute inset-0 z-10 flex items-center justify-center bg-[#1e1e1e]">
          <div className="flex items-center gap-2.5 font-mono text-[11px] uppercase tracking-wider text-zinc-500">
            <span className="h-1.5 w-1.5 rounded-full bg-amber-400 animate-breathe" />
            loading editor…
          </div>
        </div>
      )}
      <iframe
        src={`/editor/${sessionId}/`}
        allow="clipboard-read; clipboard-write"
        onLoad={() => setLoaded(true)}
        className="absolute inset-0 h-full w-full border-0"
        title={`editor-${sessionId}`}
      />
    </div>
  )
}

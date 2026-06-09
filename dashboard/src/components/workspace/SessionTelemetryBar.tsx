import { useEffect, useState } from 'react'
import { fetchSessionTelemetry } from '../../api'
import { modelLabel, type SessionTelemetry } from '../../types'

// Rich session telemetry strip (C, v0.1.5). Renders model / context% / $cost for a
// live Claude session — the ai-beacon-style card metadata — derived hub-side from
// the synced transcript (the CC hook stdin can't provide it). Self-contained: polls
// GET /api/sessions/{id}/telemetry and renders nothing until a transcript exists, so
// it never clutters a brand-new or non-Claude session.
export function SessionTelemetryBar({ sessionId }: { sessionId: string }) {
  const [tel, setTel] = useState<SessionTelemetry | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = () => {
      fetchSessionTelemetry(sessionId)
        // Keep the bar when there's telemetry OR a detected PR to surface.
        .then((t) => !cancelled && setTel(t.found || t.prUrl ? t : null))
        .catch(() => {})
    }
    load()
    const iv = setInterval(load, 20_000) // cheap refresh; transcript lags a few s
    return () => {
      cancelled = true
      clearInterval(iv)
    }
  }, [sessionId])

  if (!tel || (!tel.found && !tel.prUrl)) return null

  const ctx = Math.round(tel.contextPct)
  const cost = tel.costUsd >= 0.01 ? `$${tel.costUsd.toFixed(2)}` : '<$0.01'
  const tokens = formatTokens(tel.inputTokens + tel.outputTokens)

  return (
    <div className="absolute left-3 top-3 z-10 flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900/90 px-2.5 py-1 font-mono text-[10px] text-zinc-400 backdrop-blur-sm">
      {tel.found && (
        <>
          {tel.model && <span className="text-zinc-200">{modelLabel(tel.model)}</span>}
          <span className="text-zinc-600">·</span>
          <span title="context window used">
            ctx <span className={ctx >= 80 ? 'text-amber-400' : 'text-zinc-300'}>{ctx}%</span>
          </span>
          <span className="text-zinc-600">·</span>
          <span title="tokens this conversation">{tokens} tok</span>
          <span className="text-zinc-600">·</span>
          <span title="estimated cost (informational — Lattice runs on the Max subscription)">{cost}</span>
        </>
      )}
      {tel.prUrl && (
        <>
          {tel.found && <span className="text-zinc-600">·</span>}
          <a
            href={tel.prUrl}
            target="_blank"
            rel="noreferrer"
            title={tel.prUrl}
            className="text-teal-400 hover:text-teal-300"
          >
            PR ↗
          </a>
        </>
      )}
    </div>
  )
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

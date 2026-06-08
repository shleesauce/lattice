import { HubError } from '../api'

// Extract the {"error":"…"} message from a hub response body. Returns null when
// the body isn't a JSON object carrying a string `error`.
function bodyError(body: string): string | null {
  const idx = body.indexOf('{')
  if (idx === -1) return null
  try {
    const parsed = JSON.parse(body.slice(idx)) as { error?: unknown }
    if (typeof parsed.error === 'string' && parsed.error) return parsed.error
  } catch {
    /* not JSON */
  }
  return null
}

// Turn a thrown API error into a clean, human-readable message. A HubError carries
// the raw response body (usually a typed `{"error":"…"}`); other Errors arrive as
// the legacy `${status}: ${body}` message shape. Prefer the parsed {error}, else
// strip the leading `<status>:` prefix. One shared implementation for every call site.
export function parseHubError(e: unknown, fallback: string): string {
  if (e instanceof HubError) {
    // parsed {error} → trimmed raw body → fallback (|| handles an empty body)
    return bodyError(e.body) || e.body.trim() || fallback
  }
  if (!(e instanceof Error)) return fallback
  const raw = e.message
  const fromBody = bodyError(raw)
  if (fromBody) return fromBody
  const stripped = raw.replace(/^\d+:\s*/, '').trim()
  return stripped || fallback
}

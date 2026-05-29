import type { Agent, FileListResult, Health, WakeResult } from './types'

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json() as Promise<T>
}

export async function fetchFleet(): Promise<Agent[]> {
  const data = await json<{ agents: Agent[] | null }>(await fetch('/api/fleet'))
  return data.agents ?? []
}

export async function fetchHealth(): Promise<Health> {
  return json<Health>(await fetch('/api/health'))
}

export async function execCommand(agentId: string, command: string): Promise<string> {
  const res = await fetch(`/api/agents/${encodeURIComponent(agentId)}/exec`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ command }),
  })
  const data = await json<{ cmdId: string }>(res)
  return data.cmdId
}

export async function fetchFiles(agentId: string, path: string): Promise<FileListResult> {
  const qs = new URLSearchParams({ path })
  const res = await fetch(`/api/agents/${encodeURIComponent(agentId)}/files?${qs}`)
  return json<FileListResult>(res)
}

export function downloadUrl(agentId: string, path: string): string {
  const qs = new URLSearchParams({ path })
  return `/api/agents/${encodeURIComponent(agentId)}/download?${qs}`
}

export async function wakeAgent(senderId: string, mac: string): Promise<WakeResult> {
  const res = await fetch(`/api/agents/${encodeURIComponent(senderId)}/wake`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mac }),
  })
  // Wake always returns a typed {ok,error} body, even on 4xx/5xx.
  const data = (await res.json().catch(() => ({}))) as WakeResult
  if (typeof data.ok !== 'boolean') throw new Error(`${res.status} ${res.statusText}`)
  return data
}

// In dev (vite :5173) the WS still targets location.host because vite proxies /ws to the hub.
export function dashboardWsUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/ws/dashboard`
}

export function terminalWsUrl(agentId: string, cols: number, rows: number): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const qs = new URLSearchParams({ agent: agentId, cols: String(cols), rows: String(rows) })
  return `${proto}://${location.host}/ws/terminal?${qs}`
}

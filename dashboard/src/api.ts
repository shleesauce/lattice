import type { Agent, Health } from './types'

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

// In dev (vite :5173) the WS still targets location.host because vite proxies /ws to the hub.
export function dashboardWsUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/ws/dashboard`
}

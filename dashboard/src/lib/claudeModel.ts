import type { ClaudeContentBlock, ClaudeRaw, ClaudeUsage } from '../types'

// A normalized, render-ready view of a Claude Code stream-json conversation.
// We fold the raw event stream into ordered chat items so the UI can stay dumb.

export interface TextItem {
  kind: 'text'
  id: string
  role: 'assistant' | 'user'
  text: string
  streaming: boolean
}

export interface ToolCallItem {
  kind: 'tool_call'
  id: string // tool_use id
  name: string
  input: unknown
  result?: string
  resultIsError?: boolean
  done: boolean
}

export interface SystemItem {
  kind: 'system'
  id: string
  model?: string
  text: string
}

export interface PermissionItem {
  kind: 'permission'
  id: string // toolUseId
  toolName?: string
  resolved?: boolean
  allowed?: boolean
}

export type ChatItem = TextItem | ToolCallItem | SystemItem | PermissionItem

export interface UsageHud {
  inputTokens: number
  outputTokens: number
  cacheRead: number
  cacheCreation: number
  costUsd: number
  numTurns: number
}

export interface ClaudeState {
  items: ChatItem[]
  usage: UsageHud
  busy: boolean // a turn is in flight
  model?: string
}

export const emptyClaudeState: ClaudeState = {
  items: [],
  usage: { inputTokens: 0, outputTokens: 0, cacheRead: 0, cacheCreation: 0, costUsd: 0, numTurns: 0 },
  busy: false,
}

let seq = 0
const nextId = () => `i${++seq}`

function addUsage(hud: UsageHud, u?: ClaudeUsage): UsageHud {
  if (!u) return hud
  return {
    inputTokens: hud.inputTokens + (u.input_tokens ?? 0),
    outputTokens: hud.outputTokens + (u.output_tokens ?? 0),
    cacheRead: hud.cacheRead + (u.cache_read_input_tokens ?? 0),
    cacheCreation: hud.cacheCreation + (u.cache_creation_input_tokens ?? 0),
    costUsd: hud.costUsd,
    numTurns: hud.numTurns,
  }
}

function blocks(content: ClaudeContentBlock[] | string | undefined): ClaudeContentBlock[] {
  if (!content) return []
  if (typeof content === 'string') return [{ type: 'text', text: content }]
  return content
}

function toolResultText(content: unknown): string {
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((c) => {
        if (typeof c === 'string') return c
        if (c && typeof c === 'object' && 'text' in c) return String((c as { text: unknown }).text ?? '')
        return ''
      })
      .filter(Boolean)
      .join('\n')
  }
  if (content == null) return ''
  try {
    return JSON.stringify(content, null, 2)
  } catch {
    return String(content)
  }
}

// Apply one raw stream-json event to the state. Pure-ish (mutates a shallow copy).
export function applyClaudeEvent(state: ClaudeState, raw: ClaudeRaw): ClaudeState {
  if (!raw || typeof raw.type !== 'string') return state
  const items = state.items
  let usage = state.usage
  let busy = state.busy
  let model = state.model

  switch (raw.type) {
    case 'system': {
      if (raw.subtype === 'init') {
        model = raw.model ?? model
        return {
          ...state,
          model,
          items: [
            ...items,
            {
              kind: 'system',
              id: nextId(),
              model: raw.model,
              text: 'session initialized',
            },
          ],
        }
      }
      return state
    }

    case 'assistant': {
      const next = [...items]
      busy = true
      for (const b of blocks(raw.message?.content)) {
        if (b.type === 'text' && b.text) {
          next.push({ kind: 'text', id: nextId(), role: 'assistant', text: b.text, streaming: false })
        } else if (b.type === 'tool_use') {
          next.push({
            kind: 'tool_call',
            id: b.id ?? nextId(),
            name: b.name ?? 'tool',
            input: b.input,
            done: false,
          })
        }
      }
      usage = addUsage(usage, raw.message?.usage)
      return { ...state, items: next, usage, busy, model }
    }

    case 'user': {
      // Carries tool_result blocks — attach to their originating tool_call.
      const next = [...items]
      for (const b of blocks(raw.message?.content)) {
        if (b.type === 'tool_result') {
          const targetId = b.tool_use_id
          const idx = next.findIndex((it) => it.kind === 'tool_call' && it.id === targetId)
          if (idx !== -1) {
            const call = next[idx] as ToolCallItem
            next[idx] = {
              ...call,
              result: toolResultText(b.content),
              resultIsError: !!b.is_error,
              done: true,
            }
          }
        } else if (b.type === 'text' && b.text) {
          // A replayed user turn (--replay-user-messages).
          next.push({ kind: 'text', id: nextId(), role: 'user', text: b.text, streaming: false })
        }
      }
      return { ...state, items: next }
    }

    case 'stream_event': {
      // Partial deltas — stream assistant text token-by-token.
      const delta = raw.event?.delta
      const dtext = delta?.type === 'text_delta' ? delta.text : delta?.text
      if (!dtext) return state
      const next = [...items]
      const last = next[next.length - 1]
      if (last && last.kind === 'text' && last.role === 'assistant' && last.streaming) {
        next[next.length - 1] = { ...last, text: last.text + dtext }
      } else {
        next.push({ kind: 'text', id: nextId(), role: 'assistant', text: dtext, streaming: true })
      }
      return { ...state, items: next, busy: true }
    }

    case 'result': {
      // Finalize: stop any streaming flag, update HUD with cost.
      const next = items.map((it) =>
        it.kind === 'text' && it.streaming ? { ...it, streaming: false } : it,
      )
      usage = {
        ...usage,
        costUsd: typeof raw.total_cost_usd === 'number' ? raw.total_cost_usd : usage.costUsd,
        numTurns: typeof raw.num_turns === 'number' ? raw.num_turns : usage.numTurns + 1,
      }
      usage = addUsage(usage, raw.usage)
      return { ...state, items: next, usage, busy: false, model }
    }

    default: {
      // control_request / permission prompts surfaced by the hub.
      if (raw.subtype === 'can_use_tool' || raw.type === 'permission_request') {
        const id = raw.tool_use_id ?? nextId()
        if (items.some((it) => it.kind === 'permission' && it.id === id)) return state
        return {
          ...state,
          items: [
            ...items,
            { kind: 'permission', id, toolName: raw.tool_name, resolved: false },
          ],
        }
      }
      return state
    }
  }
}

export function applyClaudeEvents(state: ClaudeState, events: ClaudeRaw[]): ClaudeState {
  return events.reduce(applyClaudeEvent, state)
}

// Optimistic local echo of a user turn before the hub replays it.
export function appendUserTurn(state: ClaudeState, text: string): ClaudeState {
  return {
    ...state,
    busy: true,
    items: [...state.items, { kind: 'text', id: nextId(), role: 'user', text, streaming: false }],
  }
}

export function resolvePermission(state: ClaudeState, id: string, allowed: boolean): ClaudeState {
  return {
    ...state,
    items: state.items.map((it) =>
      it.kind === 'permission' && it.id === id ? { ...it, resolved: true, allowed } : it,
    ),
  }
}

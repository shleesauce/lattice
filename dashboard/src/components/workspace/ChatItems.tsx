import { useMemo, useState } from 'react'
import { renderMarkdown } from '../../lib/markdown'
import type { ChatItem, PermissionItem, SystemItem, TextItem, ToolCallItem } from '../../lib/claudeModel'

export function ChatItemView({
  item,
  onPermission,
}: {
  item: ChatItem
  onPermission: (id: string, allow: boolean) => void
}) {
  switch (item.kind) {
    case 'text':
      return <TextBubble item={item} />
    case 'tool_call':
      return <ToolCard item={item} />
    case 'system':
      return <SystemBanner item={item} />
    case 'permission':
      return <PermissionPrompt item={item} onPermission={onPermission} />
  }
}

function TextBubble({ item }: { item: TextItem }) {
  if (item.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-2xl rounded-br-sm border border-emerald-500/25 bg-emerald-500/10 px-4 py-2.5">
          <p className="whitespace-pre-wrap font-display text-[13.5px] leading-relaxed text-emerald-50">
            {item.text}
          </p>
        </div>
      </div>
    )
  }
  const html = useMemo(() => renderMarkdown(item.text), [item.text])
  return (
    <div className="flex gap-3">
      <AssistantMark />
      <div className="min-w-0 flex-1 pt-0.5">
        <div className="md-body" dangerouslySetInnerHTML={{ __html: html }} />
        {item.streaming && <span className="caret-blink" />}
      </div>
    </div>
  )
}

function ToolCard({ item }: { item: ToolCallItem }) {
  const [open, setOpen] = useState(false)
  const inputStr = useMemo(() => stringify(item.input), [item.input])
  const oneLiner = oneLine(item.input)
  return (
    <div className="ml-9 overflow-hidden rounded-lg border border-zinc-800 bg-zinc-900/50">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors hover:bg-zinc-900"
      >
        <WrenchIcon />
        <span className="font-mono text-[12px] font-medium text-zinc-200">{item.name}</span>
        {oneLiner && (
          <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-zinc-500">{oneLiner}</span>
        )}
        <span className={`ml-auto shrink-0 ${dotClass(item)}`} />
        <Chevron open={open} />
      </button>
      {open && (
        <div className="space-y-2 border-t border-zinc-800 bg-zinc-950/60 px-3 py-2.5">
          <Labeled label="input">
            <pre className="term-scroll max-h-56 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-zinc-300">
              {inputStr}
            </pre>
          </Labeled>
          {item.done && (
            <Labeled label={item.resultIsError ? 'error' : 'result'}>
              <pre
                className={`term-scroll max-h-64 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed ${
                  item.resultIsError ? 'text-red-300' : 'text-zinc-400'
                }`}
              >
                {item.result || '∅'}
              </pre>
            </Labeled>
          )}
        </div>
      )}
    </div>
  )
}

function SystemBanner({ item }: { item: SystemItem }) {
  return (
    <div className="flex items-center gap-2 py-1 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-600">
      <span className="h-px flex-1 bg-zinc-800/70" />
      <span>{item.text}</span>
      {item.model && <span className="text-emerald-500/70">· {item.model}</span>}
      <span className="h-px flex-1 bg-zinc-800/70" />
    </div>
  )
}

function PermissionPrompt({
  item,
  onPermission,
}: {
  item: PermissionItem
  onPermission: (id: string, allow: boolean) => void
}) {
  return (
    <div className="ml-9 rounded-lg border border-amber-500/40 bg-amber-500/[0.07] px-3 py-2.5">
      <div className="flex items-center gap-2 font-mono text-[11px] text-amber-200">
        <ShieldIcon />
        permission requested{item.toolName ? `: ${item.toolName}` : ''}
      </div>
      {item.resolved ? (
        <p className="mt-1.5 font-mono text-[11px] text-zinc-500">
          {item.allowed ? 'allowed' : 'denied'}
        </p>
      ) : (
        <div className="mt-2 flex gap-2">
          <button
            type="button"
            onClick={() => onPermission(item.id, true)}
            className="rounded-md bg-emerald-500 px-3 py-1 font-display text-[12px] font-semibold text-emerald-950 transition-colors hover:bg-emerald-400"
          >
            Allow
          </button>
          <button
            type="button"
            onClick={() => onPermission(item.id, false)}
            className="rounded-md border border-zinc-700 px-3 py-1 font-display text-[12px] font-semibold text-zinc-300 transition-colors hover:border-red-500/50 hover:text-red-300"
          >
            Deny
          </button>
        </div>
      )}
    </div>
  )
}

function Labeled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 font-mono text-[9px] uppercase tracking-[0.2em] text-zinc-600">{label}</div>
      {children}
    </div>
  )
}

function dotClass(item: ToolCallItem): string {
  const base = 'h-1.5 w-1.5 rounded-full '
  if (!item.done) return base + 'bg-amber-400 animate-breathe'
  return base + (item.resultIsError ? 'bg-red-500' : 'bg-emerald-400')
}

function stringify(v: unknown): string {
  if (v == null) return '∅'
  if (typeof v === 'string') return v
  try {
    return JSON.stringify(v, null, 2)
  } catch {
    return String(v)
  }
}

function oneLine(v: unknown): string {
  if (v == null || typeof v !== 'object') return ''
  const o = v as Record<string, unknown>
  const k = o.command ?? o.file_path ?? o.path ?? o.pattern ?? o.url ?? o.description
  return typeof k === 'string' ? k : ''
}

function AssistantMark() {
  return (
    <span className="mt-0.5 grid h-6 w-6 shrink-0 place-items-center rounded-md border border-emerald-500/30 bg-emerald-500/10">
      <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 text-emerald-400" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
        <path d="M12 3v18M3 12h18" strokeLinecap="round" />
        <path d="M6.5 6.5l11 11M17.5 6.5l-11 11" strokeLinecap="round" opacity="0.45" />
      </svg>
    </span>
  )
}

function WrenchIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0 text-zinc-500" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
      <path d="M14.7 6.3a3.5 3.5 0 0 0-4.6 4.3L4 16.7 7.3 20l6.1-6.1a3.5 3.5 0 0 0 4.3-4.6l-2.1 2.1-1.8-1.8z" strokeLinejoin="round" />
    </svg>
  )
}

function ShieldIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
      <path d="M12 3 5 6v6c0 4 3 6.5 7 8 4-1.5 7-4 7-8V6z" strokeLinejoin="round" />
    </svg>
  )
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={`h-3.5 w-3.5 shrink-0 text-zinc-600 transition-transform ${open ? 'rotate-90' : ''}`}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden
    >
      <path d="M9 6l6 6-6 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

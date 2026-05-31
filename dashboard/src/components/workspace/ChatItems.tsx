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
      <div className="msg user">
        <span className="who">you</span>
        <div className="bubble">{item.text}</div>
      </div>
    )
  }
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const html = useMemo(() => renderMarkdown(item.text), [item.text])
  return (
    <div className="msg ai">
      <span className="who">Claude</span>
      <div className="bubble">
        <div dangerouslySetInnerHTML={{ __html: html }} />
        {item.streaming && <span className="caret-blink" />}
      </div>
    </div>
  )
}

function ToolCard({ item }: { item: ToolCallItem }) {
  const [open, setOpen] = useState(false)
  const label = toolLabel(item)
  const icon = toolIcon(item.name)
  const isDone = item.done
  const isError = item.resultIsError

  return (
    <div className="tool">
      <button
        type="button"
        className="tool-h"
        style={{ width: '100%', textAlign: 'left', cursor: 'pointer' }}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="ic" style={{ display: 'flex', alignItems: 'center', width: 16, height: 16, flexShrink: 0 }}>
          {icon}
        </span>
        <span className="lab" dangerouslySetInnerHTML={{ __html: label }} />
        {isDone ? (
          <span className={`st ok`}>
            <CheckIcon />
            {isError ? 'error' : 'done'}
          </span>
        ) : (
          <span className="st run">
            <span className="ic" style={{ display: 'flex', alignItems: 'center', width: 12, height: 12 }}>
              <SpinnerSmall />
            </span>
            running
          </span>
        )}
      </button>
      {open && isDone && item.result && (
        <div className="tool-detail">
          {formatResult(item.result, isError)}
        </div>
      )}
    </div>
  )
}

function formatResult(result: string, isError?: boolean): React.ReactNode {
  if (isError) return <span style={{ color: 'var(--st-danger)' }}>{result}</span>
  // Detect diff-like content
  const lines = result.split('\n')
  const hasDiff = lines.some((l) => l.startsWith('+') || l.startsWith('-'))
  if (hasDiff) {
    return (
      <>
        {lines.map((line, i) => {
          if (line.startsWith('+')) return <span key={i} className="add">{line}{'\n'}</span>
          if (line.startsWith('-')) return <span key={i} className="del">{line}{'\n'}</span>
          return <span key={i}>{line}{'\n'}</span>
        })}
      </>
    )
  }
  return result
}

function toolLabel(item: ToolCallItem): string {
  const n = item.name
  const inp = item.input as Record<string, unknown> | null | undefined
  const file = inp?.file_path ?? inp?.path ?? inp?.pattern ?? inp?.url
  const cmd = inp?.command

  if (n === 'Read' || n === 'Write' || n === 'Edit' || n === 'MultiEdit') {
    const verb = n === 'Read' ? 'Read' : n === 'Write' ? 'Write' : 'Edited'
    return file ? `${verb} <b>${shortPath(String(file))}</b>` : verb
  }
  if (n === 'Bash' || n === 'Shell') {
    return cmd ? `Ran <b>${String(cmd).slice(0, 60)}</b>` : 'Ran command'
  }
  if (n === 'Task') return 'Spawned task'
  if (n === 'WebSearch' || n === 'WebFetch') return 'Web search'
  if (n === 'TodoRead' || n === 'TodoWrite') return 'Todo list'
  if (n === 'Glob' || n === 'GlobList') return file ? `Find <b>${String(file)}</b>` : 'Find files'
  if (n === 'Grep' || n === 'Search') return inp?.pattern ? `Search <b>${String(inp.pattern)}</b>` : 'Search'
  if (n === 'LS') return file ? `List <b>${shortPath(String(file))}</b>` : 'List dir'
  return `<b>${n}</b>`
}

function shortPath(p: string): string {
  const parts = p.split('/')
  return parts.length > 2 ? parts.slice(-2).join('/') : p
}

function toolIcon(name: string): React.ReactNode {
  if (name === 'Read' || name === 'Write' || name === 'Edit' || name === 'MultiEdit') {
    return <FileCodeIcon />
  }
  if (name === 'Bash' || name === 'Shell') return <TerminalIcon />
  if (name === 'Glob' || name === 'GlobList' || name === 'LS') return <FolderIcon />
  if (name === 'Grep' || name === 'Search') return <SearchIcon />
  if (name === 'Task') return <ZapIcon />
  if (name === 'WebSearch' || name === 'WebFetch') return <GlobeIcon />
  return <WrenchIcon />
}

function SystemBanner({ item }: { item: SystemItem }) {
  return (
    <div className="msg ai">
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '2px 0' }}>
        <span style={{ flex: 1, height: 1, background: 'var(--border)' }} />
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.18em', color: 'var(--fg-3)' }}>
          {item.text}
        </span>
        {item.model && (
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--teal)', opacity: 0.7 }}>
            · {item.model}
          </span>
        )}
        <span style={{ flex: 1, height: 1, background: 'var(--border)' }} />
      </div>
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
    <div className="tool" style={{ borderColor: 'color-mix(in oklch, var(--st-starting) 50%, transparent)' }}>
      <div className="tool-h">
        <span className="ic" style={{ display: 'flex', alignItems: 'center', width: 14, height: 14 }}>
          <ShieldIcon />
        </span>
        <span className="lab">
          permission requested{item.toolName ? `: ${item.toolName}` : ''}
        </span>
        {item.resolved ? (
          <span className="st ok">
            <CheckIcon />
            {item.allowed ? 'allowed' : 'denied'}
          </span>
        ) : null}
      </div>
      {!item.resolved && (
        <div className="tool-detail" style={{ display: 'flex', gap: 8 }}>
          <button
            type="button"
            onClick={() => onPermission(item.id, true)}
            style={{
              background: 'var(--teal)',
              color: 'var(--fg-on-cool)',
              border: 'none',
              borderRadius: 8,
              padding: '5px 12px',
              fontFamily: 'var(--font-ui)',
              fontSize: 12,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Allow
          </button>
          <button
            type="button"
            onClick={() => onPermission(item.id, false)}
            style={{
              background: 'none',
              color: 'var(--fg-2)',
              border: '1px solid var(--border)',
              borderRadius: 8,
              padding: '5px 12px',
              fontFamily: 'var(--font-ui)',
              fontSize: 12,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Deny
          </button>
        </div>
      )}
    </div>
  )
}

// ---- Icons (inline SVG, 14x14 stroke) ----

function FileCodeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
      <line x1="10" y1="13" x2="8" y2="15" />
      <line x1="10" y1="17" x2="8" y2="15" />
      <line x1="14" y1="13" x2="16" y2="15" />
      <line x1="14" y1="17" x2="16" y2="15" />
    </svg>
  )
}

function TerminalIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    </svg>
  )
}

function FolderIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
    </svg>
  )
}

function SearchIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="11" cy="11" r="8" />
      <line x1="21" y1="21" x2="16.65" y2="16.65" />
    </svg>
  )
}

function ZapIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
    </svg>
  )
}

function GlobeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="12" cy="12" r="10" />
      <line x1="2" y1="12" x2="22" y2="12" />
      <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
    </svg>
  )
}

function WrenchIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M14.7 6.3a3.5 3.5 0 0 0-4.6 4.3L4 16.7 7.3 20l6.1-6.1a3.5 3.5 0 0 0 4.3-4.6l-2.1 2.1-1.8-1.8z" />
    </svg>
  )
}

function ShieldIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M12 3 5 6v6c0 4 3 6.5 7 8 4-1.5 7-4 7-8V6z" />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 24 24" width={11} height={11} fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <polyline points="20 6 9 17 4 12" />
    </svg>
  )
}

function SpinnerSmall() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden
      style={{ animation: 'spin 1s linear infinite' }}>
      <path d="M12 3a9 9 0 1 0 9 9" />
    </svg>
  )
}

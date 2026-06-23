import { useEffect, useMemo, useRef, useState } from 'react'
import { fetchTranscript } from '../../api'
import type { Transcript, TranscriptBlock } from '../../types'
import { renderMarkdown } from '../../lib/markdown'

interface Props {
  sessionId: string
}

// Renders a session's SAVED conversation (F16 / fixes F15) — the read-only view
// shown when a claude session is no longer a live PTY. It reads Claude Code's
// on-disk .jsonl (parsed by the hub) so an exited/archived/trashed/restored
// session shows its full history word-for-word instead of a blank tab. Tool runs
// and thinking collapse by default so a long build log never buries the prose.
export function SessionTranscript({ sessionId }: Props) {
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [data, setData] = useState<Transcript | null>(null)
  const [err, setErr] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false
    setState('loading')
    setData(null)
    fetchTranscript(sessionId)
      .then((t) => {
        if (cancelled) return
        setData(t)
        setState('ready')
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setErr(e instanceof Error ? e.message : String(e))
        setState('error')
      })
    return () => {
      cancelled = true
    }
  }, [sessionId])

  // Pair each tool_use with its tool_result (same toolUseId) so a call + its
  // output render as one collapsible card — the consumed results are then skipped
  // in the top-level walk.
  const { items, consumed } = useMemo(() => pairBlocks(data?.blocks ?? []), [data])

  // Auto-scroll to the latest turn once the transcript lands (it's history, so the
  // bottom is the most recent — match the live xterm's behaviour).
  useEffect(() => {
    if (state === 'ready' && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [state, items])

  if (state === 'loading') {
    return (
      <Shell>
        <div className="grid h-full place-items-center">
          <span className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-wider text-zinc-500">
            <span className="h-1.5 w-1.5 rounded-full bg-amber-400 animate-breathe" />
            loading transcript…
          </span>
        </div>
      </Shell>
    )
  }

  if (state === 'error') {
    return (
      <Shell>
        <div className="grid h-full place-items-center px-6 text-center">
          <p className="font-mono text-xs text-red-400">couldn't load the transcript — {err}</p>
        </div>
      </Shell>
    )
  }

  if (!data?.found || items.length === 0) {
    return (
      <Shell meta={data ?? undefined}>
        <div className="grid h-full place-items-center px-6 text-center">
          <div className="max-w-sm space-y-1.5">
            <p className="font-mono text-xs text-zinc-400">no saved transcript to show</p>
            <p className="font-mono text-[11px] leading-relaxed text-zinc-600">
              {data?.reason || 'this session has no on-disk conversation log.'}
            </p>
          </div>
        </div>
      </Shell>
    )
  }

  return (
    <Shell meta={data}>
      <div ref={scrollRef} className="term-scroll absolute inset-0 overflow-y-auto px-4 py-4">
        <div className="mx-auto flex max-w-3xl flex-col gap-3">
          {items.map((it) =>
            it.kind === 'tool_use' ? (
              <ToolCard key={it.block.seq} call={it.block} result={consumed.get(it.block.toolUseId ?? '')} />
            ) : (
              <BlockView key={it.block.seq} block={it.block} />
            ),
          )}
          <div className="py-2 text-center font-mono text-[10px] uppercase tracking-wider text-zinc-700">
            — end of transcript —
          </div>
        </div>
      </div>
    </Shell>
  )
}

// Shell wraps the transcript body with the read-only header + token/cost meta bar.
function Shell({ children, meta }: { children: React.ReactNode; meta?: Transcript }) {
  return (
    <div className="flex h-full min-h-0 flex-col bg-zinc-950">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-zinc-800 px-4 py-2 font-mono text-[10px] uppercase tracking-wider">
        <span className="flex items-center gap-1.5 text-zinc-400">
          <ArchiveIcon /> saved transcript
        </span>
        <span className="text-zinc-600">read-only</span>
        {meta?.found && <MetaBar meta={meta} />}
      </div>
      <div className="relative min-h-0 flex-1">{children}</div>
    </div>
  )
}

function MetaBar({ meta }: { meta: Transcript }) {
  const m = meta.meta
  const out = fmtTokens(m.outputTokens)
  const inTok = fmtTokens(m.inputTokens)
  const cache = fmtTokens(m.cacheReadTokens + m.cacheCreationTokens)
  return (
    <span className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-0.5 text-zinc-600">
      {m.model && <span className="text-zinc-500">{m.model}</span>}
      {m.messageCount > 0 && <span>{m.messageCount} turns</span>}
      <span title="input / output / cached tokens (whole conversation)">
        <span className="text-zinc-500">{inTok}</span> in · <span className="text-zinc-500">{out}</span> out
        {cache !== '0' && <> · {cache} cached</>}
      </span>
    </span>
  )
}

// ───────────────────────────── block rendering ─────────────────────────────

interface RenderItem {
  kind: 'block' | 'tool_use'
  block: TranscriptBlock
}

// pairBlocks turns the flat block list into top-level render items, attaching each
// tool_result to its tool_use by toolUseId. Returns the items to render plus a map
// of consumed results (so they aren't rendered twice).
function pairBlocks(blocks: TranscriptBlock[]): { items: RenderItem[]; consumed: Map<string, TranscriptBlock> } {
  const resultByTool = new Map<string, TranscriptBlock>()
  for (const b of blocks) {
    if (b.kind === 'tool_result' && b.toolUseId) resultByTool.set(b.toolUseId, b)
  }
  const consumed = new Map<string, TranscriptBlock>()
  const items: RenderItem[] = []
  for (const b of blocks) {
    if (b.kind === 'tool_use') {
      items.push({ kind: 'tool_use', block: b })
      const res = b.toolUseId ? resultByTool.get(b.toolUseId) : undefined
      if (res) consumed.set(b.toolUseId!, res)
      continue
    }
    // A tool_result already paired to its call is skipped here.
    if (b.kind === 'tool_result' && b.toolUseId && resultByTool.get(b.toolUseId) === b && hasCall(blocks, b.toolUseId)) {
      continue
    }
    items.push({ kind: 'block', block: b })
  }
  return { items, consumed }
}

function hasCall(blocks: TranscriptBlock[], toolUseId: string): boolean {
  return blocks.some((b) => b.kind === 'tool_use' && b.toolUseId === toolUseId)
}

function BlockView({ block }: { block: TranscriptBlock }) {
  switch (block.kind) {
    case 'text':
      return block.role === 'user' ? <UserText block={block} /> : <AssistantText block={block} />
    case 'thinking':
      return <Thinking block={block} />
    case 'tool_result':
      return <ToolCard call={undefined} result={block} />
    case 'image':
      return (
        <div className="self-start rounded border border-zinc-800 bg-zinc-900/40 px-2 py-1 font-mono text-[11px] text-zinc-500">
          🖼 image
        </div>
      )
    default:
      return null
  }
}

function UserText({ block }: { block: TranscriptBlock }) {
  return (
    <div className="self-end max-w-[88%] rounded-lg border border-emerald-500/20 bg-emerald-500/[0.07] px-3 py-2">
      <div className="mb-1 flex items-center gap-1.5 font-mono text-[9px] uppercase tracking-wider text-emerald-300/70">
        you {block.sidechain && <SideTag />}
      </div>
      <div
        className="prose-transcript text-[13px] leading-relaxed text-zinc-200"
        dangerouslySetInnerHTML={{ __html: renderMarkdown(block.text ?? '') }}
      />
      {block.truncated && <TruncatedNote />}
    </div>
  )
}

function AssistantText({ block }: { block: TranscriptBlock }) {
  return (
    <div className="self-start max-w-[92%]">
      <div className="mb-1 flex items-center gap-1.5 font-mono text-[9px] uppercase tracking-wider text-zinc-500">
        claude {block.sidechain && <SideTag />}
      </div>
      <div
        className="prose-transcript text-[13px] leading-relaxed text-zinc-300"
        dangerouslySetInnerHTML={{ __html: renderMarkdown(block.text ?? '') }}
      />
      {block.truncated && <TruncatedNote />}
    </div>
  )
}

function Thinking({ block }: { block: TranscriptBlock }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="self-start max-w-[92%]">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-wider text-zinc-600 hover:text-zinc-400"
      >
        <Chevron open={open} /> thought
      </button>
      {open && (
        <div className="mt-1 border-l-2 border-zinc-800 pl-3 text-[12px] italic leading-relaxed text-zinc-500 whitespace-pre-wrap">
          {block.text}
          {block.truncated && <TruncatedNote />}
        </div>
      )}
    </div>
  )
}

// ToolCard renders a tool_use call together with its result, collapsed by default.
// Used both for a paired call+result and for an orphan result (call undefined).
function ToolCard({ call, result }: { call?: TranscriptBlock; result?: TranscriptBlock }) {
  const [open, setOpen] = useState(false)
  const name = call?.toolName ?? 'tool result'
  const summary = call ? inputSummary(call.toolInput) : ''
  const isError = result?.isError
  return (
    <div className={`self-start w-full max-w-[92%] overflow-hidden rounded-md border ${isError ? 'border-red-500/30' : 'border-zinc-800'} bg-zinc-900/40`}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left hover:bg-zinc-800/40"
      >
        <Chevron open={open} />
        <WrenchIcon error={isError} />
        <span className={`font-mono text-[11px] font-semibold ${isError ? 'text-red-300' : 'text-sky-300'}`}>{name}</span>
        {summary && <span className="truncate font-mono text-[11px] text-zinc-500">{summary}</span>}
        {isError && <span className="ml-auto font-mono text-[9px] uppercase tracking-wider text-red-400">error</span>}
        {call?.sidechain && <span className="ml-auto"><SideTag /></span>}
      </button>
      {open && (
        <div className="border-t border-zinc-800 px-2.5 py-2">
          {call && call.toolInput != null && (
            <pre className="mb-2 overflow-x-auto whitespace-pre-wrap break-words rounded bg-zinc-950/60 px-2 py-1.5 font-mono text-[11px] leading-relaxed text-zinc-400">
              {prettyInput(call.toolInput)}
            </pre>
          )}
          {result && (
            <>
              <div className="mb-1 font-mono text-[9px] uppercase tracking-wider text-zinc-600">output</div>
              <pre className={`max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded bg-zinc-950/60 px-2 py-1.5 font-mono text-[11px] leading-relaxed ${isError ? 'text-red-300/90' : 'text-zinc-400'}`}>
                {result.text || '(no output)'}
              </pre>
              {result.truncated && <TruncatedNote />}
            </>
          )}
          {!result && <div className="font-mono text-[11px] text-zinc-600">(no result captured)</div>}
        </div>
      )}
    </div>
  )
}

// ───────────────────────────── small helpers ─────────────────────────────

function SideTag() {
  return <span className="rounded bg-violet-500/15 px-1 py-px text-[8px] font-semibold text-violet-300">sub-agent</span>
}

function TruncatedNote() {
  return <div className="mt-1 font-mono text-[10px] italic text-zinc-600">…[truncated for length]</div>
}

// inputSummary picks the most representative field of a tool's input for the
// one-line collapsed header (command / file / pattern / url).
function inputSummary(input: unknown): string {
  if (!input || typeof input !== 'object') return ''
  const o = input as Record<string, unknown>
  for (const k of ['command', 'file_path', 'path', 'pattern', 'url', 'query', 'prompt', 'description']) {
    const v = o[k]
    if (typeof v === 'string' && v.trim()) return oneLine(v)
  }
  return ''
}

function prettyInput(input: unknown): string {
  if (typeof input === 'string') return input
  try {
    return JSON.stringify(input, null, 2)
  } catch {
    return String(input)
  }
}

function oneLine(s: string): string {
  const flat = s.replace(/\s+/g, ' ').trim()
  return flat.length > 140 ? flat.slice(0, 140) + '…' : flat
}

function fmtTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1).replace(/\.0$/, '') + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k'
  return String(n)
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={`h-3 w-3 shrink-0 text-zinc-500 transition-transform ${open ? 'rotate-90' : ''}`} fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="M9 6l6 6-6 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function WrenchIcon({ error }: { error?: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={`h-3 w-3 shrink-0 ${error ? 'text-red-400' : 'text-sky-400'}`} fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4l-5 5a1.5 1.5 0 0 0 2.1 2.1l5-5a4 4 0 0 0 5.4-5.4l-2.3 2.3-2.1-2.1 2.3-2.3z" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function ArchiveIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 text-zinc-500" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M3 5h18v4H3zM5 9v10h14V9M9 13h6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

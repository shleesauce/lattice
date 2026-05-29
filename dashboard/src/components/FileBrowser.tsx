import { useCallback, useEffect, useState } from 'react'
import type { FileEntry, FileListResult } from '../types'
import { downloadUrl, fetchFiles } from '../api'
import { humanBytes, shortTime } from '../format'

interface Props {
  agentId: string
  online: boolean
}

export function FileBrowser({ agentId, online }: Props) {
  const [path, setPath] = useState('') // empty ⇒ agent home dir
  const [data, setData] = useState<FileListResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    (target: string) => {
      let cancelled = false
      setLoading(true)
      setError(null)
      fetchFiles(agentId, target)
        .then((res) => {
          if (cancelled) return
          if (res.error) {
            setError(res.error)
          } else {
            setData(res)
          }
        })
        .catch((e: unknown) => {
          if (!cancelled) setError(e instanceof Error ? e.message : 'failed to list directory')
        })
        .finally(() => {
          if (!cancelled) setLoading(false)
        })
      return () => {
        cancelled = true
      }
    },
    [agentId],
  )

  // Reset to home dir whenever the selected agent changes.
  useEffect(() => {
    setPath('')
  }, [agentId])

  useEffect(() => load(path), [load, path])

  if (!online) {
    return (
      <div className="grid flex-1 place-items-center px-6 text-center">
        <p className="font-mono text-xs text-zinc-500">agent offline — files unavailable</p>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* path bar */}
      <div className="flex items-center gap-2 border-b border-zinc-800 px-3 py-2">
        <button
          type="button"
          onClick={() => data?.parent && setPath(data.parent)}
          disabled={!data?.parent || loading}
          title="up one level"
          className="rounded border border-zinc-700 bg-zinc-950 px-2 py-1 font-mono text-[11px] text-zinc-300 transition-colors hover:border-emerald-500/50 hover:text-emerald-300 disabled:cursor-not-allowed disabled:opacity-40"
        >
          ↑ up
        </button>
        <code className="min-w-0 flex-1 truncate rounded border border-zinc-800 bg-zinc-950 px-2.5 py-1 font-mono text-[11px] text-zinc-400">
          {data?.path || (loading ? 'loading…' : '~')}
        </code>
      </div>

      {/* listing */}
      <div className="term-scroll min-h-0 flex-1 overflow-y-auto">
        {loading ? (
          <SkeletonList />
        ) : error ? (
          <div className="px-4 py-10 text-center">
            <p className="font-display text-sm font-semibold text-red-300">cannot read directory</p>
            <p className="mt-1 font-mono text-[11px] text-red-400/70">{error}</p>
            <button
              type="button"
              onClick={() => load(path)}
              className="mt-3 rounded border border-zinc-700 px-3 py-1 font-mono text-[11px] text-zinc-300 hover:border-emerald-500/50"
            >
              retry
            </button>
          </div>
        ) : !data || data.entries.length === 0 ? (
          <p className="px-4 py-10 text-center font-mono text-xs text-zinc-600">// empty directory</p>
        ) : (
          <ul className="divide-y divide-zinc-800/60">
            {data.entries.map((e) => (
              <Row key={e.path} entry={e} agentId={agentId} onOpen={() => setPath(e.path)} />
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function Row({ entry, agentId, onOpen }: { entry: FileEntry; agentId: string; onOpen: () => void }) {
  const common =
    'flex w-full items-center gap-3 px-4 py-2 text-left font-mono text-[12px] transition-colors hover:bg-zinc-900'
  if (entry.isDir) {
    return (
      <li>
        <button type="button" onClick={onOpen} className={common}>
          <FolderIcon />
          <span className="min-w-0 flex-1 truncate text-zinc-200">{entry.name}</span>
          <span className="text-zinc-600">dir</span>
        </button>
      </li>
    )
  }
  return (
    <li>
      <a href={downloadUrl(agentId, entry.path)} className={common} title={`download ${entry.name}`}>
        <FileIcon />
        <span className="min-w-0 flex-1 truncate text-zinc-300">{entry.name}</span>
        <span className="shrink-0 text-zinc-500">{humanBytes(entry.size)}</span>
        <span className="hidden shrink-0 text-zinc-600 sm:inline">{shortTime(entry.modTime)}</span>
        <DownloadIcon />
      </a>
    </li>
  )
}

function SkeletonList() {
  return (
    <ul className="divide-y divide-zinc-800/60">
      {Array.from({ length: 8 }).map((_, i) => (
        <li key={i} className="flex items-center gap-3 px-4 py-2">
          <span className="h-4 w-4 animate-pulse rounded bg-zinc-800" />
          <span className="h-3 flex-1 animate-pulse rounded bg-zinc-800/70" style={{ maxWidth: `${40 + (i % 4) * 12}%` }} />
        </li>
      ))}
    </ul>
  )
}

function FolderIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4 shrink-0 text-emerald-400/80" fill="currentColor" aria-hidden>
      <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  )
}

function FileIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4 shrink-0 text-zinc-500" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
      <path d="M6 2h8l4 4v16H6z" strokeLinejoin="round" />
      <path d="M14 2v4h4" strokeLinejoin="round" />
    </svg>
  )
}

function DownloadIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0 text-zinc-600" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M12 3v12m0 0 4-4m-4 4-4-4" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M4 19h16" strokeLinecap="round" />
    </svg>
  )
}

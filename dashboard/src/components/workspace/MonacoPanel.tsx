import { lazy, Suspense, useEffect, useState } from 'react'
import { downloadUrl, fetchFiles } from '../../api'
import type { FileEntry, FileListResult } from '../../types'
import { humanBytes } from '../../format'

// Monaco is heavy — lazy-load it so it never touches the initial bundle. The
// @monaco-editor wrapper fetches the editor core on demand (default CDN loader),
// keeping the lazy chunk tiny; bundling monaco-editor itself pulls ~8MB of
// language workers we don't need for a read-only viewer.
const Editor = lazy(() => import('@monaco-editor/react').then((m) => ({ default: m.default })))

interface Props {
  agentId: string
  rootPath: string
}

function langFromName(name: string): string {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, string> = {
    ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript',
    go: 'go', py: 'python', rs: 'rust', json: 'json', md: 'markdown',
    css: 'css', html: 'html', sh: 'shell', yml: 'yaml', yaml: 'yaml',
    toml: 'ini', sql: 'sql', java: 'java', rb: 'ruby', c: 'c', cpp: 'cpp',
  }
  return map[ext] ?? 'plaintext'
}

const MAX_VIEW_BYTES = 512 * 1024

// A collapsible read-only file viewer fed by the existing file endpoints.
// View-only is acceptable for v1 (write isn't wired server-side).
export function MonacoPanel({ agentId, rootPath }: Props) {
  const [path, setPath] = useState(rootPath)
  const [list, setList] = useState<FileListResult | null>(null)
  const [listErr, setListErr] = useState<string | null>(null)
  const [file, setFile] = useState<{ name: string; content: string } | null>(null)
  const [fileState, setFileState] = useState<'idle' | 'loading' | 'error'>('idle')

  useEffect(() => {
    setPath(rootPath)
    setFile(null)
  }, [rootPath, agentId])

  useEffect(() => {
    let cancelled = false
    setListErr(null)
    fetchFiles(agentId, path)
      .then((r) => {
        if (cancelled) return
        if (r.error) setListErr(r.error)
        else setList(r)
      })
      .catch((e: unknown) => !cancelled && setListErr(e instanceof Error ? e.message : 'failed to list'))
    return () => {
      cancelled = true
    }
  }, [agentId, path])

  const openFile = async (e: FileEntry) => {
    if (e.size > MAX_VIEW_BYTES) {
      setFileState('error')
      setFile({ name: e.name, content: '' })
      return
    }
    setFileState('loading')
    try {
      const res = await fetch(downloadUrl(agentId, e.path))
      if (!res.ok) throw new Error(`${res.status}`)
      const content = await res.text()
      setFile({ name: e.name, content })
      setFileState('idle')
    } catch {
      setFileState('error')
      setFile({ name: e.name, content: '' })
    }
  }

  return (
    <div className="flex h-full min-h-0">
      {/* mini file tree */}
      <div className="flex w-56 shrink-0 flex-col border-r border-zinc-800 bg-zinc-950/50">
        <div className="flex items-center gap-2 border-b border-zinc-800 px-2.5 py-1.5">
          <button
            type="button"
            onClick={() => list?.parent && setPath(list.parent)}
            disabled={!list?.parent}
            className="rounded border border-zinc-700 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 hover:border-emerald-500/50 disabled:opacity-30"
          >
            ↑
          </button>
          <code className="min-w-0 flex-1 truncate font-mono text-[10px] text-zinc-500">{list?.path ?? '…'}</code>
        </div>
        <div className="term-scroll min-h-0 flex-1 overflow-y-auto">
          {listErr ? (
            <p className="px-3 py-4 font-mono text-[10px] text-red-400">{listErr}</p>
          ) : (
            <ul>
              {list?.entries.map((e) => (
                <li key={e.path}>
                  <button
                    type="button"
                    onClick={() => (e.isDir ? setPath(e.path) : openFile(e))}
                    className={`flex w-full items-center gap-2 px-3 py-1 text-left font-mono text-[11px] hover:bg-zinc-900 ${
                      file?.name === e.name && !e.isDir ? 'text-emerald-300' : 'text-zinc-300'
                    }`}
                  >
                    <span className={e.isDir ? 'text-emerald-400/70' : 'text-zinc-600'}>{e.isDir ? '▸' : '·'}</span>
                    <span className="min-w-0 flex-1 truncate">{e.name}</span>
                    {!e.isDir && <span className="shrink-0 text-zinc-700">{humanBytes(e.size)}</span>}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* editor */}
      <div className="min-w-0 flex-1 bg-[#09090b]">
        {!file ? (
          <div className="grid h-full place-items-center font-mono text-[11px] text-zinc-600">// select a file to view</div>
        ) : fileState === 'loading' ? (
          <div className="grid h-full place-items-center font-mono text-[11px] text-zinc-500">loading {file.name}…</div>
        ) : fileState === 'error' ? (
          <div className="grid h-full place-items-center px-6 text-center font-mono text-[11px] text-red-400">
            cannot preview {file.name} (too large or binary) — use download
          </div>
        ) : (
          <Suspense fallback={<div className="grid h-full place-items-center font-mono text-[11px] text-zinc-600">loading editor…</div>}>
            <Editor
              height="100%"
              theme="vs-dark"
              path={file.name}
              defaultLanguage={langFromName(file.name)}
              value={file.content}
              options={{
                readOnly: true,
                fontSize: 12.5,
                fontFamily: "'JetBrains Mono', ui-monospace, monospace",
                minimap: { enabled: false },
                scrollBeyondLastLine: false,
                renderLineHighlight: 'none',
              }}
            />
          </Suspense>
        )}
      </div>
    </div>
  )
}

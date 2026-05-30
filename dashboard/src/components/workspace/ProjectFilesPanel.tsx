import { useMemo } from 'react'
import type { Project } from '../../types'
import { humanBytes } from '../../format'
import { FileViewer } from './FileViewer'
import { useFileBrowser } from './useFileBrowser'

interface Props {
  // The browse root. `path` may be empty for a device home (the file endpoint
  // lists the agent's home dir when path is blank); `name` is the panel title.
  project: Project
  agentId: string
  onClose: () => void
}

// Breadcrumb segments relative to the browse root. The root itself stays a
// single labelled crumb so we never expose the host's full absolute path.
interface Crumb {
  label: string
  path: string
}

function buildCrumbs(rootPath: string, rootLabel: string, current: string): Crumb[] {
  const crumbs: Crumb[] = [{ label: rootLabel, path: rootPath }]
  if (!current || current === rootPath) return crumbs
  const rel = rootPath && current.startsWith(rootPath) ? current.slice(rootPath.length) : current
  const sep = rel.includes('\\') ? '\\' : '/'
  const segments = rel.split(sep).filter(Boolean)
  let acc = rootPath
  for (const seg of segments) {
    acc = acc ? `${acc}${sep}${seg}` : `${sep}${seg}`
    crumbs.push({ label: seg, path: acc })
  }
  return crumbs
}

// Persistent right-rail explorer. Driven by project (or device) selection rather
// than the active session: the tree is rooted at project.path and browsed via a
// chosen online agent. Reuses the shared file-browser engine + lazy viewer.
export function ProjectFilesPanel({ project, agentId, onClose }: Props) {
  const fb = useFileBrowser(agentId, project.path)

  const crumbs = useMemo(
    () => buildCrumbs(project.path, project.name, fb.list?.path ?? project.path),
    [project.path, project.name, fb.list?.path],
  )

  return (
    <div className="flex h-full min-h-0 flex-col bg-zinc-950/60">
      {/* header */}
      <div className="flex items-center gap-2 border-b border-zinc-800 px-3 py-2.5">
        <ExplorerIcon />
        <div className="min-w-0 flex-1">
          <div className="truncate font-display text-[13px] font-semibold text-zinc-100">{project.name}</div>
          <code className="block truncate font-mono text-[10px] text-zinc-600">{project.path || '~'}</code>
        </div>
        <button
          type="button"
          onClick={onClose}
          title="collapse files panel"
          className="grid h-7 w-7 shrink-0 place-items-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
        >
          <CollapseIcon />
        </button>
      </div>

      {/* breadcrumb + up-nav */}
      <div className="flex items-center gap-1.5 border-b border-zinc-800 bg-zinc-950/40 px-2.5 py-1.5">
        <button
          type="button"
          onClick={fb.goUp}
          disabled={!fb.list?.parent || fb.list.path === project.path}
          title="up one level"
          className="grid h-5 w-5 shrink-0 place-items-center rounded border border-zinc-700 text-zinc-400 transition-colors hover:border-emerald-500/50 hover:text-emerald-300 disabled:opacity-30"
        >
          <svg viewBox="0 0 24 24" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
            <path d="M12 19V5m0 0-6 6m6-6 6 6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
        <div className="term-scroll flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto whitespace-nowrap">
          {crumbs.map((c, i) => {
            const last = i === crumbs.length - 1
            return (
              <span key={c.path} className="flex items-center gap-0.5">
                {i > 0 && <span className="text-zinc-700">/</span>}
                <button
                  type="button"
                  onClick={() => fb.navigate(c.path)}
                  disabled={last}
                  className={`max-w-[10rem] truncate rounded px-1 font-mono text-[10px] transition-colors ${
                    last ? 'text-emerald-300' : 'text-zinc-500 hover:text-zinc-300'
                  }`}
                >
                  {c.label}
                </button>
              </span>
            )
          })}
        </div>
      </div>

      {/* listing */}
      <div className="term-scroll max-h-[44%] min-h-0 shrink-0 overflow-y-auto border-b border-zinc-800">
        {fb.listState === 'loading' ? (
          <ListSkeleton />
        ) : fb.listErr ? (
          <p className="px-3 py-5 font-mono text-[11px] leading-relaxed text-red-400/90">// {fb.listErr}</p>
        ) : (fb.list?.entries.length ?? 0) === 0 ? (
          <p className="px-3 py-5 text-center font-mono text-[11px] text-zinc-600">// empty directory</p>
        ) : (
          <ul className="py-1">
            {fb.list?.entries.map((e) => {
              const activeFile = fb.file?.path === e.path && !e.isDir
              return (
                <li key={e.path}>
                  <button
                    type="button"
                    onClick={() => (e.isDir ? fb.navigate(e.path) : fb.openFile(e))}
                    className={`flex w-full items-center gap-2 px-3 py-1 text-left font-mono text-[11px] transition-colors ${
                      activeFile ? 'bg-emerald-500/10 text-emerald-200' : 'text-zinc-300 hover:bg-zinc-900/70'
                    }`}
                  >
                    {e.isDir ? <DirGlyph /> : <FileGlyph />}
                    <span className="min-w-0 flex-1 truncate">{e.name}</span>
                    {!e.isDir && <span className="shrink-0 text-zinc-700">{humanBytes(e.size)}</span>}
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>

      {/* viewer */}
      <div className="min-h-0 flex-1 bg-[#09090b]">
        <FileViewer agentId={agentId} file={fb.file} fileState={fb.fileState} />
      </div>
    </div>
  )
}

function ListSkeleton() {
  return (
    <div className="space-y-1 px-3 py-2">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="flex items-center gap-2 py-0.5">
          <span className="h-3 w-3 animate-pulse rounded bg-zinc-800" />
          <span className="h-2.5 animate-pulse rounded bg-zinc-800/70" style={{ width: `${45 + (i % 4) * 13}%` }} />
        </div>
      ))}
    </div>
  )
}

function DirGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0 text-emerald-400/70" fill="currentColor" aria-hidden>
      <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  )
}

function FileGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0 text-zinc-600" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
      <path d="M6 3h7l5 5v13a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" strokeLinejoin="round" />
      <path d="M13 3v5h5" strokeLinejoin="round" />
    </svg>
  )
}

function ExplorerIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4 shrink-0 text-emerald-400/80" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
      <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" strokeLinejoin="round" />
    </svg>
  )
}

function CollapseIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.9" aria-hidden>
      <path d="M9 6l6 6-6 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

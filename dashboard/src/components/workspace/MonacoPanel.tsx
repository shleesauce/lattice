import { humanBytes } from '../../format'
import { FileViewer } from './FileViewer'
import { useFileBrowser } from './useFileBrowser'

interface Props {
  agentId: string
  rootPath: string
}

// A collapsible read-only file viewer fed by the existing file endpoints.
// Browser state + the lazy Monaco viewer are shared with the right-rail
// explorer via useFileBrowser / FileViewer.
export function MonacoPanel({ agentId, rootPath }: Props) {
  const fb = useFileBrowser(agentId, rootPath)

  return (
    <div className="flex h-full min-h-0">
      {/* mini file tree */}
      <div className="flex w-56 shrink-0 flex-col border-r border-zinc-800 bg-zinc-950/50">
        <div className="flex items-center gap-2 border-b border-zinc-800 px-2.5 py-1.5">
          <button
            type="button"
            onClick={fb.goUp}
            disabled={!fb.list?.parent}
            className="rounded border border-zinc-700 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 hover:border-emerald-500/50 disabled:opacity-30"
          >
            ↑
          </button>
          <code className="min-w-0 flex-1 truncate font-mono text-[10px] text-zinc-500">{fb.list?.path ?? '…'}</code>
        </div>
        <div className="term-scroll min-h-0 flex-1 overflow-y-auto">
          {fb.listErr ? (
            <p className="px-3 py-4 font-mono text-[10px] text-red-400">{fb.listErr}</p>
          ) : (
            <ul>
              {fb.list?.entries.map((e) => (
                <li key={e.path}>
                  <button
                    type="button"
                    onClick={() => (e.isDir ? fb.navigate(e.path) : fb.openFile(e))}
                    className={`flex w-full items-center gap-2 px-3 py-1 text-left font-mono text-[11px] hover:bg-zinc-900 ${
                      fb.file?.path === e.path && !e.isDir ? 'text-emerald-300' : 'text-zinc-300'
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
        <FileViewer agentId={agentId} file={fb.file} fileState={fb.fileState} />
      </div>
    </div>
  )
}

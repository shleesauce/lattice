import { lazy, Suspense } from 'react'
import { downloadUrl } from '../../api'
import { langFromName } from './useFileBrowser'
import type { FileViewState, OpenFile } from './useFileBrowser'

// Monaco is heavy — lazy-load it so it never touches the initial bundle. The
// @monaco-editor wrapper fetches the editor core on demand (default CDN loader),
// keeping the lazy chunk tiny; bundling monaco-editor itself pulls ~8MB of
// language workers we don't need for a read-only viewer.
const Editor = lazy(() => import('@monaco-editor/react').then((m) => ({ default: m.default })))

interface Props {
  agentId: string
  file: OpenFile | null
  fileState: FileViewState
}

// Read-only Monaco viewer with size-cap / binary fallbacks. Write isn't wired
// server-side yet, so view-only is the contract.
export function FileViewer({ agentId, file, fileState }: Props) {
  if (!file) {
    return (
      <div className="grid h-full place-items-center font-mono text-[11px] text-zinc-600">
        // select a file to view
      </div>
    )
  }

  if (fileState === 'loading') {
    return (
      <div className="grid h-full place-items-center font-mono text-[11px] text-zinc-500">
        loading {file.name}…
      </div>
    )
  }

  if (fileState === 'error') {
    return (
      <div className="grid h-full place-items-center px-6 text-center">
        <div className="max-w-xs">
          <p className="font-mono text-[11px] leading-relaxed text-red-400">
            cannot preview {file.name}
            <br />
            <span className="text-zinc-500">too large or binary</span>
          </p>
          <a
            href={downloadUrl(agentId, file.path)}
            className="mt-3 inline-flex items-center gap-1.5 rounded-md border border-emerald-500/40 bg-emerald-500/10 px-2.5 py-1 font-mono text-[11px] text-emerald-300 transition-colors hover:bg-emerald-500/20"
          >
            <DownloadIcon /> download
          </a>
        </div>
      </div>
    )
  }

  return (
    <Suspense
      fallback={
        <div className="grid h-full place-items-center font-mono text-[11px] text-zinc-600">
          loading editor…
        </div>
      }
    >
      <Editor
        height="100%"
        theme="vs-dark"
        path={file.path}
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
  )
}

function DownloadIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M12 4v12m0 0 4-4m-4 4-4-4M4 20h16" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

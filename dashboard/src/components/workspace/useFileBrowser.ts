import { useCallback, useEffect, useState } from 'react'
import { downloadUrl, fetchFiles } from '../../api'
import type { FileEntry, FileListResult } from '../../types'

export const MAX_VIEW_BYTES = 512 * 1024

export type FileViewState = 'idle' | 'loading' | 'error'

export interface OpenFile {
  name: string
  path: string
  content: string
}

export interface FileBrowser {
  path: string
  list: FileListResult | null
  listState: FileViewState
  listErr: string | null
  file: OpenFile | null
  fileState: FileViewState
  navigate: (path: string) => void
  goUp: () => void
  openFile: (entry: FileEntry) => Promise<void>
  closeFile: () => void
}

// Shared file-browser engine over the existing /files + /download endpoints.
// Owns the current directory, its listing, and the lazily-opened file. Both the
// in-session viewer and the right-rail explorer drive their UI from this.
export function useFileBrowser(agentId: string, rootPath: string): FileBrowser {
  const [path, setPath] = useState(rootPath)
  const [list, setList] = useState<FileListResult | null>(null)
  const [listState, setListState] = useState<FileViewState>('loading')
  const [listErr, setListErr] = useState<string | null>(null)
  const [file, setFile] = useState<OpenFile | null>(null)
  const [fileState, setFileState] = useState<FileViewState>('idle')

  // Reset to root whenever the target agent/project changes.
  useEffect(() => {
    setPath(rootPath)
    setFile(null)
    setFileState('idle')
  }, [rootPath, agentId])

  useEffect(() => {
    let cancelled = false
    setListState('loading')
    setListErr(null)
    fetchFiles(agentId, path)
      .then((r) => {
        if (cancelled) return
        if (r.error) {
          setListErr(r.error)
          setListState('error')
        } else {
          setList(r)
          setListState('idle')
        }
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setListErr(e instanceof Error ? e.message : 'failed to list')
        setListState('error')
      })
    return () => {
      cancelled = true
    }
  }, [agentId, path])

  const navigate = useCallback((next: string) => setPath(next), [])

  const goUp = useCallback(() => {
    setList((cur) => {
      if (cur?.parent) setPath(cur.parent)
      return cur
    })
  }, [])

  const openFile = useCallback(
    async (e: FileEntry) => {
      if (e.size > MAX_VIEW_BYTES) {
        setFile({ name: e.name, path: e.path, content: '' })
        setFileState('error')
        return
      }
      setFile({ name: e.name, path: e.path, content: '' })
      setFileState('loading')
      try {
        const res = await fetch(downloadUrl(agentId, e.path))
        if (!res.ok) throw new Error(`${res.status}`)
        const content = await res.text()
        setFile({ name: e.name, path: e.path, content })
        setFileState('idle')
      } catch {
        setFile({ name: e.name, path: e.path, content: '' })
        setFileState('error')
      }
    },
    [agentId],
  )

  const closeFile = useCallback(() => {
    setFile(null)
    setFileState('idle')
  }, [])

  return { path, list, listState, listErr, file, fileState, navigate, goUp, openFile, closeFile }
}

export function langFromName(name: string): string {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, string> = {
    ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript',
    go: 'go', py: 'python', rs: 'rust', json: 'json', md: 'markdown',
    css: 'css', html: 'html', sh: 'shell', yml: 'yaml', yaml: 'yaml',
    toml: 'ini', sql: 'sql', java: 'java', rb: 'ruby', c: 'c', cpp: 'cpp',
  }
  return map[ext] ?? 'plaintext'
}

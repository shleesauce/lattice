/* Right-side dock — a Claude-Code-desktop-style panel beside the active session.
   Switchable views, all scoped to the session's machine + project:
     Files    — browse the machine's filesystem (reuses /api/agents/{id}/files)
     Terminal — an ad-hoc PTY on the machine (/ws/terminal), cd'd into the project
     Preview  — an iframe onto a dev-server URL reachable over the tailnet
     Git      — the same PTY, auto-running `git status` + `git diff` for the project
   The dock is contextual: it follows whatever session is active. */
import { useEffect, useMemo, useState } from 'react'
import type { Agent, FileListResult, Session } from '../../types'
import { downloadUrl, fetchFiles, terminalWsUrl } from '../../api'
import { XtermSession } from './XtermSession'
import { Icon } from '../../lattice/Icon'
import { humanBytes } from '../../format'

export type DockView = 'files' | 'terminal' | 'preview' | 'git'

const VIEWS: { id: DockView; label: string; icon: string }[] = [
  { id: 'files', label: 'Files', icon: 'folder' },
  { id: 'terminal', label: 'Terminal', icon: 'terminal' },
  { id: 'preview', label: 'Preview', icon: 'monitor' },
  { id: 'git', label: 'Git', icon: 'git-branch' },
]

interface Props {
  session: Session
  agents: Agent[]
  view: DockView
  onView: (v: DockView) => void
  onClose: () => void
}

export function DockPanel({ session, agents, view, onView, onClose }: Props) {
  const machine = agents.find((a) => a.id === session.agentId)
  const machineLabel = machine?.hostname || machine?.name || session.agentId.slice(0, 8)
  const cwd = session.scope === 'device' ? '' : session.projectPath

  return (
    <div className="dock">
      <div className="dock-head">
        <div className="dock-tabs">
          {VIEWS.map((v) => (
            <button key={v.id} type="button" className={`dock-tab${view === v.id ? ' on' : ''}`} onClick={() => onView(v.id)}>
              <Icon name={v.icon} size={13} />
              {v.label}
            </button>
          ))}
        </div>
        <span className="dock-machine" title={`on ${machineLabel}`}>
          <Icon name="server" size={11} />
          {machineLabel}
        </span>
        <button type="button" className="dock-x" onClick={onClose} title="close panel">
          <Icon name="x" size={15} />
        </button>
      </div>
      <div className="dock-body">
        {/* keep each view mounted-but-hidden? no — terminals are heavy, so mount on demand */}
        {view === 'files' && <DockFiles agentId={session.agentId} start={cwd} />}
        {view === 'terminal' && (
          <XtermSession
            key={`term-${session.agentId}`}
            sessionId={`dock-term-${session.agentId}`}
            bare
            makeUrl={(c, r) => terminalWsUrl(session.agentId, c, r)}
            initialInput={cwd ? `${cdInto(cwd)} || echo '⚠ could not open the project dir on this machine'\n` : undefined}
          />
        )}
        {view === 'preview' && <DockPreview agentId={session.agentId} machineLabel={machineLabel} />}
        {view === 'git' && (
          <XtermSession
            key={`git-${session.agentId}-${session.projectPath}`}
            sessionId={`dock-git-${session.agentId}`}
            bare
            makeUrl={(c, r) => terminalWsUrl(session.agentId, c, r)}
            initialInput={
              cwd
                ? `if ${cdInto(cwd)}; then git --no-pager -c color.ui=always status && echo && git --no-pager -c color.ui=always diff --stat; else echo '⚠ could not open the project dir on this machine'; fi\n`
                : `git --no-pager -c color.ui=always status\n`
            }
          />
        )}
      </div>
    </div>
  )
}

// Minimal single-quote shell escaping for the initial `cd`.
function shq(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`
}

// Build a `cd` into the session's project that works on ANY machine. A project
// folder may be synced fleet-wide but each box's $HOME differs (/Users/alice vs
// /home/bob vs C:\Users\…), so the hub-side absolute path is wrong on a remote
// agent. Mirror the agent's resolveCwd: strip a leading per-user home prefix and
// rebase the remainder onto the shell's own $HOME; fall back to the literal path
// for anything else. No project root is hardcoded.
function cdInto(cwd: string): string {
  const rest = stripHomePrefix(cwd)
  if (rest !== null) return `cd "$HOME/${rest}"`
  return `cd ${shq(cwd)}`
}

// Detect a leading per-user home prefix in an absolute path and return the
// remainder (slash-normalised), so the caller can rebase it onto $HOME. Mirrors
// the agent's stripHomePrefix for /Users/<name>/, /home/<name>/, /root/, and
// Windows <drive>:\Users\<name>\. Returns null when no home prefix is present.
function stripHomePrefix(path: string): string | null {
  const norm = path.replace(/\\/g, '/')
  // Windows: <drive>:/Users/<name>/<rest>
  if (norm.length >= 3 && norm[1] === ':' && norm[2] === '/') {
    return afterSegments(norm.slice(2), 'Users')
  }
  return (
    afterSegments(norm, 'Users') ?? // /Users/<name>/<rest>
    afterSegments(norm, 'home') ?? // /home/<name>/<rest>
    (norm.startsWith('/root/') ? norm.slice('/root/'.length) : null) // /root/<rest>
  )
}

// Match "/<dir>/<name>/<rest>" and return "<rest>", requiring a non-empty name
// segment and a non-empty remainder so a bare "/Users/alice" isn't rebased.
function afterSegments(path: string, dir: string): string | null {
  const prefix = `/${dir}/`
  if (!path.startsWith(prefix)) return null
  const tail = path.slice(prefix.length) // "<name>/<rest>"
  const slash = tail.indexOf('/')
  if (slash <= 0 || slash === tail.length - 1) return null
  return tail.slice(slash + 1)
}

// ───────────────────────────── Files ─────────────────────────────
function DockFiles({ agentId, start }: { agentId: string; start: string }) {
  const [path, setPath] = useState(start)
  const [res, setRes] = useState<FileListResult | null>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [err, setErr] = useState('')

  // Reset to the session's directory when the active session changes.
  useEffect(() => setPath(start), [start, agentId])

  useEffect(() => {
    let cancelled = false
    setState('loading')
    fetchFiles(agentId, path)
      .then((r) => {
        if (cancelled) return
        if (r.error) {
          setState('error')
          setErr(r.error)
        } else {
          setRes(r)
          setState('ready')
          // Adopt the machine-resolved absolute path the agent returned (the hub
          // path we sent may have been re-rooted onto the remote $HOME, D23) so the
          // breadcrumb + subsequent navigation use this machine's real paths.
          if (r.path && r.path !== path) setPath(r.path)
        }
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setState('error')
        setErr(e instanceof Error ? e.message : 'failed to list')
      })
    return () => {
      cancelled = true
    }
  }, [agentId, path])

  const entries = useMemo(() => {
    const e = res?.entries ?? []
    return [...e].sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
      return a.name.localeCompare(b.name)
    })
  }, [res])

  const crumbs = useMemo(() => path.split('/').filter(Boolean), [path])

  return (
    <div className="dock-files">
      <div className="dock-crumbs term-scroll">
        <button type="button" className="dock-crumb" onClick={() => setPath('/')} title="root">/</button>
        {crumbs.map((c, i) => {
          const p = '/' + crumbs.slice(0, i + 1).join('/')
          return (
            <span key={p} style={{ display: 'inline-flex', alignItems: 'center' }}>
              <span className="dock-crumb-sep">/</span>
              <button type="button" className="dock-crumb" onClick={() => setPath(p)}>{c}</button>
            </span>
          )
        })}
      </div>
      <div className="dock-files-list term-scroll">
        {state === 'loading' && <div className="dock-files-msg">listing…</div>}
        {state === 'error' && (
          <div className="dock-files-msg err">
            {err}
            <button type="button" className="dock-files-home" onClick={() => setPath('')}>go to home</button>
          </div>
        )}
        {state === 'ready' && (
          <>
            {res?.parent != null && res.parent !== res.path && (
              <button type="button" className="dock-file" onClick={() => setPath(res.parent)}>
                <Icon name="chevron-right" size={14} style={{ transform: 'rotate(180deg)', opacity: 0.6 }} />
                <span className="dock-file-nm">..</span>
              </button>
            )}
            {entries.length === 0 && <div className="dock-files-msg">empty</div>}
            {entries.map((e) =>
              e.isDir ? (
                <button key={e.path} type="button" className="dock-file" onClick={() => setPath(e.path)}>
                  <Icon name="folder" size={14} color="var(--teal)" />
                  <span className="dock-file-nm">{e.name}</span>
                </button>
              ) : (
                <a key={e.path} className="dock-file" href={downloadUrl(agentId, e.path)} title="download" download>
                  <Icon name="file-code" size={14} color="var(--fg-3)" />
                  <span className="dock-file-nm">{e.name}</span>
                  <span className="dock-file-sz">{humanBytes(e.size)}</span>
                </a>
              ),
            )}
          </>
        )}
      </div>
    </div>
  )
}

// ───────────────────────────── Preview ─────────────────────────────
// Preview tunnels a dev server on the machine through the hub: the iframe points
// at /preview/{agentId}/{port}/ and the hub relays to 127.0.0.1:{port} on the
// machine over the existing yamux tunnel (D32). So it works for ANY localhost dev
// server, on any machine, from any device that can reach the hub — phone included
// — with zero config. No need to bind 0.0.0.0 or be on the same LAN.
function DockPreview({ agentId, machineLabel }: { agentId: string; machineLabel: string }) {
  const [draft, setDraft] = useState('3000')
  const [port, setPort] = useState('')
  const [nonce, setNonce] = useState(0)

  // The hub serves the dashboard, so a root-relative path is same-origin.
  const src = port ? `/preview/${encodeURIComponent(agentId)}/${port}/` : ''

  const go = () => {
    const p = draft.trim().replace(/^:/, '')
    if (!/^\d+$/.test(p)) return
    setPort(p)
    setNonce((n) => n + 1)
  }

  return (
    <div className="dock-preview">
      <form
        className="dock-preview-bar"
        onSubmit={(e) => {
          e.preventDefault()
          go()
        }}
      >
        <Icon name="monitor" size={13} color="var(--fg-3)" />
        <span className="dock-preview-host" title={`localhost on ${machineLabel}`}>{machineLabel}:</span>
        <input
          className="dock-preview-url"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="3000"
          inputMode="numeric"
          spellCheck={false}
          autoComplete="off"
        />
        {src && (
          <button type="button" className="dock-preview-btn" title="reload" onClick={() => setNonce((n) => n + 1)}>
            <Icon name="refresh-cw" size={13} />
          </button>
        )}
        {src && (
          <a className="dock-preview-btn" href={src} target="_blank" rel="noreferrer" title="open in new tab">
            <Icon name="maximize-2" size={13} />
          </a>
        )}
        <button type="submit" className="dock-preview-go">Go</button>
      </form>
      <div className="dock-preview-stage">
        {src ? (
          <iframe key={`${src}#${nonce}`} src={src} title="preview" className="dock-preview-frame" />
        ) : (
          <div className="dock-files-msg" style={{ padding: 24, textAlign: 'center', lineHeight: 1.6 }}>
            Enter the dev-server port to preview it here.
            <br />
            <span style={{ color: 'var(--fg-3)', fontSize: 11 }}>
              Any localhost port on {machineLabel} — tunneled through the hub, no LAN or 0.0.0.0 needed.
            </span>
          </div>
        )}
      </div>
    </div>
  )
}

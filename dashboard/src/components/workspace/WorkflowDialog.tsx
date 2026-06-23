import { useMemo, useState } from 'react'
import { createWorkflow } from '../../api'
import { CLAUDE_MODELS, DEFAULT_CLAUDE_MODEL, type Project, type WorkflowResult } from '../../types'
import type { LoadState } from '../../useWorkspace'
import { parseHubError } from '../../lib/hubError'
import { Modal } from '../Modal'
import { Icon } from '../../lattice/Icon'

// Workflow templates (E, v0.1.5): paste a GitHub issue or PR URL → Lattice classifies
// it (implement issue / review PR), picks the matching local project, and starts a
// scoped Claude session pre-briefed to do the work in a dedicated worktree
// (issue-<n> / review-<n>), auto-placed on the best machine.
//
// Kept self-contained (its own dialog, not folded into NewSessionDialog) so the
// spine stays mergeable: it shares only the model catalog + Modal.

// classify mirrors the hub's parseWorkflowURL so the dialog can preview the template
// and pre-pick the project from the repo name before submit.
function classify(url: string): { kind: 'issue' | 'pr'; owner: string; repo: string; number: string } | null {
  const m = url.trim().match(/^https:\/\/github\.com\/([\w.-]+)\/([\w.-]+)\/(issues|pull)\/(\d+)/)
  if (!m) return null
  return { kind: m[3] === 'pull' ? 'pr' : 'issue', owner: m[1], repo: m[2], number: m[4] }
}

export function WorkflowDialog({
  projects,
  projectsState,
  onClose,
  onCreated,
}: {
  projects: Project[]
  projectsState: LoadState
  onClose: () => void
  onCreated: (res: WorkflowResult) => void
}) {
  const [url, setUrl] = useState('')
  const [projectPath, setProjectPath] = useState('')
  const [model, setModel] = useState(DEFAULT_CLAUDE_MODEL)
  const [permissionMode, setPermissionMode] = useState('bypassPermissions')
  const [notifyOnIdle, setNotifyOnIdle] = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const parsed = useMemo(() => classify(url), [url])

  // Suggest the local project whose folder name matches the repo (a common 1:1).
  const suggested = useMemo(() => {
    if (!parsed) return null
    return projects.find((p) => p.name.toLowerCase() === parsed.repo.toLowerCase()) ?? null
  }, [parsed, projects])

  // Pre-select the suggestion the first time it appears, unless the user already chose.
  const effectivePath = projectPath || suggested?.path || ''

  const templateLabel = parsed
    ? parsed.kind === 'pr'
      ? `Review PR #${parsed.number}`
      : `Implement issue #${parsed.number}`
    : null

  const submit = async () => {
    if (!parsed || !effectivePath) return
    setCreating(true)
    setError(null)
    try {
      const res = await createWorkflow({
        url: url.trim(),
        projectPath: effectivePath,
        model,
        permissionMode,
        notifyOnIdle,
      })
      onCreated(res)
    } catch (e) {
      setError(parseHubError(e, 'failed to start workflow'))
      setCreating(false)
    }
  }

  return (
    <Modal width="wide" onClose={onClose} ariaLabel="Workflow">
      <h3>Implement issue / Review PR</h3>
      <div className="sub">
        Paste a GitHub issue or pull-request URL. Lattice starts a Claude session pre-briefed to do the work in a dedicated worktree, on the best machine.
      </div>

      <label className="flabel">GitHub URL</label>
      <input
        className="field mono"
        placeholder="https://github.com/owner/repo/issues/42"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        spellCheck={false}
        autoComplete="off"
        autoFocus
      />
      {url.trim() !== '' && !parsed && (
        <p style={{ margin: '6px 0 0', fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--st-orphaned)' }}>
          not a GitHub issue or pull-request URL
        </p>
      )}
      {templateLabel && (
        <div style={{ marginTop: 8, display: 'flex', alignItems: 'center', gap: 8 }}>
          <span className="chip cool">
            <Icon name="git-branch" size={12} /> {templateLabel}
          </span>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-3)' }}>
            worktree: {parsed!.kind === 'pr' ? 'review' : 'issue'}-{parsed!.number}
          </span>
        </div>
      )}

      {parsed && (
        <>
          <label className="flabel">Project (local clone)</label>
          <select
            className="field mono"
            value={effectivePath}
            onChange={(e) => setProjectPath(e.target.value)}
            style={{ cursor: 'pointer' }}
          >
            <option value="">{projectsState === 'loading' ? 'loading projects…' : 'select the local repo…'}</option>
            {[...projects]
              .sort((a, b) => a.name.localeCompare(b.name))
              .map((p) => (
                <option key={p.path} value={p.path}>
                  {p.name}
                </option>
              ))}
          </select>

          <label className="flabel">
            Model
            <span className="hint">which Claude model runs the workflow</span>
          </label>
          <select className="field mono" value={model} onChange={(e) => setModel(e.target.value)} style={{ cursor: 'pointer' }}>
            {CLAUDE_MODELS.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label}
                {m.legacy ? ' · Legacy' : ''}
              </option>
            ))}
          </select>

          <label className="flabel">
            Permissions
            <span className="hint">how much Claude asks before acting</span>
          </label>
          <select
            className="field mono"
            value={permissionMode}
            onChange={(e) => setPermissionMode(e.target.value)}
            style={{ cursor: 'pointer' }}
          >
            <option value="bypassPermissions">Bypass permissions</option>
            <option value="auto">Auto</option>
            <option value="acceptEdits">Accept edits</option>
            <option value="plan">Plan mode</option>
            <option value="default">Ask permissions</option>
          </select>

          <label
            style={{ marginTop: 12, display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 12, color: 'var(--fg-2)' }}
          >
            <input type="checkbox" checked={notifyOnIdle} onChange={(e) => setNotifyOnIdle(e.target.checked)} />
            Ping my phone when it waits or finishes
          </label>
        </>
      )}

      {error && (
        <div
          style={{
            marginTop: 12,
            padding: '10px 13px',
            borderRadius: 11,
            border: '1px solid color-mix(in oklch, var(--st-danger) 40%, transparent)',
            background: 'color-mix(in oklch, var(--st-danger) 8%, transparent)',
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--st-danger)',
          }}
        >
          {error}
        </div>
      )}

      <footer style={{ display: 'flex', alignItems: 'center', marginTop: 22, gap: 8 }}>
        <div style={{ flex: 1 }} />
        <button className="btn btn-secondary" type="button" onClick={onClose}>
          Cancel
        </button>
        <button
          className="btn btn-primary"
          type="button"
          onClick={submit}
          disabled={!parsed || !effectivePath || creating}
        >
          {creating ? 'Starting…' : !parsed ? 'Paste a URL' : !effectivePath ? 'Pick the repo' : 'Start workflow'}
        </button>
      </footer>
    </Modal>
  )
}

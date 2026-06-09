import { useEffect, useMemo, useRef, useState } from 'react'
import { createSession, fetchSettings, previewPlacement } from '../../api'
import { canHostClaude, CLAUDE_MODELS, DEFAULT_CLAUDE_MODEL, type Agent, type PlacementCandidate, type PlacementResult, type Project, type SessionKind, type SessionWithPlacement } from '../../types'
import type { LoadState } from '../../useWorkspace'
import { parseHubError } from '../../lib/hubError'
import { Modal } from '../Modal'
import { Icon } from '../../lattice/Icon'

// Either start a session inside a synced project (auto-placed across the mesh)
// or directly on one machine to do device-local work. The two flows share the
// kind/title controls but differ on placement: projects preview + override,
// devices are pinned by definition. A project target may carry a pre-selected
// project (launched from the sidebar) or null (launched from ⌘K / empty state) —
// when null the dialog opens on a searchable picker over every project.
export type NewSessionTarget =
  | { kind: 'project'; project: Project | null }
  | { kind: 'device'; agent: Agent }

interface Props {
  target: NewSessionTarget
  agents: Agent[]
  projects: Project[]
  projectsState: LoadState
  onClose: () => void
  onCreated: (res: SessionWithPlacement) => void
}

function agentLabel(agents: Agent[], id: string): string {
  const a = agents.find((x) => x.id === id)
  return a?.name || a?.hostname || id.slice(0, 8)
}

export function NewSessionDialog({ target, agents, projects, projectsState, onClose, onCreated }: Props) {
  // A session can run inside a synced PROJECT (auto-placed across the mesh) or
  // DEVICE-LOCAL on one machine. When the dialog is opened on a device we still
  // let the user flip to a project — so "select a project" is reachable from
  // whichever prompt they opened. Launched from a project there's no specific
  // device to pin, so the flip isn't offered (placement chooses the machine).
  const deviceAgent = target.kind === 'device' ? target.agent : null
  const initialProject = target.kind === 'project' ? target.project : null
  const [scope, setScope] = useState<'project' | 'device'>(target.kind)

  const scopeTabs = deviceAgent ? (
    <ScopeTabs scope={scope} onScope={setScope} deviceName={deviceAgent.hostname || deviceAgent.name || deviceAgent.id.slice(0, 8)} />
  ) : null

  if (scope === 'device' && deviceAgent) {
    return <DeviceSessionDialog agent={deviceAgent} scopeTabs={scopeTabs} onClose={onClose} onCreated={onCreated} />
  }
  return (
    <ProjectSessionDialog
      initialProject={initialProject}
      projects={projects}
      projectsState={projectsState}
      agents={agents}
      scopeTabs={scopeTabs}
      onClose={onClose}
      onCreated={onCreated}
    />
  )
}

// Segmented Project ⇄ This-device switch, shown when the dialog was opened on a
// device so the user can flip to a project picker (and back) without reopening.
function ScopeTabs({ scope, onScope, deviceName }: { scope: 'project' | 'device'; onScope: (s: 'project' | 'device') => void; deviceName: string }) {
  return (
    <div className="scope-tabs">
      <button type="button" className={`scope-tab${scope === 'project' ? ' on' : ''}`} onClick={() => onScope('project')}>
        <Icon name="folder" size={13} />
        Project
      </button>
      <button type="button" className={`scope-tab${scope === 'device' ? ' on' : ''}`} onClick={() => onScope('device')}>
        <Icon name="server" size={13} />
        {deviceName}
      </button>
    </div>
  )
}

// ───────────────────────────── project target ─────────────────────────────

function ProjectSessionDialog({
  initialProject,
  projects,
  projectsState,
  agents,
  scopeTabs,
  onClose,
  onCreated,
}: {
  initialProject: Project | null
  projects: Project[]
  projectsState: LoadState
  agents: Agent[]
  scopeTabs?: React.ReactNode
  onClose: () => void
  onCreated: (res: SessionWithPlacement) => void
}) {
  // The chosen project. null until picked — opens straight on the picker when
  // launched without a project (⌘K / empty state).
  const [project, setProject] = useState<Project | null>(initialProject)
  const [picking, setPicking] = useState(initialProject == null)
  const [kind, setKind] = useState<SessionKind>('claude')
  const [title, setTitle] = useState('')
  const [permissionMode, setPermissionMode] = useState('bypassPermissions')
  const [model, setModel] = useState(DEFAULT_CLAUDE_MODEL)
  const [fastMode, setFastMode] = useState(false)
  const [notifyOnIdle, setNotifyOnIdle] = useState(false)
  const [pinAgentId, setPinAgentId] = useState<string>('')
  const [preview, setPreview] = useState<PlacementResult | null>(null)
  const [previewState, setPreviewState] = useState<'idle' | 'loading' | 'error'>('idle')
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // D32: the primary coding machine (the Studio). The picker pre-selects it for
  // every project session so the 90% case is one click; a manual pick wins and
  // is never overridden once the user touches the list.
  const [primaryAgent, setPrimaryAgent] = useState<string>('')
  const userPinnedRef = useRef(false)

  // Capability gates — computed from agents list. Claude needs a box that can
  // actually host it (installed AND authable — F14), not merely have the binary.
  const claudeAvailable = agents.some((a) => a.online && canHostClaude(a))
  const editorAvailable = agents.some((a) => a.online && (a.capabilities?.codeServerInstalled ?? false))

  useEffect(() => {
    let cancelled = false
    fetchSettings()
      .then((s) => !cancelled && setPrimaryAgent(s.primaryAgent ?? ''))
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  const projectPath = project?.path
  useEffect(() => {
    if (!projectPath) {
      setPreview(null)
      setPreviewState('idle')
      return
    }
    let cancelled = false
    setPreviewState('loading')
    setError(null)
    previewPlacement({ kind, projectPath })
      .then((r) => {
        if (cancelled) return
        setPreview(r)
        setPreviewState('idle')
        // Pre-select the Studio when it can actually host this kind, unless the
        // user has already chosen a machine. Falls back to the server's `chosen`.
        if (!userPinnedRef.current) {
          const primaryEligible = r.candidates.some((c) => c.agentId === primaryAgent && c.eligible)
          setPinAgentId(primaryEligible ? primaryAgent : '')
        }
      })
      .catch(() => !cancelled && setPreviewState('error'))
    return () => {
      cancelled = true
    }
  }, [kind, projectPath, primaryAgent])

  // A manual pick (or unpin) freezes the default — the Studio won't re-assert
  // itself on the next preview refresh.
  const handlePin = (id: string) => {
    userPinnedRef.current = true
    setPinAgentId(id)
  }

  const noEligible = preview ? preview.candidates.every((c) => !c.eligible) : false

  const submit = async () => {
    if (!project) return
    setCreating(true)
    setError(null)
    try {
      const res = await createSession({
        kind,
        scope: 'project',
        projectPath: project.path,
        title: title.trim() || undefined,
        pinAgentId: pinAgentId || undefined,
        permissionMode: kind === 'claude' ? permissionMode : undefined,
        model: kind === 'claude' ? model : undefined,
        fastMode: kind === 'claude' ? fastMode : undefined,
        notifyOnIdle: kind === 'claude' ? notifyOnIdle : undefined,
      })
      onCreated(res)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to create session')
      setCreating(false)
    }
  }

  const titlePlaceholder = kind === 'claude' ? 'pair-on-mesh' : kind === 'editor' ? 'edit-mesh' : 'build-watcher'

  return (
    <Modal width="wide" onClose={onClose} ariaLabel="New session">
        <h3>New session</h3>
        {scopeTabs}
        <div className="sub">
          Lattice places it on the best machine in your mesh. It survives sleep and disconnects — reattach from any node.
        </div>

        <label className="flabel">Project</label>
        <ProjectField
          project={project}
          picking={picking}
          projects={projects}
          projectsState={projectsState}
          onPick={(p) => {
            setProject(p)
            setPicking(false)
          }}
          onTogglePicking={() => setPicking((v) => !v)}
        />

        {project && (
          <>
            <label className="flabel">Session type</label>
            <TypeGrid kind={kind} onChange={setKind} claudeDisabled={!claudeAvailable} editorDisabled={!editorAvailable} />

            <label className="flabel">Name</label>
            <input
              className="field mono"
              placeholder={titlePlaceholder}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
            />

            {kind === 'claude' && (
              <>
                <label className="flabel">
                  Model
                  <span className="hint">which Claude model this session runs</span>
                </label>
                <ModelSelect value={model} onChange={setModel} fastMode={fastMode} onFastMode={setFastMode} />
                <label className="flabel">
                  Permissions
                  <span className="hint">how much Claude asks before acting</span>
                </label>
                <PermissionModeSelect value={permissionMode} onChange={setPermissionMode} />
                <NotifyToggle value={notifyOnIdle} onChange={setNotifyOnIdle} />
              </>
            )}

            <label className="flabel">
              Place on
              <span className="hint">ranked by free RAM · load · locality</span>
            </label>
            <PlacementList
              state={previewState}
              preview={preview}
              agents={agents}
              pinAgentId={pinAgentId}
              onPin={handlePin}
            />
          </>
        )}

        {project && noEligible && (
          <div
            style={{
              marginTop: 12,
              padding: '10px 13px',
              borderRadius: 11,
              border: '1px solid color-mix(in oklch, var(--st-orphaned) 40%, transparent)',
              background: 'color-mix(in oklch, var(--st-orphaned) 8%, transparent)',
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              color: 'var(--st-orphaned)',
            }}
          >
            no eligible machine for a {kind} session —{' '}
            {kind === 'claude'
              ? 'no online agent has claude installed'
              : kind === 'editor'
                ? 'no online agent has code-server installed'
                : 'no online agents'}
          </div>
        )}
        {error && <ErrorBox text={error} />}

        <footer style={{ display: 'flex', alignItems: 'center', marginTop: 22, gap: 8 }}>
          <div style={{ flex: 1 }} />
          <button className="btn btn-secondary" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            type="button"
            onClick={submit}
            disabled={!project || creating || (noEligible && !pinAgentId)}
          >
            {creating ? 'Creating…' : !project ? 'Pick a project' : 'Create & open'}
          </button>
        </footer>
    </Modal>
  )
}

// ───────────────────────────── project picker ─────────────────────────────
// Picks the project the session runs in, from every folder the hub scans under
// the configured projects root (/api/projects). Pre-selected when launched from a
// project; a searchable list when launched cold (⌘K / empty state). Always
// changeable so you can retarget without reopening the dialog.
function ProjectField({
  project,
  picking,
  projects,
  projectsState,
  onPick,
  onTogglePicking,
}: {
  project: Project | null
  picking: boolean
  projects: Project[]
  projectsState: LoadState
  onPick: (p: Project) => void
  onTogglePicking: () => void
}) {
  const [query, setQuery] = useState('')
  const searchRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (picking) {
      setQuery('')
      const t = requestAnimationFrame(() => searchRef.current?.focus())
      return () => cancelAnimationFrame(t)
    }
  }, [picking])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = q ? projects.filter((p) => p.name.toLowerCase().includes(q) || p.path.toLowerCase().includes(q)) : projects
    return [...list].sort((a, b) => a.name.localeCompare(b.name))
  }, [projects, query])

  if (!picking && project) {
    return (
      <button type="button" className="proj-pick-current" onClick={onTogglePicking} title="change project">
        <Icon name="folder" size={15} color="var(--teal)" />
        <span className="proj-pick-nm">{project.name}</span>
        <span className="proj-pick-path">{project.path}</span>
        <span className="proj-pick-change">change</span>
      </button>
    )
  }

  return (
    <div className="proj-pick">
      <div className="proj-pick-search">
        <Icon name="search" size={14} color="var(--fg-3)" />
        <input
          ref={searchRef}
          className="proj-pick-input"
          placeholder="search projects…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          spellCheck={false}
          autoComplete="off"
        />
        {project && (
          <button type="button" className="proj-pick-cancel" onClick={onTogglePicking} title="keep current">
            <Icon name="x" size={13} />
          </button>
        )}
      </div>
      <div className="proj-pick-list term-scroll">
        {projectsState === 'loading' && <div className="proj-pick-msg">loading projects…</div>}
        {projectsState === 'error' && <div className="proj-pick-msg err">// projects unavailable</div>}
        {projectsState === 'ready' && filtered.length === 0 && (
          <div className="proj-pick-msg">{query ? `no projects match “${query.trim()}”` : '// no projects'}</div>
        )}
        {filtered.map((p) => (
          <button
            key={p.path}
            type="button"
            className={`proj-pick-row${project?.path === p.path ? ' on' : ''}`}
            onClick={() => onPick(p)}
          >
            <Icon name="folder" size={14} color={project?.path === p.path ? 'var(--teal)' : 'var(--fg-3)'} />
            <span className="proj-pick-nm">{p.name}</span>
            <span className="proj-pick-path">{p.path}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

// ───────────────────────────── device target ─────────────────────────────

function DeviceSessionDialog({
  agent,
  scopeTabs,
  onClose,
  onCreated,
}: {
  agent: Agent
  scopeTabs?: React.ReactNode
  onClose: () => void
  onCreated: (res: SessionWithPlacement) => void
}) {
  // Claude is offered on a device only when it can actually run there (installed AND
  // authable — F14). A box like the hub host has claude but can't sign in, so it
  // gets terminal/editor only; we tell the user which case it is.
  const claudeReady = canHostClaude(agent)
  const claudeInstalledButLocked = (agent.capabilities?.claudeInstalled ?? false) && !claudeReady
  const editorReady = agent.capabilities?.codeServerInstalled ?? false
  const [kind, setKind] = useState<SessionKind>(claudeReady ? 'claude' : 'terminal')
  const [title, setTitle] = useState('')
  const [permissionMode, setPermissionMode] = useState('bypassPermissions')
  const [model, setModel] = useState(DEFAULT_CLAUDE_MODEL)
  const [fastMode, setFastMode] = useState(false)
  const [notifyOnIdle, setNotifyOnIdle] = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const deviceName = agent.hostname || agent.name || agent.id.slice(0, 8)
  const titlePlaceholder = kind === 'claude' ? 'set-up-dev-tools' : kind === 'editor' ? 'edit-files' : 'organize-downloads'

  const submit = async () => {
    setCreating(true)
    setError(null)
    try {
      const res = await createSession({
        kind,
        scope: 'device',
        pinAgentId: agent.id,
        title: title.trim() || undefined,
        permissionMode: kind === 'claude' ? permissionMode : undefined,
        model: kind === 'claude' ? model : undefined,
        fastMode: kind === 'claude' ? fastMode : undefined,
        notifyOnIdle: kind === 'claude' ? notifyOnIdle : undefined,
      })
      onCreated(res)
    } catch (e) {
      setError(parseHubError(e, 'failed to create session'))
      setCreating(false)
    }
  }

  return (
    <Modal width="wide" onClose={onClose} ariaLabel="New session">
        <h3>New session</h3>
        {scopeTabs}
        <div className="sub">
          Device-local — runs in <span style={{ color: 'var(--fg-1)', fontFamily: 'var(--font-mono)' }}>{deviceName}</span>'s home directory.
        </div>

        <label className="flabel">Session type</label>
        <TypeGrid kind={kind} onChange={setKind} claudeDisabled={!claudeReady} editorDisabled={!editorReady} />

        {!claudeReady && (
          <p style={{ margin: '6px 0 0', fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--st-orphaned)' }}>
            {claudeInstalledButLocked
              ? "claude can't sign in on this device (background service) — terminal/editor only"
              : 'no claude on this device — terminal only'}
          </p>
        )}
        {!editorReady && (
          <p style={{ margin: '6px 0 0', fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--st-orphaned)' }}>
            code-server not installed — editor unavailable
          </p>
        )}

        <label className="flabel">Name</label>
        <input
          className="field mono"
          placeholder={titlePlaceholder}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          autoFocus
        />

        {kind === 'claude' && (
          <>
            <label className="flabel">
              Model
              <span className="hint">which Claude model this session runs</span>
            </label>
            <ModelSelect value={model} onChange={setModel} fastMode={fastMode} onFastMode={setFastMode} />
            <label className="flabel">
              Permissions
              <span className="hint">how much Claude asks before acting</span>
            </label>
            <PermissionModeSelect value={permissionMode} onChange={setPermissionMode} />
            <NotifyToggle value={notifyOnIdle} onChange={setNotifyOnIdle} />
          </>
        )}

        {/* Pinned machine row */}
        <div
          style={{
            marginTop: 12,
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '10px 14px',
            borderRadius: 13,
            border: '1px solid var(--border-alive)',
            background: 'color-mix(in oklch, var(--green) 6%, var(--void))',
            boxShadow: 'var(--glow-alive)',
          }}
        >
          <span className="dot live" />
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--fg-1)', fontWeight: 500 }}>
            {deviceName}
          </span>
          <span className="chip alive" style={{ marginLeft: 'auto' }}>Pinned</span>
        </div>

        {error && <ErrorBox text={error} />}

        <footer style={{ display: 'flex', alignItems: 'center', marginTop: 22, gap: 8 }}>
          <div style={{ flex: 1 }} />
          <button className="btn btn-secondary" type="button" onClick={onClose}>
            Cancel
          </button>
          <button className="btn btn-primary" type="button" onClick={submit} disabled={creating}>
            {creating ? 'Creating…' : 'Start session'}
          </button>
        </footer>
    </Modal>
  )
}

// ───────────────────────────── session type grid ─────────────────────────────

const SESSION_TYPES: { id: SessionKind; name: string; desc: string; icon: React.ReactNode }[] = [
  {
    id: 'terminal',
    name: 'Terminal',
    desc: 'A shell on the node',
    icon: (
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <polyline points="4 17 10 11 4 5" />
        <line x1="12" x2="20" y1="19" y2="19" />
      </svg>
    ),
  },
  {
    id: 'claude',
    name: 'Claude',
    desc: 'AI-paired session',
    icon: (
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z" />
        <path d="M20 3v4" /><path d="M22 5h-4" /><path d="M4 17v2" /><path d="M5 18H3" />
      </svg>
    ),
  },
  {
    id: 'editor',
    name: 'Editor',
    desc: 'VS Code + terminal',
    icon: (
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M10 12.5 8 15l2 2.5" /><path d="m14 12.5 2 2.5-2 2.5" />
        <path d="M14 2v4a2 2 0 0 0 2 2h4" />
        <path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7z" />
      </svg>
    ),
  },
]

function TypeGrid({
  kind,
  onChange,
  claudeDisabled = false,
  editorDisabled = false,
}: {
  kind: SessionKind
  onChange: (k: SessionKind) => void
  claudeDisabled?: boolean
  editorDisabled?: boolean
}) {
  return (
    <div className="type-grid">
      {SESSION_TYPES.map((t) => {
        const disabled = (t.id === 'claude' && claudeDisabled) || (t.id === 'editor' && editorDisabled)
        return (
          <button
            key={t.id}
            type="button"
            disabled={disabled}
            onClick={() => !disabled && onChange(t.id)}
            className={`type-opt${kind === t.id ? ' on' : ''}`}
            style={disabled ? { opacity: 0.4, cursor: 'not-allowed' } : undefined}
            title={
              disabled
                ? t.id === 'claude'
                  ? 'no claude installed on any online agent'
                  : 'code-server not installed on any online agent'
                : undefined
            }
          >
            <span className="ti">{t.icon}</span>
            <span className="tn">{t.name}</span>
            <span className="td">{t.desc}</span>
          </button>
        )
      })}
    </div>
  )
}

// ───────────────────────────── placement rank list ─────────────────────────────

function agentStats(agents: Agent[], agentId: string) {
  const a = agents.find((x) => x.id === agentId)
  if (!a) return null
  const freeGb = a.memTotal > 0 ? ((a.memTotal * (1 - a.memUsedPct / 100)) / 1073741824).toFixed(0) : null
  const loadPct = a.loadAvg1 != null ? `${Math.round(a.loadAvg1 * 10)}%` : null
  const cores = a.cpuCount != null ? `${a.cpuCount}c` : null
  const locality = a.local ? 'this mac' : 'remote'
  return { freeGb, loadPct, cores, locality, online: a.online }
}

function rankDotClass(eligible: boolean, online: boolean): string {
  if (!online) return 'dot'
  return eligible ? 'dot live' : 'dot idle'
}

function PlacementList({
  state,
  preview,
  agents,
  pinAgentId,
  onPin,
}: {
  state: 'idle' | 'loading' | 'error'
  preview: PlacementResult | null
  agents: Agent[]
  pinAgentId: string
  onPin: (id: string) => void
}) {
  if (state === 'loading') {
    return (
      <p style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-3)', margin: 0 }}>
        scoring machines…
      </p>
    )
  }
  if (state === 'error') {
    return (
      <p style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--st-danger)', margin: 0 }}>
        placement preview unavailable
      </p>
    )
  }
  if (!preview || preview.candidates.length === 0) {
    return (
      <p style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-3)', margin: 0 }}>
        no candidates reported
      </p>
    )
  }

  // Sort eligible first by score desc, then ineligible
  const sorted = [...preview.candidates].sort((a, b) => {
    if (a.eligible !== b.eligible) return a.eligible ? -1 : 1
    return b.score - a.score
  })

  return (
    <div className="rank-list">
      {sorted.map((c, i) => {
        const isChosen = pinAgentId ? c.agentId === pinAgentId : c.agentId === preview.chosen
        const isBest = !pinAgentId && i === 0 && c.eligible
        const isSelected = !!pinAgentId && c.agentId === pinAgentId
        const agentInfo = agentStats(agents, c.agentId)
        const online = agentInfo?.online ?? c.eligible
        const score = Math.round(c.score)
        const badgeLabel = c.eligible ? String(i + 1) : '—'

        return (
          <RankRow
            key={c.agentId}
            candidate={c}
            label={agentLabel(agents, c.agentId)}
            badgeLabel={badgeLabel}
            isChosen={isChosen}
            isBest={isBest}
            isSelected={isSelected}
            online={online}
            score={score}
            agentInfo={agentInfo}
            onPin={() => c.eligible && onPin(pinAgentId === c.agentId ? '' : c.agentId)}
          />
        )
      })}
    </div>
  )
}

interface RankRowProps {
  candidate: PlacementCandidate
  label: string
  badgeLabel: string
  isChosen: boolean
  isBest: boolean
  isSelected: boolean
  online: boolean
  score: number
  agentInfo: ReturnType<typeof agentStats>
  onPin: () => void
}

function RankRow({ candidate, label, badgeLabel, isChosen, isBest, isSelected, online, score, agentInfo, onPin }: RankRowProps) {
  const cls = `rank${isChosen ? ' on' : ''}${!candidate.eligible ? ' dis' : ''}`
  const dotCls = rankDotClass(candidate.eligible, online)
  const barWidth = candidate.eligible ? Math.min(100, Math.max(0, score)) : 0

  // Build exclusion reason string from the candidate data
  const exclusionText = !candidate.eligible
    ? (candidate.excluded ?? 'excluded')
    : null

  return (
    <div
      className={cls}
      onClick={onPin}
      role="button"
      tabIndex={candidate.eligible ? 0 : -1}
      onKeyDown={(e) => e.key === 'Enter' && onPin()}
    >
      {/* Left: rank badge */}
      <span className="badge">{badgeLabel}</span>

      {/* Center: name + stats */}
      <div>
        <div className="id-line">
          <span className={dotCls} />
          <span className="nm">{label}</span>
          {isBest && <span className="chip alive">Best fit</span>}
          {isSelected && !isBest && <span className="chip cool">Selected</span>}
        </div>

        {!candidate.eligible ? (
          <div className="stats">
            <div className="stat">
              <span className="v" style={{ color: 'var(--fg-3)' }}>
                {online ? exclusionText : 'Offline · wake in Fleet to place here'}
              </span>
            </div>
          </div>
        ) : (
          <div className="stats">
            <div className="stat">
              <span className="k">Free RAM</span>
              <span className={`v${agentInfo?.freeGb ? ' good' : ''}`}>
                {agentInfo?.freeGb != null ? `${agentInfo.freeGb} GB` : '—'}
              </span>
            </div>
            <div className="stat">
              <span className="k">Load</span>
              <span className="v">
                {agentInfo?.loadPct != null && agentInfo?.cores != null
                  ? `${agentInfo.loadPct} · ${agentInfo.cores}`
                  : '—'}
              </span>
            </div>
            <div className="stat">
              <span className="k">Locality</span>
              <span className="v">{agentInfo?.locality ?? '—'}</span>
            </div>
          </div>
        )}
      </div>

      {/* Right: fit score + bar */}
      <div className="fit">
        <span className="score">{candidate.eligible ? score : '—'}</span>
        <div className="fitbar">
          <i style={{ width: `${barWidth}%` }} />
        </div>
      </div>
    </div>
  )
}

// ───────────────────────────── permission mode selector ─────────────────────────────

const PERMISSION_MODES: { value: string; label: string }[] = [
  { value: 'bypassPermissions', label: 'Bypass permissions' },
  { value: 'auto', label: 'Auto' },
  { value: 'acceptEdits', label: 'Accept edits' },
  { value: 'plan', label: 'Plan mode' },
  { value: 'default', label: 'Ask permissions' },
]

// Claude model picker: a dropdown over the catalog (default pre-selected) plus a
// "fast mode" toggle that maps to claude's low-effort setting at launch. Threaded
// through SessionCreatePayload → claudeCommand `--model <id>` / `--effort low`.
function ModelSelect({
  value,
  onChange,
  fastMode,
  onFastMode,
}: {
  value: string
  onChange: (v: string) => void
  fastMode: boolean
  onFastMode: (v: boolean) => void
}) {
  return (
    <>
      <select
        className="field mono"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        style={{ cursor: 'pointer' }}
      >
        {CLAUDE_MODELS.map((m) => (
          <option key={m.id} value={m.id}>
            {m.label}
            {m.legacy ? ' · Legacy' : ''}
          </option>
        ))}
      </select>
      <button
        type="button"
        role="switch"
        aria-checked={fastMode}
        onClick={() => onFastMode(!fastMode)}
        className="notify-toggle"
        style={{
          marginTop: 10,
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          gap: 11,
          padding: '11px 14px',
          borderRadius: 13,
          cursor: 'pointer',
          textAlign: 'left',
          border: `1px solid ${fastMode ? 'var(--border-alive)' : 'var(--border)'}`,
          background: fastMode ? 'color-mix(in oklch, var(--teal) 8%, var(--void))' : 'transparent',
          boxShadow: fastMode ? 'var(--glow-alive)' : 'none',
          transition: 'background .15s, border-color .15s',
        }}
      >
        <Icon name="zap" size={15} color={fastMode ? 'var(--teal)' : 'var(--fg-3)'} />
        <span style={{ flex: 1 }}>
          <span style={{ display: 'block', fontSize: 13, color: 'var(--fg-1)', fontWeight: 500 }}>Fast mode</span>
          <span style={{ display: 'block', fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-3)', marginTop: 1 }}>
            lower effort, snappier replies
          </span>
        </span>
        <span
          aria-hidden
          style={{
            flexShrink: 0,
            width: 34,
            height: 20,
            borderRadius: 999,
            background: fastMode ? 'var(--teal)' : 'color-mix(in oklch, var(--fg-3) 30%, transparent)',
            position: 'relative',
            transition: 'background .15s',
          }}
        >
          <span
            style={{
              position: 'absolute',
              top: 2,
              left: fastMode ? 16 : 2,
              width: 16,
              height: 16,
              borderRadius: '50%',
              background: 'var(--void)',
              transition: 'left .15s',
            }}
          />
        </span>
      </button>
    </>
  )
}

function PermissionModeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="perm-seg">
      {PERMISSION_MODES.map((m) => (
        <button
          key={m.value}
          type="button"
          className={`perm-seg-opt${value === m.value ? ' on' : ''}`}
          onClick={() => onChange(m.value)}
        >
          {m.label}
        </button>
      ))}
    </div>
  )
}

// Fire-and-forget opt-in: when armed, the hub pings your phone (ntfy) the moment
// this Claude run goes quiet waiting on you, or finishes — and the push carries
// Approve / Deny buttons so you can answer a prompt without opening the laptop.
function NotifyToggle({ value, onChange }: { value: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={value}
      onClick={() => onChange(!value)}
      className="notify-toggle"
      style={{
        marginTop: 10,
        width: '100%',
        display: 'flex',
        alignItems: 'center',
        gap: 11,
        padding: '11px 14px',
        borderRadius: 13,
        cursor: 'pointer',
        textAlign: 'left',
        border: `1px solid ${value ? 'var(--border-alive)' : 'var(--border)'}`,
        background: value ? 'color-mix(in oklch, var(--teal) 8%, var(--void))' : 'transparent',
        boxShadow: value ? 'var(--glow-alive)' : 'none',
        transition: 'background .15s, border-color .15s',
      }}
    >
      <Icon name="smartphone" size={15} color={value ? 'var(--teal)' : 'var(--fg-3)'} />
      <span style={{ flex: 1 }}>
        <span style={{ display: 'block', fontSize: 13, color: 'var(--fg-1)', fontWeight: 500 }}>Ping my phone</span>
        <span style={{ display: 'block', fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-3)', marginTop: 1 }}>
          notify + approve from anywhere when it waits or finishes
        </span>
      </span>
      <span
        aria-hidden
        style={{
          flexShrink: 0,
          width: 34,
          height: 20,
          borderRadius: 999,
          background: value ? 'var(--teal)' : 'color-mix(in oklch, var(--fg-3) 30%, transparent)',
          position: 'relative',
          transition: 'background .15s',
        }}
      >
        <span
          style={{
            position: 'absolute',
            top: 2,
            left: value ? 16 : 2,
            width: 16,
            height: 16,
            borderRadius: '50%',
            background: 'var(--void)',
            transition: 'left .15s',
          }}
        />
      </span>
    </button>
  )
}

// ───────────────────────────── shared pieces ─────────────────────────────

function ErrorBox({ text }: { text: string }) {
  return (
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
      {text}
    </div>
  )
}

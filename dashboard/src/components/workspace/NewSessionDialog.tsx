import { useEffect, useRef, useState } from 'react'
import { createSession, fetchSettings, previewPlacement } from '../../api'
import type { Agent, PlacementCandidate, PlacementResult, Project, SessionKind, SessionWithPlacement } from '../../types'

// Either start a session inside a synced project (auto-placed across the mesh)
// or directly on one machine to do device-local work. The two flows share the
// kind/title controls but differ on placement: projects preview + override,
// devices are pinned by definition.
export type NewSessionTarget =
  | { kind: 'project'; project: Project }
  | { kind: 'device'; agent: Agent }

interface Props {
  target: NewSessionTarget
  agents: Agent[]
  onClose: () => void
  onCreated: (res: SessionWithPlacement) => void
}

function agentLabel(agents: Agent[], id: string): string {
  const a = agents.find((x) => x.id === id)
  return a?.name || a?.hostname || id.slice(0, 8)
}

export function NewSessionDialog({ target, agents, onClose, onCreated }: Props) {
  if (target.kind === 'device') {
    return <DeviceSessionDialog agent={target.agent} onClose={onClose} onCreated={onCreated} />
  }
  return <ProjectSessionDialog project={target.project} agents={agents} onClose={onClose} onCreated={onCreated} />
}

// ───────────────────────────── project target ─────────────────────────────

function ProjectSessionDialog({
  project,
  agents,
  onClose,
  onCreated,
}: {
  project: Project
  agents: Agent[]
  onClose: () => void
  onCreated: (res: SessionWithPlacement) => void
}) {
  const [kind, setKind] = useState<SessionKind>('claude')
  const [title, setTitle] = useState('')
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

  // Capability gates — computed from agents list
  const claudeAvailable = agents.some((a) => a.online && (a.capabilities?.claudeInstalled ?? false))
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

  useEffect(() => {
    let cancelled = false
    setPreviewState('loading')
    setError(null)
    previewPlacement({ kind, projectPath: project.path })
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
  }, [kind, project.path, primaryAgent])

  // A manual pick (or unpin) freezes the default — the Studio won't re-assert
  // itself on the next preview refresh.
  const handlePin = (id: string) => {
    userPinnedRef.current = true
    setPinAgentId(id)
  }

  const noEligible = preview ? preview.candidates.every((c) => !c.eligible) : false

  const submit = async () => {
    setCreating(true)
    setError(null)
    try {
      const res = await createSession({
        kind,
        scope: 'project',
        projectPath: project.path,
        title: title.trim() || undefined,
        pinAgentId: pinAgentId || undefined,
      })
      onCreated(res)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to create session')
      setCreating(false)
    }
  }

  const titlePlaceholder = kind === 'claude' ? 'pair-on-mesh' : kind === 'editor' ? 'edit-mesh' : 'build-watcher'

  return (
    <div className="scrim" onClick={onClose}>
      <div className="modal wide" onClick={(e) => e.stopPropagation()}>
        <h3>New session</h3>
        <div className="sub">
          Lattice places it on the best machine in your mesh. It survives sleep and disconnects — reattach from any node.
        </div>

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

        {noEligible && (
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
            disabled={creating || (noEligible && !pinAgentId)}
          >
            {creating ? 'Creating…' : 'Create & open'}
          </button>
        </footer>
      </div>
    </div>
  )
}

// ───────────────────────────── device target ─────────────────────────────

function DeviceSessionDialog({
  agent,
  onClose,
  onCreated,
}: {
  agent: Agent
  onClose: () => void
  onCreated: (res: SessionWithPlacement) => void
}) {
  const claudeReady = agent.capabilities?.claudeInstalled ?? false
  const editorReady = agent.capabilities?.codeServerInstalled ?? false
  const [kind, setKind] = useState<SessionKind>(claudeReady ? 'claude' : 'terminal')
  const [title, setTitle] = useState('')
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
      })
      onCreated(res)
    } catch (e) {
      setError(parseHostError(e))
      setCreating(false)
    }
  }

  return (
    <div className="scrim" onClick={onClose}>
      <div className="modal wide" onClick={(e) => e.stopPropagation()}>
        <h3>New session</h3>
        <div className="sub">
          Device-local — runs in <span style={{ color: 'var(--fg-1)', fontFamily: 'var(--font-mono)' }}>{deviceName}</span>'s home directory.
        </div>

        <label className="flabel">Session type</label>
        <TypeGrid kind={kind} onChange={setKind} claudeDisabled={!claudeReady} editorDisabled={!editorReady} />

        {!claudeReady && (
          <p style={{ margin: '6px 0 0', fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--st-orphaned)' }}>
            no claude on this device — terminal only
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
      </div>
    </div>
  )
}

function parseHostError(e: unknown): string {
  const raw = e instanceof Error ? e.message : 'failed to create session'
  const idx = raw.indexOf('{')
  if (idx !== -1) {
    try {
      const parsed = JSON.parse(raw.slice(idx)) as { error?: string }
      if (parsed.error) return parsed.error
    } catch {
      /* fall through to raw */
    }
  }
  return raw
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

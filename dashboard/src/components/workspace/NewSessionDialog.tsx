import { useEffect, useState } from 'react'
import { createSession, previewPlacement } from '../../api'
import type { Agent, PlacementResult, Project, SessionKind, SessionWithPlacement } from '../../types'

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

  useEffect(() => {
    let cancelled = false
    setPreviewState('loading')
    setError(null)
    previewPlacement({ kind, projectPath: project.path })
      .then((r) => {
        if (cancelled) return
        setPreview(r)
        setPreviewState('idle')
      })
      .catch(() => !cancelled && setPreviewState('error'))
    return () => {
      cancelled = true
    }
  }, [kind, project.path])

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

  return (
    <Shell title="New session" subtitle={project.name} onClose={onClose}>
      <div className="space-y-4 px-5 py-4">
        <Field label="kind">
          <KindPicker kind={kind} onChange={setKind} />
        </Field>

        <Field label="title (optional)">
          <TitleInput
            value={title}
            onChange={setTitle}
            placeholder={kind === 'claude' ? 'e.g. fix the placement bug' : 'e.g. build + test'}
          />
        </Field>

        <Field label="placement">
          <PlacementPreview state={previewState} preview={preview} agents={agents} pinAgentId={pinAgentId} onPin={setPinAgentId} />
        </Field>

        {noEligible && (
          <div className="rounded-md border border-orange-500/40 bg-orange-500/[0.07] px-3 py-2 font-mono text-[11px] text-orange-300">
            no eligible machine for a {kind} session —{' '}
            {kind === 'claude'
              ? 'no online agent has claude installed'
              : kind === 'editor'
                ? 'no online agent has code-server installed'
                : 'no online agents'}
          </div>
        )}
        {error && <ErrorBox text={error} />}
      </div>

      <Footer
        onClose={onClose}
        onSubmit={submit}
        creating={creating}
        disabled={creating || (noEligible && !pinAgentId)}
      />
    </Shell>
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
      // The hub returns a 400 with a JSON {error} body when the box can't host
      // (e.g. claude not installed). Surface the message text inline.
      setError(parseHostError(e))
      setCreating(false)
    }
  }

  return (
    <Shell title={`New session on ${deviceName}`} subtitle="device-local — runs in this machine's home dir" onClose={onClose}>
      <div className="space-y-4 px-5 py-4">
        <Field label="kind">
          <KindPicker kind={kind} onChange={setKind} claudeDisabled={!claudeReady} editorDisabled={!editorReady} />
          {!claudeReady && (
            <p className="mt-1.5 font-mono text-[10px] text-orange-400/80">no claude on this device — terminal only</p>
          )}
          {!editorReady && (
            <p className="mt-1.5 font-mono text-[10px] text-orange-400/80">code-server not installed on this device — editor unavailable</p>
          )}
        </Field>

        <Field label="title (optional)">
          <TitleInput
            value={title}
            onChange={setTitle}
            placeholder={kind === 'claude' ? 'e.g. set up dev tools' : 'e.g. organize ~/Downloads'}
          />
        </Field>

        <div className="flex items-center gap-2 rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2 font-mono text-[11px] text-zinc-400">
          <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
          pinned to <span className="text-zinc-200">{deviceName}</span>
        </div>

        {error && <ErrorBox text={error} />}
      </div>

      <Footer onClose={onClose} onSubmit={submit} creating={creating} disabled={creating} />
    </Shell>
  )
}

function parseHostError(e: unknown): string {
  const raw = e instanceof Error ? e.message : 'failed to create session'
  // api.json() throws `${status}: ${body}` where body may be JSON {error}.
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

// ───────────────────────────── shared pieces ─────────────────────────────

function Shell({
  title,
  subtitle,
  onClose,
  children,
}: {
  title: string
  subtitle: string
  onClose: () => void
  children: React.ReactNode
}) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="w-full max-w-md rounded-xl border border-zinc-800 bg-zinc-900 shadow-[0_20px_60px_-20px_rgba(0,0,0,0.8)] animate-risein"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="border-b border-zinc-800 px-5 py-4">
          <h3 className="font-display text-base font-semibold text-zinc-50">{title}</h3>
          <p className="mt-0.5 truncate font-mono text-[11px] text-zinc-500">{subtitle}</p>
        </header>
        {children}
      </div>
    </div>
  )
}

function kindLabel(k: SessionKind): string {
  switch (k) {
    case 'editor':
      return '</> editor'
    default:
      return k
  }
}

function KindPicker({
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
    <div className="inline-flex rounded-md border border-zinc-800 bg-zinc-950 p-0.5">
      {(['claude', 'terminal', 'editor'] as SessionKind[]).map((k) => {
        const disabled = (k === 'claude' && claudeDisabled) || (k === 'editor' && editorDisabled)
        const disabledTitle =
          k === 'claude' ? 'no claude on this device' : k === 'editor' ? 'code-server not installed on this device' : undefined
        return (
          <button
            key={k}
            type="button"
            disabled={disabled}
            onClick={() => onChange(k)}
            title={disabled ? disabledTitle : undefined}
            className={`rounded px-4 py-1.5 font-display text-xs font-semibold uppercase tracking-wider transition-colors ${
              kind === k ? 'bg-emerald-500/15 text-emerald-300' : 'text-zinc-500 hover:text-zinc-300'
            } ${disabled ? 'cursor-not-allowed opacity-40 hover:text-zinc-500' : ''}`}
          >
            {kindLabel(k)}
          </button>
        )
      })}
    </div>
  )
}

function TitleInput({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
}) {
  return (
    <input
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:border-emerald-500/60 focus:outline-none"
    />
  )
}

function ErrorBox({ text }: { text: string }) {
  return (
    <div className="rounded-md border border-red-500/40 bg-red-500/[0.07] px-3 py-2 font-mono text-[11px] text-red-300">
      {text}
    </div>
  )
}

function Footer({
  onClose,
  onSubmit,
  creating,
  disabled,
}: {
  onClose: () => void
  onSubmit: () => void
  creating: boolean
  disabled: boolean
}) {
  return (
    <footer className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3.5">
      <button
        type="button"
        onClick={onClose}
        className="rounded-md px-3 py-1.5 font-display text-sm text-zinc-400 transition-colors hover:text-zinc-200"
      >
        Cancel
      </button>
      <button
        type="button"
        onClick={onSubmit}
        disabled={disabled}
        className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-50"
      >
        {creating ? 'creating…' : 'Create & open'}
      </button>
    </footer>
  )
}

function PlacementPreview({
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
  if (state === 'loading') return <p className="font-mono text-[11px] text-zinc-600">scoring machines…</p>
  if (state === 'error') return <p className="font-mono text-[11px] text-red-400">placement preview unavailable</p>
  if (!preview || preview.candidates.length === 0)
    return <p className="font-mono text-[11px] text-zinc-600">no candidates reported</p>

  return (
    <div className="overflow-hidden rounded-md border border-zinc-800">
      {preview.candidates.map((c) => {
        const chosen = pinAgentId ? c.agentId === pinAgentId : c.agentId === preview.chosen
        return (
          <button
            key={c.agentId}
            type="button"
            disabled={!c.eligible}
            onClick={() => onPin(pinAgentId === c.agentId ? '' : c.agentId)}
            className={`flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors ${
              chosen ? 'bg-emerald-500/10' : 'hover:bg-zinc-900'
            } ${c.eligible ? '' : 'opacity-50'} border-b border-zinc-800/70 last:border-0`}
          >
            <span className={`h-1.5 w-1.5 rounded-full ${c.eligible ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
            <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-zinc-200">
              {agentLabel(agents, c.agentId)}
            </span>
            {pinAgentId === c.agentId && <span className="font-mono text-[10px] text-emerald-400">pinned</span>}
            {!pinAgentId && c.agentId === preview.chosen && (
              <span className="font-mono text-[10px] text-emerald-400/70">auto</span>
            )}
            {c.eligible ? (
              <span className="font-mono text-[12px] tabular-nums text-emerald-400">{c.score.toFixed(1)}</span>
            ) : (
              <span className="font-mono text-[10px] text-orange-400/80">{c.excluded ?? 'excluded'}</span>
            )}
          </button>
        )
      })}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1.5 block font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">{label}</label>
      {children}
    </div>
  )
}

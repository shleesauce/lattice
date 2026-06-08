import { useEffect, useMemo, useState } from 'react'
import { createProject } from '../../api'
import { parseHubError } from '../../lib/hubError'
import type { CreateProjectEnvVar, CreateProjectResult, Project } from '../../types'

interface Props {
  projects: Project[]
  onClose: () => void
  onCreated: (res: CreateProjectResult) => void
}

const STEPS = ['identity', 'shape', 'links', 'review'] as const
type Step = (typeof STEPS)[number]

const FOLDER_RE = /^[a-z0-9][a-z0-9-]*$/
const CONNECTOR_SUGGESTIONS = ['Airtable', 'Supabase', 'Slack', 'Vercel', 'Gmail', 'Calendar', 'ClickUp']
// Curated starting points for Step 2. Multi-select — real stacks combine (e.g.
// React/Vite + Tailwind + Supabase). Anything not here goes in "Other".
const STACK_PRESETS = [
  'React/Vite',
  'Next.js',
  'Electron',
  'Node',
  'Go',
  'Python',
  'Tailwind',
  'Supabase',
  'Vite',
  'Astro',
]

function kebab(s: string): string {
  return s
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

// Guided onboarding for a brand-new project. Four steps, hub scaffolds + (opt)
// registers in the project registry and returns a Session that opens like any other.
export function NewProjectWizard({ projects, onClose, onCreated }: Props) {
  const [step, setStep] = useState<Step>('identity')
  const [officialName, setOfficialName] = useState('')
  const [folderName, setFolderName] = useState('')
  const [folderTouched, setFolderTouched] = useState(false)
  const [description, setDescription] = useState('')
  const [stackPresets, setStackPresets] = useState<string[]>([])
  const [stackOther, setStackOther] = useState('')
  const [port, setPort] = useState('')
  const [connectors, setConnectors] = useState<string[]>([])
  const [agents, setAgents] = useState<string[]>([])
  const [relatedProjects, setRelatedProjects] = useState<string[]>([])
  const [envVars, setEnvVars] = useState<CreateProjectEnvVar[]>([])
  const [register, setRegister] = useState(true)
  const [launchClaude, setLaunchClaude] = useState(true)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<CreateProjectResult | null>(null)

  // Auto-suggest the folder name from the official name until the user edits it.
  useEffect(() => {
    if (!folderTouched) setFolderName(kebab(officialName))
  }, [officialName, folderTouched])

  const folderError = useMemo(() => {
    const v = folderName.trim()
    if (!v) return null
    if (!FOLDER_RE.test(v)) return 'lowercase letters, numbers and dashes only'
    if (projects.some((p) => p.name.toLowerCase() === v.toLowerCase())) return 'already exists'
    return null
  }, [folderName, projects])

  // The stack is built from the chosen presets plus an optional free-text "Other"
  // (versions/anything goes there) — joined into the single string the hub stores.
  const stack = useMemo(
    () => [...stackPresets, stackOther.trim()].filter(Boolean).join(', '),
    [stackPresets, stackOther],
  )

  const identityValid =
    officialName.trim().length > 0 && folderName.trim().length > 0 && !folderError && description.trim().length > 0

  const idx = STEPS.indexOf(step)
  const canAdvance = step === 'identity' ? identityValid : true

  const goNext = () => {
    if (idx < STEPS.length - 1 && canAdvance) setStep(STEPS[idx + 1])
  }
  const goBack = () => {
    if (idx > 0) setStep(STEPS[idx - 1])
  }

  // Esc closes; Enter advances on non-review steps (but not while typing in a textarea).
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      onClose()
      return
    }
    if (e.key === 'Enter' && step !== 'review' && !(e.target instanceof HTMLTextAreaElement)) {
      e.preventDefault()
      goNext()
    }
  }

  const submit = async () => {
    setCreating(true)
    setError(null)
    try {
      const portNum = port.trim() ? Number(port.trim()) : undefined
      const res = await createProject({
        officialName: officialName.trim(),
        folderName: folderName.trim(),
        description: description.trim(),
        stack: stack.trim() || undefined,
        port: Number.isFinite(portNum) ? portNum : undefined,
        connectors: connectors.length ? connectors : undefined,
        agents: agents.length ? agents : undefined,
        relatedProjects: relatedProjects.length ? relatedProjects : undefined,
        envVars: envVars.filter((v) => v.key.trim()).length
          ? envVars.filter((v) => v.key.trim())
          : undefined,
        register,
        launchClaude,
      })
      setResult(res)
      // Hand off to the workspace: refresh projects + open the session if any.
      onCreated(res)
    } catch (e) {
      setError(parseHubError(e, 'failed to create project'))
      setCreating(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4" onClick={onClose} onKeyDown={onKeyDown}>
      <div
        className="flex max-h-[88vh] w-full max-w-lg flex-col rounded-xl border border-zinc-800 bg-zinc-900 shadow-[0_20px_60px_-20px_rgba(0,0,0,0.8)] animate-risein"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="border-b border-zinc-800 px-5 py-4">
          <div className="flex items-baseline justify-between">
            <h3 className="font-display text-base font-semibold text-zinc-50">Begin new project</h3>
            <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-600">
              step {idx + 1} / {STEPS.length}
            </span>
          </div>
          <Stepper current={idx} done={!!result} />
        </header>

        <div className="term-scroll min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {result ? (
            <ResultView result={result} />
          ) : step === 'identity' ? (
            <IdentityStep
              officialName={officialName}
              setOfficialName={setOfficialName}
              folderName={folderName}
              onFolderChange={(v) => {
                setFolderTouched(true)
                setFolderName(v)
              }}
              folderError={folderError}
              description={description}
              setDescription={setDescription}
            />
          ) : step === 'shape' ? (
            <ShapeStep
              stackPresets={stackPresets}
              setStackPresets={setStackPresets}
              stackOther={stackOther}
              setStackOther={setStackOther}
              port={port}
              setPort={setPort}
            />
          ) : step === 'links' ? (
            <LinksStep
              connectors={connectors}
              setConnectors={setConnectors}
              agents={agents}
              setAgents={setAgents}
              relatedProjects={relatedProjects}
              setRelatedProjects={setRelatedProjects}
              projects={projects}
              envVars={envVars}
              setEnvVars={setEnvVars}
            />
          ) : (
            <ReviewStep
              officialName={officialName}
              folderName={folderName}
              description={description}
              stack={stack}
              port={port}
              connectors={connectors}
              agents={agents}
              relatedProjects={relatedProjects}
              envVars={envVars}
              register={register}
              setRegister={setRegister}
              launchClaude={launchClaude}
              setLaunchClaude={setLaunchClaude}
              error={error}
            />
          )}
        </div>

        <footer className="flex items-center justify-between gap-2 border-t border-zinc-800 px-5 py-3.5">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md px-3 py-1.5 font-display text-sm text-zinc-400 transition-colors hover:text-zinc-200"
          >
            {result ? 'Close' : 'Cancel'}
          </button>
          {!result && (
            <div className="flex items-center gap-2">
              {idx > 0 && (
                <button
                  type="button"
                  onClick={goBack}
                  className="rounded-md border border-zinc-800 px-3 py-1.5 font-display text-sm text-zinc-300 transition-colors hover:border-zinc-700 hover:text-zinc-100"
                >
                  Back
                </button>
              )}
              {step !== 'review' ? (
                <button
                  type="button"
                  onClick={goNext}
                  disabled={!canAdvance}
                  className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Next
                </button>
              ) : (
                <button
                  type="button"
                  onClick={submit}
                  disabled={creating}
                  className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {creating ? 'creating…' : 'Create project'}
                </button>
              )}
            </div>
          )}
        </footer>
      </div>
    </div>
  )
}

// ───────────────────────────── steps ─────────────────────────────

function IdentityStep({
  officialName,
  setOfficialName,
  folderName,
  onFolderChange,
  folderError,
  description,
  setDescription,
}: {
  officialName: string
  setOfficialName: (v: string) => void
  folderName: string
  onFolderChange: (v: string) => void
  folderError: string | null
  description: string
  setDescription: (v: string) => void
}) {
  return (
    <div className="space-y-4">
      <Field label="official name">
        <TextInput value={officialName} onChange={setOfficialName} placeholder="e.g. My Project" autoFocus />
      </Field>
      <Field label="local / folder name">
        <TextInput
          value={folderName}
          onChange={onFolderChange}
          placeholder="my-project"
          invalid={!!folderError}
        />
        {folderError ? (
          <p className="mt-1.5 font-mono text-[10px] text-red-400">{folderError}</p>
        ) : (
          folderName && <p className="mt-1.5 font-mono text-[10px] text-zinc-600">{folderName} in the projects directory</p>
        )}
      </Field>
      <Field label="description">
        <TextInput value={description} onChange={setDescription} placeholder="one line — what is this project?" />
      </Field>
    </div>
  )
}

function ShapeStep({
  stackPresets,
  setStackPresets,
  stackOther,
  setStackOther,
  port,
  setPort,
}: {
  stackPresets: string[]
  setStackPresets: (v: string[]) => void
  stackOther: string
  setStackOther: (v: string) => void
  port: string
  setPort: (v: string) => void
}) {
  const toggle = (p: string) =>
    setStackPresets(stackPresets.includes(p) ? stackPresets.filter((x) => x !== p) : [...stackPresets, p])
  return (
    <div className="space-y-4">
      <Field label="stack (optional)">
        <div className="flex flex-wrap gap-1.5">
          {STACK_PRESETS.map((p) => {
            const on = stackPresets.includes(p)
            return (
              <button
                key={p}
                type="button"
                onClick={() => toggle(p)}
                aria-pressed={on}
                className={`rounded-md border px-2.5 py-1 font-mono text-[11px] transition-colors ${
                  on
                    ? 'border-emerald-500/50 bg-emerald-500/15 text-emerald-300'
                    : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700 hover:text-zinc-200'
                }`}
              >
                {p}
              </button>
            )
          })}
        </div>
        <div className="mt-2">
          <TextInput
            value={stackOther}
            onChange={setStackOther}
            placeholder="other — anything else (versions ok, e.g. Rust 1.79)"
          />
        </div>
      </Field>
      <Field label="port (optional)">
        <TextInput
          value={port}
          onChange={(v) => setPort(v.replace(/[^0-9]/g, ''))}
          placeholder="3000"
          inputMode="numeric"
        />
      </Field>
    </div>
  )
}

function LinksStep({
  connectors,
  setConnectors,
  agents,
  setAgents,
  relatedProjects,
  setRelatedProjects,
  projects,
  envVars,
  setEnvVars,
}: {
  connectors: string[]
  setConnectors: (v: string[]) => void
  agents: string[]
  setAgents: (v: string[]) => void
  relatedProjects: string[]
  setRelatedProjects: (v: string[]) => void
  projects: Project[]
  envVars: CreateProjectEnvVar[]
  setEnvVars: (v: CreateProjectEnvVar[]) => void
}) {
  return (
    <div className="space-y-4">
      <div className="rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-2 text-[11px] leading-relaxed text-zinc-400">
        <span className="text-zinc-300">Captured as intent, not auto-wired.</span> Everything below is
        written into the new project&apos;s docs for the auto-launched Claude session to set up — Lattice
        scaffolds files but never touches your connected accounts. Hover each{' '}
        <span className="font-mono text-zinc-500">i</span> for what it does.
      </div>
      <Field
        label="connectors / mcps"
        info="Captured as intent, not wired. Listed in docs/ONBOARDING.md + CLAUDE.md as setup tasks for the auto-launched Claude session — the hub never touches your already-connected Airtable / Supabase / Slack / GWS accounts."
      >
        <ChipInput
          values={connectors}
          onChange={setConnectors}
          placeholder="type a connector + Enter"
          suggestions={CONNECTOR_SUGGESTIONS}
        />
      </Field>
      <Field
        label="agents (intent)"
        info="Captured as intent. Written to the onboarding brief as agents for Claude to add or reference from ~/.claude/agents — nothing is installed or copied automatically."
      >
        <ChipInput values={agents} onChange={setAgents} placeholder="type an agent + Enter" />
      </Field>
      <Field
        label="related projects"
        info="Context only. Cross-linked in the new project's CLAUDE.md + onboarding brief so Claude knows about them — it changes nothing in those projects."
      >
        {projects.length === 0 ? (
          <p className="font-mono text-[11px] text-zinc-600">// no existing projects to link</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {projects.map((p) => {
              const on = relatedProjects.includes(p.name)
              return (
                <button
                  key={p.path}
                  type="button"
                  onClick={() =>
                    setRelatedProjects(on ? relatedProjects.filter((x) => x !== p.name) : [...relatedProjects, p.name])
                  }
                  className={`rounded-md border px-2.5 py-1 font-mono text-[11px] transition-colors ${
                    on
                      ? 'border-emerald-500/50 bg-emerald-500/15 text-emerald-300'
                      : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700 hover:text-zinc-200'
                  }`}
                >
                  {p.name}
                </button>
              )
            })}
          </div>
        )}
      </Field>
      <Field
        label="env vars"
        info="Written only to the new project's local .env + .env.example (.env stays git-ignored). Not added to any global or shared config. Keys follow the TOKEN_NAME convention."
      >
        <EnvVarRows envVars={envVars} setEnvVars={setEnvVars} />
      </Field>
    </div>
  )
}

function ReviewStep({
  officialName,
  folderName,
  description,
  stack,
  port,
  connectors,
  agents,
  relatedProjects,
  envVars,
  register,
  setRegister,
  launchClaude,
  setLaunchClaude,
  error,
}: {
  officialName: string
  folderName: string
  description: string
  stack: string
  port: string
  connectors: string[]
  agents: string[]
  relatedProjects: string[]
  envVars: CreateProjectEnvVar[]
  register: boolean
  setRegister: (v: boolean) => void
  launchClaude: boolean
  setLaunchClaude: (v: boolean) => void
  error: string | null
}) {
  const filledEnv = envVars.filter((v) => v.key.trim())
  return (
    <div className="space-y-4">
      <dl className="overflow-hidden rounded-md border border-zinc-800">
        <SummaryRow label="name" value={officialName} />
        <SummaryRow label="folder" value={folderName} mono />
        <SummaryRow label="description" value={description} />
        {stack && <SummaryRow label="stack" value={stack} />}
        {port && <SummaryRow label="port" value={port} mono />}
        {connectors.length > 0 && <SummaryRow label="connectors" value={connectors.join(', ')} />}
        {agents.length > 0 && <SummaryRow label="agents" value={agents.join(', ')} />}
        {relatedProjects.length > 0 && <SummaryRow label="related" value={relatedProjects.join(', ')} />}
        {filledEnv.length > 0 && (
          <SummaryRow label="env vars" value={filledEnv.map((v) => v.key).join(', ')} mono />
        )}
      </dl>

      <Toggle
        checked={register}
        onChange={setRegister}
        label="Register project"
        hint="adds the project to the configured registry"
      />
      <Toggle
        checked={launchClaude}
        onChange={setLaunchClaude}
        label="Auto-launch a Claude session"
        hint="opens a session to finish setup"
      />

      {error && (
        <div className="rounded-md border border-red-500/40 bg-red-500/[0.07] px-3 py-2 font-mono text-[11px] text-red-300">
          {error}
        </div>
      )}
    </div>
  )
}

function ResultView({ result }: { result: CreateProjectResult }) {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2.5">
        <span className="grid h-7 w-7 place-items-center rounded-full border border-emerald-500/40 bg-emerald-500/10">
          <svg viewBox="0 0 24 24" className="h-4 w-4 text-emerald-400" fill="none" stroke="currentColor" strokeWidth="2.4" aria-hidden>
            <path d="M5 13l4 4L19 7" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
        <div>
          <p className="font-display text-sm font-semibold text-zinc-100">{result.project.name} created</p>
          <p className="font-mono text-[11px] text-zinc-500">{result.project.path}</p>
        </div>
      </div>

      <dl className="overflow-hidden rounded-md border border-zinc-800">
        <SummaryRow label="registered" value={result.registered ? 'yes' : 'no'} mono />
        <SummaryRow label="session" value={result.session ? 'launched — opening…' : 'none'} mono />
      </dl>

      {result.warnings.length > 0 && (
        <div className="rounded-md border border-orange-500/40 bg-orange-500/[0.07] px-3 py-2">
          <p className="font-mono text-[10px] uppercase tracking-[0.18em] text-orange-300/80">warnings</p>
          <ul className="mt-1 space-y-0.5">
            {result.warnings.map((w, i) => (
              <li key={i} className="font-mono text-[11px] text-orange-300">
                · {w}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

// ───────────────────────────── shared pieces ─────────────────────────────

function Stepper({ current, done }: { current: number; done: boolean }) {
  return (
    <div className="mt-3 flex items-center gap-1.5">
      {STEPS.map((s, i) => {
        const active = !done && i === current
        const past = done || i < current
        return (
          <div key={s} className="flex flex-1 items-center gap-1.5">
            <span
              className={`h-1 flex-1 rounded-full transition-colors ${
                past ? 'bg-emerald-500' : active ? 'bg-emerald-500/40' : 'bg-zinc-800'
              }`}
            />
          </div>
        )
      })}
    </div>
  )
}

function Field({ label, info, children }: { label: string; info?: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-1.5">
        <label className="block font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">{label}</label>
        {info && <InfoDot text={info} />}
      </div>
      {children}
    </div>
  )
}

// InfoDot — a small "i" badge that reveals an honest explanation on hover/focus.
// Used in the wizard's links step to make clear each section is captured as INTENT
// for the auto-launched Claude session, not auto-wired by the hub (F9).
function InfoDot({ text }: { text: string }) {
  return (
    <span className="group relative inline-flex">
      <button
        type="button"
        tabIndex={0}
        aria-label={text}
        className="grid h-3.5 w-3.5 cursor-help place-items-center rounded-full border border-zinc-600 font-mono text-[8px] font-bold leading-none text-zinc-500 transition-colors hover:border-emerald-500/60 hover:text-emerald-300"
        onClick={(e) => e.preventDefault()}
      >
        i
      </button>
      <span
        role="tooltip"
        className="pointer-events-none absolute left-0 top-5 z-10 w-60 rounded-md border border-zinc-700 bg-zinc-950 px-2.5 py-2 text-[11px] normal-case leading-relaxed tracking-normal text-zinc-300 opacity-0 shadow-[0_8px_24px_-8px_rgba(0,0,0,0.9)] transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100"
      >
        {text}
      </span>
    </span>
  )
}

function TextInput({
  value,
  onChange,
  placeholder,
  autoFocus = false,
  invalid = false,
  inputMode,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
  autoFocus?: boolean
  invalid?: boolean
  inputMode?: 'numeric'
}) {
  return (
    <input
      value={value}
      autoFocus={autoFocus}
      inputMode={inputMode}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className={`w-full rounded-md border bg-zinc-950 px-3 py-2 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:outline-none ${
        invalid ? 'border-red-500/60 focus:border-red-500' : 'border-zinc-700 focus:border-emerald-500/60'
      }`}
    />
  )
}

function ChipInput({
  values,
  onChange,
  placeholder,
  suggestions = [],
}: {
  values: string[]
  onChange: (v: string[]) => void
  placeholder: string
  suggestions?: string[]
}) {
  const [draft, setDraft] = useState('')
  const add = (raw: string) => {
    const v = raw.trim()
    if (v && !values.includes(v)) onChange([...values, v])
    setDraft('')
  }
  const open = suggestions.filter((s) => !values.includes(s))
  return (
    <div>
      <div className="flex flex-wrap items-center gap-1.5 rounded-md border border-zinc-700 bg-zinc-950 px-2 py-1.5 focus-within:border-emerald-500/60">
        {values.map((v) => (
          <span
            key={v}
            className="inline-flex items-center gap-1 rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[11px] text-emerald-300"
          >
            {v}
            <button
              type="button"
              onClick={() => onChange(values.filter((x) => x !== v))}
              className="text-emerald-400/70 hover:text-emerald-200"
              aria-label={`remove ${v}`}
            >
              ×
            </button>
          </span>
        ))}
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              e.stopPropagation()
              add(draft)
            } else if (e.key === 'Backspace' && !draft && values.length) {
              onChange(values.slice(0, -1))
            }
          }}
          placeholder={values.length ? '' : placeholder}
          className="min-w-[8rem] flex-1 bg-transparent font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:outline-none"
        />
      </div>
      {open.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1.5">
          {open.map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => add(s)}
              className="rounded border border-zinc-800 bg-zinc-950 px-2 py-0.5 font-mono text-[10px] text-zinc-500 transition-colors hover:border-emerald-500/40 hover:text-emerald-300"
            >
              + {s}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// sanitizeEnvKey enforces the TOKEN_NAME convention as the user types: uppercase,
// spaces (and other separators) collapse to `_`, and anything outside [A-Z0-9_] is
// dropped — so a key like "my api key" becomes "MY_API_KEY", never an invalid env
// var. Digits stay (versions/indices are valid in keys).
function sanitizeEnvKey(raw: string): string {
  return raw
    .toUpperCase()
    .replace(/[\s-]+/g, '_')
    .replace(/[^A-Z0-9_]/g, '')
}

function EnvVarRows({
  envVars,
  setEnvVars,
}: {
  envVars: CreateProjectEnvVar[]
  setEnvVars: (v: CreateProjectEnvVar[]) => void
}) {
  const update = (i: number, patch: Partial<CreateProjectEnvVar>) =>
    setEnvVars(envVars.map((row, j) => (j === i ? { ...row, ...patch } : row)))
  return (
    <div className="space-y-1.5">
      {envVars.map((row, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <input
            value={row.key}
            onChange={(e) => update(i, { key: sanitizeEnvKey(e.target.value) })}
            placeholder="KEY"
            spellCheck={false}
            autoCapitalize="characters"
            className="w-2/5 rounded-md border border-zinc-700 bg-zinc-950 px-2.5 py-1.5 font-mono text-[11px] uppercase text-zinc-200 placeholder:text-zinc-600 focus:border-emerald-500/60 focus:outline-none"
          />
          <input
            value={row.value}
            onChange={(e) => update(i, { value: e.target.value })}
            placeholder="value"
            className="min-w-0 flex-1 rounded-md border border-zinc-700 bg-zinc-950 px-2.5 py-1.5 font-mono text-[11px] text-zinc-200 placeholder:text-zinc-600 focus:border-emerald-500/60 focus:outline-none"
          />
          <button
            type="button"
            onClick={() => setEnvVars(envVars.filter((_, j) => j !== i))}
            aria-label="remove env var"
            className="grid h-7 w-7 shrink-0 place-items-center rounded text-zinc-600 hover:bg-zinc-800 hover:text-red-300"
          >
            <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
              <path d="M6 6l12 12M18 6 6 18" strokeLinecap="round" />
            </svg>
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={() => setEnvVars([...envVars, { key: '', value: '' }])}
        className="flex items-center gap-1.5 font-mono text-[11px] text-zinc-500 transition-colors hover:text-emerald-300"
      >
        <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
          <path d="M12 5v14m-7-7h14" strokeLinecap="round" />
        </svg>
        add env var
      </button>
    </div>
  )
}

function SummaryRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex gap-3 border-b border-zinc-800/70 px-3 py-2 last:border-0">
      <dt className="w-24 shrink-0 font-mono text-[10px] uppercase tracking-[0.14em] text-zinc-600">{label}</dt>
      <dd className={`min-w-0 flex-1 break-words text-[12px] text-zinc-200 ${mono ? 'font-mono' : 'font-display'}`}>
        {value}
      </dd>
    </div>
  )
}

function Toggle({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  hint: string
}) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className="flex w-full items-center gap-3 rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2.5 text-left transition-colors hover:border-zinc-700"
    >
      <span
        className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${
          checked ? 'bg-emerald-500' : 'bg-zinc-700'
        }`}
      >
        <span
          className={`absolute top-0.5 h-4 w-4 rounded-full bg-zinc-950 transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
      <span className="min-w-0">
        <span className="block font-display text-[13px] text-zinc-100">{label}</span>
        <span className="block font-mono text-[10px] text-zinc-500">{hint}</span>
      </span>
    </button>
  )
}

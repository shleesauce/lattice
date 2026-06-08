import { useEffect, useMemo, useRef, useState } from 'react'
import { checkSetupRoot, submitSetup } from '../api'
import type { RootCheck, SetupStatus } from '../types'
import { parseHubError } from '../lib/hubError'
import logoMark from '../design/logo-mark.svg'
import { Icon } from '../lattice/Icon'

interface Props {
  status: SetupStatus
  onDone: () => void
}

const STEPS = ['admin', 'mesh', 'root', 'machines', 'review'] as const
type Step = (typeof STEPS)[number]

// Full-screen gate shown until the hub is configured. Five steps — admin
// password, mesh name, projects root (live-validated), add machines (optional),
// review — then POST /api/setup. No Cancel/close: you can't skip setup, and
// Esc never dismisses it.
export function FirstRunWizard({ status, onDone }: Props) {
  const [step, setStep] = useState<Step>('admin')

  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [showPassword, setShowPassword] = useState(false)

  const [meshName, setMeshName] = useState(status.meshName || 'lattice')

  const [root, setRoot] = useState(status.suggestedRoot || status.projectsRoot || '')
  const [rootCheck, setRootCheck] = useState<RootCheck | null>(null)
  const [checking, setChecking] = useState(false)

  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Monotonic token so a stale debounced check (path changed mid-flight) is dropped.
  const checkSeq = useRef(0)

  // ── validation ──
  const passwordError = useMemo(() => {
    if (!password) return null
    if (password.length < 8) return 'password must be at least 8 characters'
    if (confirm && password !== confirm) return 'passwords do not match'
    return null
  }, [password, confirm])
  const passwordValid = password.length >= 8 && password === confirm

  const meshValid = meshName.trim().length > 0 && meshName.trim().length <= 40

  // The root step is valid unless the latest live check explicitly failed.
  const rootValid = root.trim().length > 0 && rootCheck?.ok !== false

  // ── debounced live root check (~350ms) ──
  useEffect(() => {
    const path = root.trim()
    if (!path) {
      setRootCheck(null)
      setChecking(false)
      return
    }
    setChecking(true)
    const seq = ++checkSeq.current
    const handle = setTimeout(() => {
      checkSetupRoot(path)
        .then((res) => {
          if (seq === checkSeq.current) setRootCheck(res)
        })
        .catch(() => {
          if (seq === checkSeq.current) setRootCheck(null)
        })
        .finally(() => {
          if (seq === checkSeq.current) setChecking(false)
        })
    }, 350)
    return () => clearTimeout(handle)
  }, [root])

  const idx = STEPS.indexOf(step)
  // 'machines' is optional/informational — always valid so Next is never blocked.
  const stepValid =
    step === 'admin' ? passwordValid : step === 'mesh' ? meshValid : step === 'root' ? rootValid : true

  const goNext = () => {
    if (idx < STEPS.length - 1 && stepValid) setStep(STEPS[idx + 1])
  }
  const goBack = () => {
    if (idx > 0) setStep(STEPS[idx - 1])
  }

  // Enter advances on non-review steps. Esc is swallowed — this is a gate.
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      return
    }
    if (e.key === 'Enter' && step !== 'review') {
      e.preventDefault()
      goNext()
    }
  }

  const resolvedRoot = rootCheck?.ok ? rootCheck.resolved ?? root.trim() : root.trim()

  const submit = async () => {
    setSubmitting(true)
    setError(null)
    try {
      await submitSetup({
        adminPassword: password,
        meshName: meshName.trim(),
        projectsRoot: root.trim(),
      })
      onDone()
    } catch (e) {
      setError(parseHubError(e, 'setup failed'))
      setSubmitting(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 grid min-h-screen place-items-center overflow-y-auto bg-[var(--base)] p-4"
      onKeyDown={onKeyDown}
    >
      <div className="flex max-h-[92vh] w-full max-w-lg flex-col rounded-xl border border-zinc-800 bg-zinc-900 shadow-[0_20px_60px_-20px_rgba(0,0,0,0.8)] animate-risein">
        <header className="border-b border-zinc-800 px-5 py-5">
          <div className="flex items-center gap-3">
            <img src={logoMark} alt="" className="h-8 w-8" />
            <div className="min-w-0">
              <h1 className="font-display text-base font-semibold text-zinc-50">Welcome to Lattice</h1>
              <p className="font-mono text-[11px] text-zinc-500">Let&apos;s set up your hub.</p>
            </div>
            <span className="ml-auto self-start font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-600">
              step {idx + 1} / {STEPS.length}
            </span>
          </div>
          <Stepper current={idx} />
        </header>

        <div className="term-scroll min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {step === 'admin' ? (
            <AdminStep
              password={password}
              setPassword={setPassword}
              confirm={confirm}
              setConfirm={setConfirm}
              show={showPassword}
              setShow={setShowPassword}
              error={passwordError}
            />
          ) : step === 'mesh' ? (
            <MeshStep meshName={meshName} setMeshName={setMeshName} hostname={status.hostname} />
          ) : step === 'root' ? (
            <RootStep root={root} setRoot={setRoot} check={rootCheck} checking={checking} />
          ) : step === 'machines' ? (
            <MachinesStep />
          ) : (
            <ReviewStep
              meshName={meshName.trim()}
              resolvedRoot={resolvedRoot}
              willCreate={!!rootCheck?.willCreate}
              error={error}
            />
          )}
        </div>

        <footer className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3.5">
          {idx > 0 && (
            <button
              type="button"
              onClick={goBack}
              disabled={submitting}
              className="rounded-md border border-zinc-800 px-3 py-1.5 font-display text-sm text-zinc-300 transition-colors hover:border-zinc-700 hover:text-zinc-100 disabled:opacity-50"
            >
              Back
            </button>
          )}
          {step !== 'review' ? (
            <button
              type="button"
              onClick={goNext}
              disabled={!stepValid}
              className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Next
            </button>
          ) : (
            <button
              type="button"
              onClick={submit}
              disabled={submitting}
              className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {submitting ? 'finishing…' : 'Finish setup'}
            </button>
          )}
        </footer>
      </div>
    </div>
  )
}

// ───────────────────────────── steps ─────────────────────────────

function AdminStep({
  password,
  setPassword,
  confirm,
  setConfirm,
  show,
  setShow,
  error,
}: {
  password: string
  setPassword: (v: string) => void
  confirm: string
  setConfirm: (v: string) => void
  show: boolean
  setShow: (v: boolean) => void
  error: string | null
}) {
  return (
    <div className="space-y-4">
      <div>
        <h2 className="font-display text-sm font-semibold text-zinc-100">Set an admin password</h2>
        <p className="mt-1 text-[12px] leading-relaxed text-zinc-400">This password protects your hub&apos;s dashboard.</p>
      </div>
      <Field label="password">
        <PasswordInput value={password} onChange={setPassword} placeholder="at least 8 characters" show={show} setShow={setShow} autoFocus invalid={!!error} />
      </Field>
      <Field label="confirm password">
        <PasswordInput value={confirm} onChange={setConfirm} placeholder="re-enter password" show={show} setShow={setShow} invalid={!!error && password.length >= 8} />
        {error && <p className="mt-1.5 font-mono text-[10px] text-red-400">{error}</p>}
      </Field>
      <div className="rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-2 text-[11px] leading-relaxed text-zinc-400">
        <span className="text-zinc-300">Note:</span> this password protects the dashboard and API. Auth activates as
        soon as it&apos;s set — leave it blank only on a trusted private network.
      </div>
    </div>
  )
}

function MeshStep({
  meshName,
  setMeshName,
  hostname,
}: {
  meshName: string
  setMeshName: (v: string) => void
  hostname?: string
}) {
  return (
    <div className="space-y-4">
      <div>
        <h2 className="font-display text-sm font-semibold text-zinc-100">Name your mesh</h2>
        <p className="mt-1 text-[12px] leading-relaxed text-zinc-400">A label for this fleet of machines.</p>
      </div>
      <Field label="mesh name">
        <TextInput value={meshName} onChange={(v) => setMeshName(v.slice(0, 40))} placeholder="lattice" autoFocus />
        <p className="mt-1.5 font-mono text-[10px] text-zinc-600">
          {meshName.trim().length}/40{hostname ? ` · this hub: ${hostname}` : ''}
        </p>
      </Field>
    </div>
  )
}

function RootStep({
  root,
  setRoot,
  check,
  checking,
}: {
  root: string
  setRoot: (v: string) => void
  check: RootCheck | null
  checking: boolean
}) {
  return (
    <div className="space-y-4">
      <div>
        <h2 className="font-display text-sm font-semibold text-zinc-100">Where are your projects?</h2>
        <p className="mt-1 text-[12px] leading-relaxed text-zinc-400">The folder Lattice scans for projects.</p>
      </div>
      <Field label="projects root">
        <TextInput value={root} onChange={setRoot} placeholder="~/projects" autoFocus invalid={check?.ok === false} />
        <div className="mt-1.5 min-h-[14px]">
          {checking ? (
            <p className="font-mono text-[10px] text-zinc-600">checking…</p>
          ) : check?.ok === false ? (
            <p className="font-mono text-[10px] text-red-400">{check.error || 'invalid path'}</p>
          ) : check?.ok ? (
            check.willCreate ? (
              <p className="font-mono text-[10px] text-teal-400">{check.resolved} — will be created</p>
            ) : (
              <p className="font-mono text-[10px] text-zinc-500">{check.resolved}</p>
            )
          ) : null}
        </div>
      </Field>
    </div>
  )
}

// ── MachinesStep — optional, always-valid ──────────────────────────────────
// Tells the user how to add other machines after setup. Surfaces the Tailscale
// install command so they can get this hub on a tailnet now if they want.
function MachinesStep() {
  // Initialize with the canonical commands so the field is NEVER empty. The
  // wizard runs before an admin password exists, and /api/enroll is auth-gated
  // (401 mid-first-run), so the fetch can't be relied on — we just best-effort
  // override these defaults if the endpoint happens to answer.
  const [tsUnix, setTsUnix] = useState('curl -fsSL https://tailscale.com/install.sh | sh && sudo tailscale up')
  const [tsWindows, setTsWindows] = useState('winget install -e --id tailscale.tailscale --source winget; tailscale up')
  const [tsOs, setTsOs] = useState<'unix' | 'windows'>('unix')
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])

  // Best-effort: if the enroll endpoint is reachable, prefer its values so the
  // commands stay in lockstep with the backend. Guarded on r.ok so a 401/HTML
  // error body never overwrites the working defaults above.
  useEffect(() => {
    fetch('/api/enroll')
      .then((r) => (r.ok ? r.json() : null))
      .then((d: { tailscaleUnix?: string; tailscaleWindows?: string } | null) => {
        if (d?.tailscaleUnix) setTsUnix(d.tailscaleUnix)
        if (d?.tailscaleWindows) setTsWindows(d.tailscaleWindows)
      })
      .catch(() => {})
  }, [])

  const cmd = tsOs === 'unix' ? (tsUnix ?? '') : (tsWindows ?? '')

  const copyCmd = () => {
    if (!cmd) return
    const markCopied = () => {
      setCopied(true)
      clearTimeout(timer.current)
      timer.current = setTimeout(() => setCopied(false), 1600)
    }
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(cmd).then(markCopied).catch(legacyCopy)
    } else {
      legacyCopy()
    }
    function legacyCopy() {
      try {
        const ta = document.createElement('textarea')
        ta.value = cmd
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
        markCopied()
      } catch { /* ignore */ }
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <h2 className="font-display text-sm font-semibold text-zinc-100">Add your other machines</h2>
        <p className="mt-1 text-[12px] leading-relaxed text-zinc-400">
          This hub is set up. To bring in more computers, use{' '}
          <span className="text-zinc-200">Manage mesh → Add machine</span> after setup. Machines on a
          different network than this hub need{' '}
          <a
            href="https://tailscale.com/download"
            target="_blank"
            rel="noreferrer"
            className="text-emerald-400 hover:text-emerald-300 transition-colors"
          >
            Tailscale
          </a>{' '}
          (free) — installed and signed into the same account on each.
        </p>
      </div>

      <div className="rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-3 space-y-2.5">
        <div className="flex items-center gap-2">
          <Icon name="zap" size={13} color="var(--green)" />
          <span className="font-display text-[12px] font-semibold text-zinc-200">Install Tailscale on this hub now</span>
          <span className="font-mono text-[10px] text-zinc-600">· optional</span>
        </div>
        <p className="font-mono text-[10px] leading-relaxed text-zinc-500">
          Run this in a terminal on this machine so the hub itself is reachable on your tailnet.
        </p>
        <div>
          <div className="flex items-center gap-1 mb-1.5">
            {(['unix', 'windows'] as const).map((o) => {
              const on = tsOs === o
              return (
                <button
                  key={o}
                  type="button"
                  onClick={() => setTsOs(o)}
                  className={`rounded-md px-2.5 py-1 font-mono text-[11px] transition-colors ${
                    on ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'
                  }`}
                >
                  {o === 'unix' ? 'macOS / Linux' : 'Windows'}
                </button>
              )
            })}
          </div>
          <div className="flex items-start gap-2 rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2.5">
            {cmd ? (
              <code className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-zinc-300">
                {cmd}
              </code>
            ) : (
              <div className="h-4 flex-1 animate-pulse rounded bg-zinc-800" />
            )}
            <button
              type="button"
              onClick={copyCmd}
              disabled={!cmd}
              aria-label="copy command"
              className={`flex shrink-0 items-center gap-1 rounded border px-2 py-1 font-mono text-[10px] transition-colors disabled:opacity-40 ${
                copied
                  ? 'border-emerald-500/50 bg-emerald-500/10 text-emerald-300'
                  : 'border-zinc-800 text-zinc-400 hover:border-zinc-700 hover:text-zinc-200'
              }`}
            >
              <Icon name={copied ? 'check' : 'copy'} size={11} />
              {copied ? 'copied' : 'copy'}
            </button>
          </div>
        </div>
        <a
          href="https://tailscale.com/download"
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 font-mono text-[11px] text-emerald-300 transition-colors hover:text-emerald-200"
        >
          <Icon name="link" size={11} />
          tailscale.com/download
        </a>
      </div>

      <p className="font-mono text-[10px] text-zinc-600">
        This step is optional — you can skip it and add machines any time from the dashboard.
      </p>
    </div>
  )
}

function ReviewStep({
  meshName,
  resolvedRoot,
  willCreate,
  error,
}: {
  meshName: string
  resolvedRoot: string
  willCreate: boolean
  error: string | null
}) {
  return (
    <div className="space-y-4">
      <div>
        <h2 className="font-display text-sm font-semibold text-zinc-100">Review</h2>
        <p className="mt-1 text-[12px] leading-relaxed text-zinc-400">Confirm and finish setting up your hub.</p>
      </div>
      <dl className="overflow-hidden rounded-md border border-zinc-800">
        <SummaryRow label="mesh name" value={meshName} />
        <SummaryRow label="projects root" value={willCreate ? `${resolvedRoot} (new)` : resolvedRoot} mono />
        <SummaryRow label="admin password" value="set" mono />
      </dl>
      {error && (
        <div className="rounded-md border border-red-500/40 bg-red-500/[0.07] px-3 py-2 font-mono text-[11px] text-red-300">
          {error}
        </div>
      )}
    </div>
  )
}

// ───────────────────────────── shared pieces ─────────────────────────────

function Stepper({ current }: { current: number }) {
  return (
    <div className="mt-4 flex items-center gap-1.5">
      {STEPS.map((s, i) => (
        <span
          key={s}
          className={`h-1 flex-1 rounded-full transition-colors ${
            i < current ? 'bg-emerald-500' : i === current ? 'bg-emerald-500/40' : 'bg-zinc-800'
          }`}
        />
      ))}
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

function TextInput({
  value,
  onChange,
  placeholder,
  autoFocus = false,
  invalid = false,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
  autoFocus?: boolean
  invalid?: boolean
}) {
  return (
    <input
      value={value}
      autoFocus={autoFocus}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className={`w-full rounded-md border bg-zinc-950 px-3 py-2 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:outline-none ${
        invalid ? 'border-red-500/60 focus:border-red-500' : 'border-zinc-700 focus:border-emerald-500/60'
      }`}
    />
  )
}

function PasswordInput({
  value,
  onChange,
  placeholder,
  show,
  setShow,
  autoFocus = false,
  invalid = false,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
  show: boolean
  setShow: (v: boolean) => void
  autoFocus?: boolean
  invalid?: boolean
}) {
  return (
    <div className="relative">
      <input
        type={show ? 'text' : 'password'}
        value={value}
        autoFocus={autoFocus}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoComplete="new-password"
        className={`w-full rounded-md border bg-zinc-950 px-3 py-2 pr-10 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:outline-none ${
          invalid ? 'border-red-500/60 focus:border-red-500' : 'border-zinc-700 focus:border-emerald-500/60'
        }`}
      />
      <button
        type="button"
        onClick={() => setShow(!show)}
        aria-label={show ? 'hide password' : 'show password'}
        className="absolute right-1.5 top-1/2 grid h-7 w-7 -translate-y-1/2 place-items-center rounded text-zinc-500 transition-colors hover:text-zinc-200"
      >
        {show ? (
          <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
            <path d="M3 3l18 18M10.6 10.6a2 2 0 002.8 2.8M9.9 5.1A9.5 9.5 0 0121 12a17 17 0 01-2.2 3M6.6 6.6A17 17 0 003 12a16 16 0 0011 7" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        ) : (
          <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
            <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" strokeLinecap="round" strokeLinejoin="round" />
            <circle cx="12" cy="12" r="3" />
          </svg>
        )}
      </button>
    </div>
  )
}

function SummaryRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex gap-3 border-b border-zinc-800/70 px-3 py-2 last:border-0">
      <dt className="w-28 shrink-0 font-mono text-[10px] uppercase tracking-[0.14em] text-zinc-600">{label}</dt>
      <dd className={`min-w-0 flex-1 break-words text-[12px] text-zinc-200 ${mono ? 'font-mono' : 'font-display'}`}>
        {value}
      </dd>
    </div>
  )
}

import { useState } from 'react'
import { login } from '../api'
import { parseHubError } from '../lib/hubError'
import logoMark from '../design/logo-mark.svg'

interface Props {
  onAuthed: () => void
}

// Full-screen gate shown when the hub requires auth and this browser isn't yet
// authenticated. Single password field → POST /api/auth/login → on success the
// caller reloads into the dashboard. Same chrome as FirstRunWizard, one step.
export function Login({ onAuthed }: Props) {
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const canSubmit = password.length > 0 && !submitting

  const submit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      await login(password)
      onAuthed()
    } catch (e) {
      setError(parseHubError(e, 'login failed'))
      setSubmitting(false)
    }
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      void submit()
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 grid min-h-screen place-items-center overflow-y-auto bg-[var(--base)] p-4"
      onKeyDown={onKeyDown}
    >
      <div className="flex w-full max-w-sm flex-col rounded-xl border border-zinc-800 bg-zinc-900 shadow-[0_20px_60px_-20px_rgba(0,0,0,0.8)] animate-risein">
        <header className="border-b border-zinc-800 px-5 py-5">
          <div className="flex items-center gap-3">
            <img src={logoMark} alt="" className="h-8 w-8" />
            <div className="min-w-0">
              <h1 className="font-display text-base font-semibold text-zinc-50">Lattice</h1>
              <p className="font-mono text-[11px] text-zinc-500">Enter your admin password</p>
            </div>
          </div>
        </header>

        <div className="px-5 py-4">
          <label className="mb-1.5 block font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">password</label>
          <div className="relative">
            <input
              type={show ? 'text' : 'password'}
              value={password}
              autoFocus
              onChange={(e) => {
                setPassword(e.target.value)
                if (error) setError(null)
              }}
              placeholder="admin password"
              autoComplete="current-password"
              className={`w-full rounded-md border bg-zinc-950 px-3 py-2 pr-10 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:outline-none ${
                error ? 'border-red-500/60 focus:border-red-500' : 'border-zinc-700 focus:border-emerald-500/60'
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
          {error && <p className="mt-1.5 font-mono text-[10px] text-red-400">{error}</p>}
        </div>

        <footer className="flex items-center justify-end border-t border-zinc-800 px-5 py-3.5">
          <button
            type="button"
            onClick={() => void submit()}
            disabled={!canSubmit}
            className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {submitting ? 'unlocking…' : 'Unlock'}
          </button>
        </footer>
      </div>
    </div>
  )
}

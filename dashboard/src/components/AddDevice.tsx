import { useEffect, useState } from 'react'
import type { Enroll } from '../types'
import { fetchEnroll } from '../api'
import { OSGlyph } from './Glyphs'

type Tab = 'unix' | 'windows'

export function AddDevice({ onClose }: { onClose: () => void }) {
  const [data, setData] = useState<Enroll | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [tab, setTab] = useState<Tab>('unix')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    fetchEnroll()
      .then((d) => {
        if (!cancelled) setData(d)
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'failed to load enrollment')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Close on Escape; lock background scroll while open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [onClose])

  const oneLiner = data ? (tab === 'unix' ? data.unix : data.windows) : ''

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-zinc-950/80 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-label="Add a device to the mesh"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="lattice-bg flex max-h-[90vh] w-full max-w-2xl animate-risein flex-col overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/95 shadow-2xl shadow-black/60">
        {/* header */}
        <header className="flex items-center gap-3 border-b border-zinc-800 px-5 py-4">
          <div className="grid h-9 w-9 place-items-center rounded-lg border border-emerald-500/30 bg-emerald-500/10">
            <PlusNode />
          </div>
          <div className="min-w-0">
            <h2 className="font-display text-base font-bold tracking-tight text-zinc-50">add a device</h2>
            <p className="font-mono text-[10px] uppercase tracking-[0.22em] text-zinc-500">enroll into the mesh</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="close"
            className="ml-auto grid h-8 w-8 place-items-center rounded-md border border-zinc-800 text-zinc-500 transition-colors hover:border-zinc-700 hover:text-zinc-200"
          >
            <CloseIcon />
          </button>
        </header>

        <div className="term-scroll min-h-0 flex-1 overflow-y-auto px-5 py-5">
          {loading ? (
            <Skeleton />
          ) : error ? (
            <ErrorState error={error} />
          ) : data ? (
            <>
              <p className="mb-4 text-sm leading-relaxed text-zinc-400">
                Run one line on the new machine. The agent downloads itself from this hub, installs as a
                background service, and joins the mesh automatically. No GitHub, no manual config.
              </p>

              {/* OS tabs */}
              <div className="flex gap-1 rounded-lg border border-zinc-800 bg-zinc-950 p-1">
                <TabButton active={tab === 'unix'} onClick={() => setTab('unix')}>
                  <OSGlyph os="darwin" className="h-3.5 w-3.5" />
                  <OSGlyph os="linux" className="h-3.5 w-3.5" />
                  macOS / Linux
                </TabButton>
                <TabButton active={tab === 'windows'} onClick={() => setTab('windows')}>
                  <OSGlyph os="windows" className="h-3.5 w-3.5" />
                  Windows
                </TabButton>
              </div>

              {/* one-liner */}
              <div className="mt-3">
                <Label>{tab === 'windows' ? 'PowerShell (run as your user)' : 'Terminal'}</Label>
                <CopyBlock text={oneLiner} />
                <p className="mt-2 font-mono text-[11px] text-zinc-600">
                  {tab === 'windows'
                    ? '// sets the enroll token for this session, then runs the installer'
                    : '// curl falls back to wget; installs a per-user service that restarts on boot'}
                </p>
              </div>

              {/* raw values */}
              <div className="mt-5 grid gap-3 sm:grid-cols-[auto_1fr] sm:items-center">
                <Label inline>hub url</Label>
                <CopyBlock text={data.hubUrl} compact />
                <Label inline>enroll token</Label>
                <CopyBlock text={data.token} compact mono />
              </div>

              <p className="mt-5 text-[11px] leading-relaxed text-zinc-500">
                Once it runs, the new machine appears in the{' '}
                <span className="text-emerald-400">fleet</span> grid within a few seconds.
              </p>
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex flex-1 items-center justify-center gap-1.5 rounded-md px-3 py-2 font-display text-xs font-semibold transition-colors ${
        active ? 'bg-emerald-500 text-emerald-950' : 'text-zinc-400 hover:text-zinc-200'
      }`}
    >
      {children}
    </button>
  )
}

function CopyBlock({ text, compact, mono }: { text: string; compact?: boolean; mono?: boolean }) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Fallback for non-secure contexts where the Clipboard API is unavailable.
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      try {
        document.execCommand('copy')
      } catch {
        /* ignore */
      }
      document.body.removeChild(ta)
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 1400)
  }

  return (
    <div className="group flex items-stretch gap-2 rounded-md border border-zinc-800 bg-zinc-950 focus-within:border-emerald-500/50">
      <code
        className={`min-w-0 flex-1 select-all overflow-x-auto whitespace-nowrap px-3 ${
          compact ? 'py-1.5' : 'py-2.5'
        } font-mono ${compact && !mono ? 'text-zinc-300' : 'text-zinc-200'} text-[12.5px] term-scroll`}
      >
        {text}
      </code>
      <button
        type="button"
        onClick={copy}
        aria-label="copy"
        className={`flex shrink-0 items-center gap-1.5 rounded-r-md border-l border-zinc-800 px-3 font-mono text-[11px] font-semibold transition-colors ${
          copied ? 'text-emerald-400' : 'text-zinc-400 hover:text-emerald-300'
        }`}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
        {copied ? 'copied' : 'copy'}
      </button>
    </div>
  )
}

function Label({ children, inline }: { children: React.ReactNode; inline?: boolean }) {
  return (
    <div className={`font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500 ${inline ? '' : 'mb-1.5'}`}>
      {children}
    </div>
  )
}

function Skeleton() {
  return (
    <div className="space-y-4">
      <div className="h-3 w-3/4 animate-pulse rounded bg-zinc-800/70" />
      <div className="h-10 w-full animate-pulse rounded-lg bg-zinc-800/50" />
      <div className="h-11 w-full animate-pulse rounded-md bg-zinc-800/50" />
      <div className="h-8 w-1/2 animate-pulse rounded-md bg-zinc-800/40" />
    </div>
  )
}

function ErrorState({ error }: { error: string }) {
  return (
    <div className="px-2 py-8 text-center">
      <p className="font-display text-sm font-semibold text-red-300">could not load enrollment</p>
      <p className="mt-1 font-mono text-[11px] text-red-400/70">{error}</p>
    </div>
  )
}

function PlusNode() {
  return (
    <svg viewBox="0 0 24 24" className="h-5 w-5 text-emerald-400" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
      <path d="M12 2 22 7v10L12 22 2 17V7z" strokeLinejoin="round" />
      <path d="M12 9v6m-3-3h6" strokeLinecap="round" />
    </svg>
  )
}

function CloseIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M6 6l12 12M18 6 6 18" strokeLinecap="round" />
    </svg>
  )
}

function CopyIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M5 12l5 5L20 7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

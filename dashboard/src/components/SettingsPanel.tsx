/* Settings — the gear in the top bar. Keeps to what actually persists and acts:
   the primary coding machine (D32, a soft placement pin honoured when eligible),
   plus an honest "about" block (hub version, fleet size, link). */
import { useEffect, useRef, useState } from 'react'
import { canHostClaude, type Agent } from '../types'
import { fetchSettings, logout, setPrimaryAgent } from '../api'
import { Modal } from './Modal'
import { ReleaseNotes } from './ReleaseNotes'
import { Icon } from '../lattice/Icon'

interface Props {
  agents: Agent[]
  version?: string
  onlineCount: number
  totalCount: number
  onClose: () => void
}

function label(a: Agent): string {
  return a.name || a.hostname || a.id.slice(0, 8)
}

export function SettingsPanel({ agents, version, onlineCount, totalCount, onClose }: Props) {
  const [primary, setPrimary] = useState<string>('')
  const [loaded, setLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loggingOut, setLoggingOut] = useState(false)
  const [showNotes, setShowNotes] = useState(false)

  // Log out clears the session cookie then reloads — the App gate then shows the
  // Login screen (or stays on the dashboard if auth is off, which is harmless).
  const onLogout = async () => {
    if (loggingOut) return
    setLoggingOut(true)
    try {
      await logout()
    } finally {
      window.location.reload()
    }
  }

  useEffect(() => {
    let cancelled = false
    fetchSettings()
      .then((s) => {
        if (cancelled) return
        setPrimary(s.primaryAgent ?? '')
        setLoaded(true)
      })
      .catch(() => !cancelled && setLoaded(true))
    return () => {
      cancelled = true
    }
  }, [])

  // Eligible primaries = machines that can actually host a Claude/project session
  // (installed AND authable — F14). The primary is claude's default target, so a
  // non-authable box must not be offered or it'd default new sessions to a blank tab.
  const candidates = agents.filter((a) => canHostClaude(a))

  const choose = async (id: string) => {
    if (saving) return
    const prev = primary
    setPrimary(id) // optimistic
    setSaving(true)
    setError(null)
    try {
      await setPrimaryAgent(id)
    } catch (e) {
      setPrimary(prev)
      setError(e instanceof Error ? e.message : 'failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal onClose={onClose} ariaLabel="Settings">
      <div className="flex items-start justify-between">
          <div>
            <h3>Settings</h3>
            <p className="sub" style={{ marginBottom: 0 }}>Hub-wide preferences for the mesh.</p>
          </div>
          <button type="button" className="iconbtn" onClick={onClose} title="close">
            <Icon name="x" size={17} />
          </button>
        </div>

        <div className="set-section">
          <div className="set-section-t">Primary coding machine</div>
          <div className="set-section-d">Default placement for new project sessions</div>
        </div>
        {!loaded ? (
          <div className="set-skel" />
        ) : candidates.length === 0 ? (
          <p className="sub" style={{ margin: 0 }}>No machine reports Claude yet — start an agent on a box with the Claude CLI.</p>
        ) : (
          <PrimaryPicker candidates={candidates} primary={primary} saving={saving} onChoose={choose} />
        )}
        <p className="set-note">
          {primary
            ? 'New project sessions pin here when it’s eligible; otherwise the mesh auto-places on the best machine.'
            : 'No primary set — every project session is auto-placed on the best available machine.'}
        </p>
        {error && <p className="set-err">{error}</p>}

        <div className="set-section" style={{ marginTop: 22 }}>
          <div className="set-section-t">About</div>
        </div>
        <div className="set-about">
          <div><span className="k">Hub version</span><span className="v mono">{version ? `v${version}` : '—'}</span></div>
          <div><span className="k">Fleet</span><span className="v mono">{onlineCount} online · {totalCount} known</span></div>
          <div>
            <span className="k">Hub URL</span>
            <span className="v mono" style={{ color: 'var(--teal)' }}>{location.host}</span>
          </div>
          <button type="button" className="set-about-action" onClick={() => setShowNotes(true)}>
            <Icon name="sparkles" size={14} color="var(--teal)" />
            <span>Release notes</span>
            <Icon name="chevron-right" size={14} color="var(--fg-3)" style={{ marginLeft: 'auto' }} />
          </button>
        </div>

        {showNotes && <ReleaseNotes onClose={() => setShowNotes(false)} />}

        <div className="mt-5 flex justify-end">
          <button
            type="button"
            onClick={onLogout}
            disabled={loggingOut}
            className="inline-flex items-center gap-2 rounded-md border border-zinc-700 px-3 py-1.5 font-display text-sm text-zinc-300 transition-colors hover:border-red-500/60 hover:bg-red-500/[0.06] hover:text-red-300 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Icon name="power" size={15} />
            {loggingOut ? 'logging out…' : 'Log out'}
          </button>
        </div>
    </Modal>
  )
}

// PrimaryPicker — a dropdown (collapsed by default, F7) for choosing the primary
// coding machine, replacing the always-expanded list of every candidate. Closed it
// shows the current pick (or "Auto-place"); open it lists the auto-place option plus
// each Claude-capable machine. Closes on outside click or Esc.
function PrimaryPicker({
  candidates,
  primary,
  saving,
  onChoose,
}: {
  candidates: Agent[]
  primary: string
  saving: boolean
  onChoose: (id: string) => void
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  const selected = candidates.find((a) => a.id === primary)

  const pick = (id: string) => {
    onChoose(id)
    setOpen(false)
  }

  return (
    <div
      className="set-dd"
      ref={ref}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && open) {
          e.stopPropagation()
          setOpen(false)
        }
      }}
    >
      <button
        type="button"
        className={`set-dd-trigger ${open ? 'open' : ''}`}
        onClick={() => setOpen((v) => !v)}
        disabled={saving}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        {selected ? (
          <>
            <span className={`dot ${selected.online ? 'live' : 'exited'}`} />
            <span className="set-name">{label(selected)}</span>
            <span className="set-meta">{selected.online ? `${selected.cpuCount} cores` : 'offline'}</span>
          </>
        ) : (
          <span className="set-name set-dd-auto">Auto-place — no primary</span>
        )}
        <Icon name="chevron-down" size={15} style={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur) var(--ease)' }} />
      </button>

      {open && (
        <div className="set-dd-menu" role="listbox">
          <button
            type="button"
            role="option"
            aria-selected={!primary}
            className={`set-dd-opt ${!primary ? 'on' : ''}`}
            onClick={() => pick('')}
          >
            <span className="set-name set-dd-auto">Auto-place — no primary</span>
            {!primary && <Icon name="check" size={15} color="var(--green)" />}
          </button>
          {candidates.map((a) => {
            const on = a.id === primary
            return (
              <button
                key={a.id}
                type="button"
                role="option"
                aria-selected={on}
                className={`set-dd-opt ${on ? 'on' : ''}`}
                onClick={() => pick(a.id)}
              >
                <span className={`dot ${a.online ? 'live' : 'exited'}`} />
                <span className="set-name">{label(a)}</span>
                <span className="set-meta">{a.online ? `${a.cpuCount} cores` : 'offline'}</span>
                {on && <Icon name="check" size={15} color="var(--green)" />}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

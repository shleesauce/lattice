/* One-click fleet auto-update (v0.1.5 / H). A slim banner appears when the hub
   reports a newer release (/api/releases → updateAvailable). The button opens a
   progress modal that POSTs /api/update — the hub self-updates, cascades every
   agent in LOCKSTEP, then restarts; the dashboard polls /api/health until the new
   build answers and reloads onto it. "What's new" surfaces the Phase-G release
   notes inline so the operator sees the changelog before committing. */
import { useEffect, useRef, useState } from 'react'
import { fetchHealth, fetchReleases, startUpdate } from '../api'
import type { ReleasesResponse, UpdateResult } from '../types'
import { Modal } from './Modal'
import { ReleaseNotes } from './ReleaseNotes'
import { Icon } from '../lattice/Icon'
import { Dot } from '../lattice/primitives'

// Poll /api/releases periodically so the banner appears without a reload once a
// release lands. The hub caches GitHub for 30min, so a light client poll is free.
const RELEASE_POLL_MS = 5 * 60 * 1000

export function UpdateBanner({ currentVersion }: { currentVersion?: string }) {
  const [releases, setReleases] = useState<ReleasesResponse | null>(null)
  const [open, setOpen] = useState(false)
  const [showNotes, setShowNotes] = useState(false)
  const [dismissed, setDismissed] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = () =>
      fetchReleases()
        .then((r) => !cancelled && setReleases(r))
        .catch(() => {})
    void load()
    const t = setInterval(load, RELEASE_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [])

  if (!releases?.updateAvailable) return null
  // Let the operator dismiss a given version's banner for this session.
  if (dismissed === releases.latest) return null

  return (
    <>
      <div className="upd-banner">
        <Icon name="sparkles" size={14} color="var(--teal)" />
        <span className="upd-banner-text">
          Lattice <strong>{releases.latest}</strong> is available
          {currentVersion ? ` — you're on ${currentVersion}` : ''}
        </span>
        <button type="button" className="upd-link" onClick={() => setShowNotes(true)}>
          what's new
        </button>
        <button type="button" className="upd-btn" onClick={() => setOpen(true)}>
          <Icon name="zap" size={13} />
          Update fleet
        </button>
        <button
          type="button"
          className="upd-x"
          onClick={() => setDismissed(releases.latest)}
          aria-label="Dismiss"
        >
          <Icon name="x" size={13} />
        </button>
      </div>

      {showNotes && <ReleaseNotes onClose={() => setShowNotes(false)} />}
      {open && (
        <UpdateProgress
          target={releases.latest}
          current={currentVersion}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  )
}

type Phase = 'confirm' | 'running' | 'agents' | 'reconnecting' | 'error'

function UpdateProgress({
  target,
  current,
  onClose,
}: {
  target: string
  current?: string
  onClose: () => void
}) {
  const [phase, setPhase] = useState<Phase>('confirm')
  const [result, setResult] = useState<UpdateResult | null>(null)
  const [err, setErr] = useState<string>('')
  const pollRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  useEffect(() => () => clearInterval(pollRef.current), [])

  const begin = () => {
    setPhase('running')
    startUpdate()
      .then((res) => {
        setResult(res)
        setPhase('agents')
        // The hub is restarting in the background; wait for the new build to
        // answer /api/health, then reload onto it. Stamp the version we expect so
        // we don't reconnect to the SAME (not-yet-restarted) process and bail early.
        setPhase('reconnecting')
        waitForNewBuild(res.to)
      })
      .catch((e) => {
        setErr(e?.body || e?.message || 'update failed')
        setPhase('error')
      })
  }

  const waitForNewBuild = (to: string) => {
    let tries = 0
    pollRef.current = setInterval(async () => {
      tries++
      try {
        const h = await fetchHealth()
        // The hub answers again AND reports the new version → reload onto it.
        if (sameVersion(h.version, to)) {
          clearInterval(pollRef.current)
          window.location.reload()
        }
      } catch {
        // hub mid-restart; keep polling.
      }
      // Safety valve: after ~60s, reload anyway — the hub is almost certainly back
      // and a stale build string shouldn't trap the operator on the old page.
      if (tries > 60) {
        clearInterval(pollRef.current)
        window.location.reload()
      }
    }, 1000)
  }

  return (
    <Modal width="wide" onClose={phase === 'running' || phase === 'reconnecting' ? () => {} : onClose} ariaLabel="Update Lattice">
      <header className="rn-head" style={{ marginBottom: 14 }}>
        <div className="rn-title">
          <Icon name="zap" size={16} color="var(--teal)" />
          <h3 style={{ margin: 0 }}>Update the fleet</h3>
        </div>
      </header>

      {phase === 'confirm' && (
        <div className="upd-body">
          <p className="upd-msg">
            This updates the <strong>hub</strong> to <strong>{target}</strong>, then every
            online agent in lockstep, then reconnects you to the new build. Each binary is
            checksum-verified before it's swapped.
          </p>
          <div className="upd-actions">
            <button type="button" className="btn btn-ghost" onClick={onClose}>
              Cancel
            </button>
            <button type="button" className="btn btn-primary" onClick={begin}>
              <Icon name="zap" size={13} />
              Update to {target}
            </button>
          </div>
        </div>
      )}

      {phase === 'running' && (
        <div className="upd-body">
          <Step active label={`Updating hub → ${target}`} done={false} />
          <p className="upd-sub">verifying + swapping the hub binary…</p>
        </div>
      )}

      {(phase === 'agents' || phase === 'reconnecting') && (
        <div className="upd-body">
          <Step active={false} done label={`Hub updated → ${result?.to ?? target}`} />
          <div className="upd-agents">
            {result?.agents?.length ? (
              result.agents.map((a) => (
                <div key={a.agentId} className="upd-agent">
                  <Dot status={a.ok ? 'live' : 'danger'} />
                  <span className="upd-agent-name">{a.name || a.agentId}</span>
                  <span className="upd-agent-state">
                    {a.ok ? `updated${a.restarted ? ` · ${a.restarted}` : ''}` : a.error || 'failed'}
                  </span>
                </div>
              ))
            ) : (
              <p className="upd-sub">no online agents to update</p>
            )}
          </div>
          {phase === 'reconnecting' && (
            <p className="upd-sub reconnecting">
              <Icon name="refresh-cw" size={13} style={{ animation: 'spin 1s linear infinite' }} />
              hub restarting — reconnecting to {result?.to ?? target}…
            </p>
          )}
        </div>
      )}

      {phase === 'error' && (
        <div className="upd-body">
          <p className="upd-msg err">
            <Icon name="wifi-off" size={14} color="var(--st-danger)" />
            {err}
          </p>
          <p className="upd-sub">
            {current ? `Still on ${current}. ` : ''}
            Nothing was swapped if verification failed — your fleet is unchanged.
          </p>
          <div className="upd-actions">
            <button type="button" className="btn btn-ghost" onClick={onClose}>
              Close
            </button>
          </div>
        </div>
      )}
    </Modal>
  )
}

function Step({ label, active, done }: { label: string; active: boolean; done: boolean }) {
  return (
    <div className={`upd-step${done ? ' done' : active ? ' active' : ''}`}>
      {done ? (
        <Icon name="check" size={14} color="var(--teal)" />
      ) : (
        <Icon name="refresh-cw" size={14} style={{ animation: 'spin 1s linear infinite' }} />
      )}
      <span>{label}</span>
    </div>
  )
}

// sameVersion mirrors the hub's lenient x.y.z compare (ignore a leading "v").
function sameVersion(a?: string, b?: string): boolean {
  if (!a || !b) return false
  const norm = (s: string) => s.trim().replace(/^v/, '').split(/[-+]/)[0]
  return norm(a) === norm(b)
}

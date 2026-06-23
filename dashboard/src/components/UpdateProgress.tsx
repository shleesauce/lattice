/* One-click fleet auto-update — the progress modal (v0.1.5 / H). POSTs /api/update:
   the hub self-updates, cascades every agent in LOCKSTEP, then restarts; the
   dashboard polls /api/health until the new build answers and reloads onto it.
   This is the canonical update FLOW — UpdateAlert (header chip + banner) opens it;
   do NOT reimplement the update logic elsewhere. */
import { useEffect, useRef, useState } from 'react'
import { fetchHealth, startUpdate } from '../api'
import type { UpdateResult } from '../types'
import { Modal } from './Modal'
import { Icon } from '../lattice/Icon'
import { Dot } from '../lattice/primitives'

type Phase = 'confirm' | 'running' | 'agents' | 'reconnecting' | 'manual' | 'error'

export function UpdateProgress({
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
        // restartRequired: the hub swapped its binary but can't self-restart (pm2 /
        // bare process), so it's STILL on the old code. Polling for the new build
        // would never converge and the safety-valve reload would land us back on the
        // old page — so stop here and tell the operator to restart it by hand.
        if (res.restartRequired) {
          setPhase('manual')
          return
        }
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

      {(phase === 'agents' || phase === 'reconnecting' || phase === 'manual') && (
        <div className="upd-body">
          <Step active={false} done label={`Hub binary updated → ${result?.to ?? target}`} />
          <div className="upd-agents">
            {result?.agents?.length ? (
              result.agents.map((a) => {
                // Tri-state: a timeout / drop is 'pending' (amber), NOT a red failure —
                // the binary still applies on the agent's next start. Fall back to the
                // legacy ok flag for a hub that predates the status field.
                const status = a.status ?? (a.ok ? 'updated' : 'failed')
                const dot = status === 'updated' ? 'live' : status === 'pending' ? 'starting' : 'danger'
                const label =
                  status === 'updated'
                    ? `updated${a.restarted ? ` · ${a.restarted}` : ''}`
                    : status === 'pending'
                      ? a.detail || 'applies on restart'
                      : a.error || a.detail || 'failed'
                return (
                  <div key={a.agentId} className="upd-agent">
                    <Dot status={dot} />
                    <span className="upd-agent-name">{a.name || a.agentId}</span>
                    <span className="upd-agent-state">{label}</span>
                  </div>
                )
              })
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
          {phase === 'manual' && (
            <>
              <p
                className="upd-msg"
                style={{ display: 'flex', alignItems: 'center', gap: 8, color: 'var(--amber)', fontWeight: 600, marginTop: 10 }}
              >
                <Icon name="wifi-off" size={14} color="var(--amber)" />
                Hub restart required — it&apos;s still running the old build.
              </p>
              <p className="upd-sub">
                This hub runs under a manager Lattice can&apos;t restart for you. The new binary is
                in place; restart the hub to finish:
              </p>
              {result?.restartHint && (
                <pre className="upd-hint">{result.restartHint}</pre>
              )}
              <div className="upd-actions">
                <button type="button" className="btn btn-ghost" onClick={onClose}>
                  Close
                </button>
              </div>
            </>
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

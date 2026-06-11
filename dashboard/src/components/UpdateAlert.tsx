/* Version surfacing (v0.1.5). Two coordinated pieces driven by /api/releases:

   • <VersionBadge> — an always-visible mono chip in the header. Up to date it's a
     subtle muted pill that opens "What's new" on click. When the hub reports a newer
     release it transforms into an unmissable teal "Update available → vX" alert (a
     gentle glow pulse) that opens the update flow.
   • <UpdateAlertBanner> — the prominent full-width strip directly under the header
     when an update is available: "You're on {running} — Lattice {latest} is
     available", a primary "Update now" button, a "what's new" link, and a
     per-version session dismiss.

   Both share App's single useReleases() poll (passed in) and both open the EXISTING
   <UpdateProgress> modal — the real update logic is never reimplemented here. */
import { useState } from 'react'
import type { ReleasesState } from '../useReleases'
import { ReleaseNotes } from './ReleaseNotes'
import { UpdateProgress } from './UpdateProgress'
import { Icon } from '../lattice/Icon'

// Always-visible version chip for the header. Subtle when up to date (opens "What's
// new"); a prominent teal alert when an update is available (opens the update flow).
export function VersionBadge({
  version,
  releases,
}: {
  version?: string
  releases: ReleasesState
}) {
  const [showNotes, setShowNotes] = useState(false)
  const [showUpdate, setShowUpdate] = useState(false)
  const updateAvailable = !!releases.data?.updateAvailable
  const latest = releases.data?.latest
  // The last release check failed. We can't claim "up to date" — flag it so the
  // muted chip's tooltip says so instead of silently implying current.
  const checkFailed = releases.error && !updateAvailable

  if (updateAvailable && latest) {
    return (
      <>
        <button
          type="button"
          className="ver-badge alert"
          onClick={() => setShowUpdate(true)}
          title={`Update available — ${version ? `you're on ${version}, ` : ''}${latest} is ready`}
        >
          <span className="ver-badge-pulse" aria-hidden="true" />
          <Icon name="sparkles" size={12} />
          <span>Update available</span>
          <span className="ver-badge-arrow">
            <Icon name="arrow-right" size={11} />
          </span>
          <strong>{latest}</strong>
        </button>
        {showUpdate && (
          <UpdateProgress target={latest} current={version} onClose={() => setShowUpdate(false)} />
        )}
      </>
    )
  }

  if (!version) {
    return <span className="ver-badge muted dash">—</span>
  }

  return (
    <>
      <button
        type="button"
        className="ver-badge muted"
        onClick={() => setShowNotes(true)}
        title={checkFailed ? `Lattice ${version} — couldn't check for updates` : `Lattice ${version} — what's new`}
      >
        {version}
        {checkFailed && <span className="ver-badge-dot" aria-hidden="true" title="couldn't check for updates" />}
      </button>
      {showNotes && <ReleaseNotes onClose={() => setShowNotes(false)} />}
    </>
  )
}

// Prominent full-width strip under the header when an update is available. Shares
// App's single useReleases() poll (passed in) with the header chip.
export function UpdateAlertBanner({
  version,
  releases,
}: {
  version?: string
  releases: ReleasesState
}) {
  const [showNotes, setShowNotes] = useState(false)
  const [showUpdate, setShowUpdate] = useState(false)
  const [dismissed, setDismissed] = useState<string | null>(null)

  if (!releases.data?.updateAvailable) return null
  const latest = releases.data.latest
  // Let the operator dismiss a given version's banner for this session.
  if (dismissed === latest) return null
  const running = releases.data.current || version

  return (
    <>
      <div className="upd-strip">
        <span className="upd-strip-icon" aria-hidden="true">
          <Icon name="sparkles" size={15} color="var(--teal)" />
        </span>
        <span className="upd-strip-text">
          {running ? (
            <>
              You're on <span className="mono">{running}</span> —{' '}
            </>
          ) : null}
          Lattice <strong>{latest}</strong> is available
        </span>
        <button type="button" className="upd-link" onClick={() => setShowNotes(true)}>
          what's new
        </button>
        <button type="button" className="upd-strip-btn" onClick={() => setShowUpdate(true)}>
          <Icon name="zap" size={13} />
          Update now
        </button>
        <button
          type="button"
          className="upd-x"
          onClick={() => setDismissed(latest)}
          aria-label="Dismiss"
        >
          <Icon name="x" size={13} />
        </button>
      </div>

      {showNotes && <ReleaseNotes onClose={() => setShowNotes(false)} />}
      {showUpdate && (
        <UpdateProgress target={latest} current={running} onClose={() => setShowUpdate(false)} />
      )}
    </>
  )
}

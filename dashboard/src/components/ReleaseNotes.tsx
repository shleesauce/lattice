/* Release notes — "What's new" for Lattice. Pulls the recent GitHub releases
   through the hub (/api/releases, cached), flags the running build, and renders
   each release body as markdown. The update BUTTON itself lands in Phase H; this
   panel is read-only history + an honest "you're up to date / vX available" line. */
import { useEffect, useState } from 'react'
import { fetchReleases } from '../api'
import type { ReleaseInfo, ReleasesResponse } from '../types'
import { renderMarkdown } from '../lib/markdown'
import { Modal } from './Modal'
import { Icon } from '../lattice/Icon'

export function ReleaseNotes({ onClose }: { onClose: () => void }) {
  const [data, setData] = useState<ReleasesResponse | null>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')

  useEffect(() => {
    let cancelled = false
    fetchReleases()
      .then((r) => {
        if (cancelled) return
        setData(r)
        setState('ready')
      })
      .catch(() => !cancelled && setState('error'))
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <Modal width="wide" flush onClose={onClose} ariaLabel="Release notes">
      <header className="rn-head">
        <div className="rn-title">
          <Icon name="sparkles" size={16} color="var(--teal)" />
          <h3 style={{ margin: 0 }}>What's new</h3>
        </div>
        {data && <UpdateLine data={data} />}
        <button type="button" className="rn-x" onClick={onClose} aria-label="Close">
          <Icon name="x" size={15} />
        </button>
      </header>

      <div className="rn-body term-scroll">
        {state === 'loading' && <p className="rn-msg">loading release notes…</p>}
        {state === 'error' && (
          <p className="rn-msg err">
            couldn't reach GitHub — release notes are unavailable offline
          </p>
        )}
        {state === 'ready' && data && data.releases.length === 0 && (
          <p className="rn-msg">no releases published yet</p>
        )}
        {state === 'ready' &&
          data?.releases.map((rel) => <ReleaseEntry key={rel.version} rel={rel} />)}
      </div>
    </Modal>
  )
}

function UpdateLine({ data }: { data: ReleasesResponse }) {
  if (data.updateAvailable) {
    return (
      <span className="rn-update on">
        <span className="dot live" />
        {data.latest} available
      </span>
    )
  }
  return (
    <span className="rn-update">
      <Icon name="check" size={13} color="var(--fg-3)" />
      up to date
    </span>
  )
}

function ReleaseEntry({ rel }: { rel: ReleaseInfo }) {
  const date = rel.publishedAt ? new Date(rel.publishedAt).toLocaleDateString() : ''
  return (
    <section className={`rn-rel${rel.current ? ' current' : ''}`}>
      <div className="rn-rel-head">
        <span className="rn-rel-ver">{rel.version}</span>
        {rel.current && <span className="chip cool">you're here</span>}
        {rel.newer && <span className="chip alive">newer</span>}
        {rel.prerelease && <span className="chip">pre-release</span>}
        {date && <span className="rn-rel-date">{date}</span>}
      </div>
      {rel.body ? (
        <div
          className="rn-rel-body prose-transcript text-[13px] leading-relaxed text-zinc-300"
          dangerouslySetInnerHTML={{ __html: renderMarkdown(rel.body) }}
        />
      ) : (
        <p className="rn-msg">no notes for this release</p>
      )}
    </section>
  )
}

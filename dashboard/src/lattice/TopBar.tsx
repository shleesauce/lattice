/* Top bar — view switcher, context status (mesh / breadcrumb), search */
import { Icon } from './Icon'
import { Dot } from './primitives'
import type { Machine } from './data'

export function TopBar({
  view,
  onView,
  fleet,
  activeFile,
}: {
  view: string
  onView: (v: string) => void
  fleet: Machine[]
  activeFile: string
}) {
  const liveCount = fleet.reduce((n, m) => n + m.sessions.filter((s) => s.status === 'live').length, 0)
  const wovenCount = fleet.filter((m) => !m.offline).length
  return (
    <header className="topbar">
      <div className="seg">
        <button className={view === 'fleet' ? 'on' : ''} onClick={() => onView('fleet')}>
          <Icon name="layers" />
          Fleet
        </button>
        <button className={view === 'workspace' ? 'on' : ''} onClick={() => onView('workspace')}>
          <Icon name="terminal" />
          Workspace
        </button>
      </div>

      {view === 'fleet' ? (
        <div className="tb-stat">
          <Dot status="live" />
          <span style={{ color: 'var(--green)' }}>{liveCount} alive</span>
          <span style={{ color: 'var(--fg-3)' }}>·</span>
          <span>{wovenCount} woven</span>
        </div>
      ) : (
        <div className="tb-crumb">
          <Icon name="folder" size={14} color="var(--fg-3)" />
          <b>lattice-core</b>
          <span className="sep">→</span>
          <span style={{ color: 'var(--green)' }}>studio-mbp</span>
          <span className="sep">/</span>
          <b style={{ color: 'var(--amber)' }}>{activeFile}</b>
        </div>
      )}

      <div className="tb-spacer" />

      <div className="tb-search">
        <Icon name="search" size={14} color="var(--fg-3)" />
        <span>Search the mesh</span>
        <span className="mono" style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--fg-3)' }}>
          ⌘K
        </span>
      </div>
      <button className="iconbtn">
        <Icon name="activity" size={17} />
      </button>
      <button className="iconbtn">
        <Icon name="settings" size={17} />
      </button>
    </header>
  )
}

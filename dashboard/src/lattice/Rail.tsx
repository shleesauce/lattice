/* Left rail — context-aware. Fleet view: machines. Workspace view: projects → sessions + devices. */
import { Fragment, useState } from 'react'
import { Icon } from './Icon'
import { Dot } from './primitives'
import { STATUS_LABEL, PROJECTS } from './data'
import type { Machine } from './data'
import logoMark from '../design/logo-mark.svg'

export function Rail({
  view,
  fleet,
  selected,
  activeFile,
  onSelect,
  onSelectFile,
  onStart,
  onWake,
  onGoFleet,
}: {
  view: string
  fleet: Machine[]
  selected: string
  activeFile: string
  onSelect: (id: string) => void
  onSelectFile: (n: string) => void
  onStart: () => void
  onWake: (id: string) => void
  onGoFleet: () => void
}) {
  const woven = fleet.filter((m) => !m.offline).length

  return (
    <aside className="rail">
      <div className="rail-head">
        <img src={logoMark} alt="" />
        <span className="wm">lattice</span>
        <span className="net">
          <Dot status="live" />
          mesh
        </span>
      </div>

      <div className="rail-scroll">
        {view === 'fleet' ? (
          <FleetRail fleet={fleet} selected={selected} onSelect={onSelect} onWake={onWake} />
        ) : (
          <WorkspaceRail activeFile={activeFile} onSelectFile={onSelectFile} onGoFleet={onGoFleet} fleet={fleet} woven={woven} />
        )}
      </div>

      <div className="rail-foot">
        <button className="btn btn-run" style={{ width: '100%', justifyContent: 'center' }} onClick={onStart}>
          <Icon name="plus" />
          New session
        </button>
      </div>
    </aside>
  )
}

function FleetRail({
  fleet,
  selected,
  onSelect,
  onWake,
}: {
  fleet: Machine[]
  selected: string
  onSelect: (id: string) => void
  onWake: (id: string) => void
}) {
  return (
    <>
      <div className="rail-sec">
        Fleet <span className="ct">{fleet.length} machines</span>
      </div>
      {fleet.map((m) => {
        const alive = m.sessions.some((s) => s.status === 'live')
        return (
          <div
            key={m.id}
            className={`mrow ${selected === m.id ? 'sel' : ''} ${m.offline ? 'off' : ''}`}
            onClick={() => onSelect(m.id)}
          >
            <Dot status={m.status} />
            <Icon name={m.kind} size={14} color="var(--fg-3)" />
            <span className="name">{m.label}</span>
            {m.offline ? (
              <button
                className="wake-mini"
                title="Wake machine"
                onClick={(e) => {
                  e.stopPropagation()
                  onWake(m.id)
                }}
              >
                <Icon name="power" size={13} />
              </button>
            ) : alive ? (
              <span className="meta" style={{ color: 'var(--green)' }}>
                {m.sessions.length}●
              </span>
            ) : (
              <span className="meta">{STATUS_LABEL[m.status]}</span>
            )}
          </div>
        )
      })}

      <div className="rail-sec" style={{ marginTop: 6 }}>
        Recent
      </div>
      <div className="mrow">
        <Icon name="folder" size={14} color="var(--fg-3)" />
        <span className="name" style={{ fontWeight: 400, color: 'var(--fg-2)' }}>
          lattice-core
        </span>
      </div>
      <div className="mrow">
        <Icon name="folder" size={14} color="var(--fg-3)" />
        <span className="name" style={{ fontWeight: 400, color: 'var(--fg-2)' }}>
          site
        </span>
      </div>
    </>
  )
}

function WorkspaceRail({
  fleet,
  activeFile,
  onSelectFile,
  onGoFleet,
  woven,
}: {
  fleet: Machine[]
  activeFile: string
  onSelectFile: (n: string) => void
  onGoFleet: () => void
  woven: number
}) {
  const [open, setOpen] = useState<string | null>('lattice-core')
  return (
    <>
      <div className="rail-sec">
        Projects <span className="ct">{PROJECTS.length}</span>
      </div>
      {PROJECTS.map((p) => {
        const isOpen = open === p.id
        return (
          <Fragment key={p.id}>
            <div className={`prow proj ${isOpen ? 'active' : ''}`} onClick={() => setOpen(isOpen ? null : p.id)}>
              <Icon name={isOpen ? 'chevron-down' : 'chevron-right'} size={14} color="var(--fg-3)" />
              <span className="nm">{p.name}</span>
              <span className="loc" style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-3)' }}>
                {p.node}
              </span>
            </div>
            {isOpen && (
              <>
                {p.files.map((f, i) => (
                  <div
                    key={i}
                    className={`prow file ${f.dir ? 'dir' : ''} ${f.n === activeFile ? 'on' : ''}`}
                    style={{ paddingLeft: 26 + f.ind * 14 }}
                    onClick={() => !f.dir && onSelectFile(f.n)}
                  >
                    <Icon name={f.icon} size={13} color={f.dir ? 'var(--fg-3)' : f.warm ? 'var(--amber)' : 'var(--fg-3)'} />
                    <span className="nm">{f.n}</span>
                  </div>
                ))}
                <div className="rail-subsec">Sessions · {p.sessions.length}</div>
                {p.sessions.map((s, i) => (
                  <div className="srow" key={i}>
                    <Dot status={s.status} />
                    <span className="nm">{s.name}</span>
                    <span className="loc">{s.node}</span>
                  </div>
                ))}
              </>
            )}
          </Fragment>
        )
      })}

      <div className="rail-sec" style={{ marginTop: 8 }}>
        Devices <span className="ct">{woven} woven</span>
      </div>
      {fleet.map((m) => (
        <div key={m.id} className={`mrow ${m.offline ? 'off' : ''}`} onClick={onGoFleet}>
          <Dot status={m.status} />
          <Icon name={m.kind} size={14} color="var(--fg-3)" />
          <span className="name" style={{ fontWeight: 400 }}>
            {m.label}
          </span>
          <span className="meta">{m.offline ? 'offline' : STATUS_LABEL[m.status]}</span>
        </div>
      ))}
    </>
  )
}

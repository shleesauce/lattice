/* Control Room — the fleet map + a side panel for the selected machine (with wake action) */
import { Icon } from './Icon'
import { Dot, Chip } from './primitives'
import { FleetMap } from './FleetMap'
import { STATUS_LABEL, fitScore } from './data'
import type { Machine } from './data'

export function Fleet({
  fleet,
  selected,
  onSelect,
  onStart,
  onWake,
  onOpenWorkspace,
}: {
  fleet: Machine[]
  selected: string
  onSelect: (id: string) => void
  onStart: () => void
  onWake: (id: string) => void
  onOpenWorkspace: () => void
}) {
  const m = fleet.find((x) => x.id === selected) || fleet[0]
  const alive = m.sessions.some((s) => s.status === 'live')
  const waking = m.status === 'starting'
  const memPct = m.memTotal ? Math.round((m.memUsed / m.memTotal) * 100) : 0
  const fit = fitScore(m)

  return (
    <div className="cr">
      <FleetMap fleet={fleet} selected={m.id} onSelect={onSelect} />

      <div className="cr-side">
        <div className={`panel ${alive ? 'alive' : ''} ${waking ? 'waking' : ''}`}>
          <div className="panel-h">
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <Dot status={m.status} />
              <Icon name={m.kind} size={16} color="var(--fg-2)" />
              <span className="mono" style={{ fontSize: 15, color: 'var(--fg-1)', fontWeight: 500, whiteSpace: 'nowrap' }}>
                {m.label}
              </span>
            </div>
            <button className="iconbtn">
              <Icon name="ellipsis" size={18} />
            </button>
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {alive ? (
              <Chip kind="alive">
                <Dot status="live" />
                {m.sessions.length} alive
              </Chip>
            ) : (
              <Chip kind={m.offline ? 'dim' : waking ? 'yellow' : 'cool'}>
                <Dot status={m.status} />
                {STATUS_LABEL[m.status]}
              </Chip>
            )}
            <Chip kind="ghost">{m.cores} cores</Chip>
            <Chip kind="ghost">{m.memTotal} GB</Chip>
            <Chip kind="ghost">{m.locLabel}</Chip>
          </div>
        </div>

        {m.offline || waking ? (
          <div className={`wake-card ${waking ? 'is-waking' : ''}`}>
            <div className="ttl">
              <Icon
                name={waking ? 'refresh-cw' : 'power'}
                size={16}
                style={waking ? { animation: 'spin 1s linear infinite' } : {}}
              />
              {waking ? 'Waking ' + m.label + '…' : m.label + ' is offline'}
            </div>
            <p>
              {waking
                ? "Routing wake-on-LAN through the mesh. It'll be woven back in a moment, then ready to place work."
                : 'Last seen 4m ago on the LAN. Wake it to weave it back into the fabric — 24 cores, 128 GB waiting.'}
            </p>
            <button
              className="btn btn-wake"
              style={{ justifyContent: 'center' }}
              disabled={waking}
              onClick={() => onWake(m.id)}
            >
              <Icon name={waking ? 'refresh-cw' : 'power'} style={waking ? { animation: 'spin 1s linear infinite' } : {}} />
              {waking ? 'Waking…' : 'Wake machine'}
            </button>
          </div>
        ) : (
          <div className="panel">
            <div className="panel-h">
              <span className="t">Live metrics</span>
              <Icon name="gauge" size={16} color="var(--fg-3)" />
            </div>
            <div className="metric-grid">
              <div className="metric">
                <div className="lab">CPU · {m.cores} cores</div>
                <div className={`val ${alive ? 'warm' : ''}`}>{m.cpu}%</div>
                <div className="bar">
                  <i className={alive ? 'warm' : ''} style={{ width: m.cpu + '%' }} />
                </div>
              </div>
              <div className="metric">
                <div className="lab">Memory</div>
                <div className="val">{memPct}%</div>
                <div className="bar">
                  <i style={{ width: memPct + '%' }} />
                </div>
              </div>
              <div className="metric">
                <div className="lab">Free RAM</div>
                <div className="val" style={{ fontSize: 18 }}>
                  {(m.memTotal - m.memUsed).toFixed(0)} GB
                </div>
              </div>
              <div className="metric">
                <div className="lab">Throughput</div>
                <div className="val" style={{ fontSize: 18 }}>
                  {m.net}
                </div>
              </div>
            </div>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 9,
                marginTop: 14,
                paddingTop: 13,
                borderTop: '1px solid var(--border)',
              }}
            >
              <span
                className="mono"
                style={{ fontSize: 10, textTransform: 'uppercase', letterSpacing: '.12em', color: 'var(--fg-3)' }}
              >
                Placement fit
              </span>
              <div className="bar" style={{ flex: 1, marginTop: 0 }}>
                <i style={{ width: fit + '%' }} />
              </div>
              <span className="mono" style={{ fontSize: 13, color: 'var(--teal)', fontWeight: 500 }}>
                {fit}
              </span>
            </div>
          </div>
        )}

        {!m.offline && !waking && (
          <div className="panel">
            <div className="panel-h">
              <span className="t">Sessions</span>
              <button className="btn btn-ghost" style={{ padding: '4px 8px', fontSize: 12 }} onClick={onStart}>
                <Icon name="plus" size={13} />
                New
              </button>
            </div>
            {m.sessions.length === 0 ? (
              <div style={{ color: 'var(--fg-3)', fontSize: 13, padding: '6px 2px', lineHeight: 1.5 }}>
                No sessions alive. Pick a machine and start one.
              </div>
            ) : (
              m.sessions.map((s) => (
                <div className="sess" key={s.name}>
                  <Dot status={s.status} />
                  <span className="nm">{s.name}</span>
                  <span className="du">{s.dur}</span>
                  <button className="iconbtn" style={{ width: 26, height: 26 }} onClick={onOpenWorkspace}>
                    <Icon name="arrow-right" size={15} />
                  </button>
                </div>
              ))
            )}
          </div>
        )}

        {!m.offline && !waking && (
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn btn-secondary" style={{ flex: 1, justifyContent: 'center' }} onClick={onOpenWorkspace}>
              <Icon name="terminal" />
              Open
            </button>
            {alive ? (
              <button className="btn btn-danger" style={{ flex: 1, justifyContent: 'center' }}>
                <Icon name="square" />
                Stop
              </button>
            ) : (
              <button className="btn btn-run" style={{ flex: 1, justifyContent: 'center' }} onClick={onStart}>
                <Icon name="play" />
                Start
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

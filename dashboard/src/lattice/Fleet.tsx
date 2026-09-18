/* Control Room — machines rail + the live mesh map + a side panel for the
   selected machine. All real fleet data (no mock). */
import { useEffect, useState } from 'react'
import { Icon } from './Icon'
import { Dot, Chip } from './primitives'
import { FleetMap } from './FleetMap'
import { STATUS_LABEL, fitScore } from './adapt'
import type { Machine } from './adapt'
import type { PowerAction } from '../api'

export function Fleet({
  machines,
  selected,
  recentProjects,
  connLabel,
  canWake,
  onManageMesh,
  onSelect,
  onWake,
  onPower,
  onNewSession,
  onOpenWorkspace,
  onOpenProject,
  onOpenSession,
}: {
  machines: Machine[]
  selected: string
  recentProjects: string[]
  connLabel: string
  canWake: boolean
  onManageMesh: () => void
  onSelect: (id: string) => void
  onWake: (m: Machine) => void
  onPower: (m: Machine, action: PowerAction) => void
  onNewSession: (m: Machine) => void
  onOpenWorkspace: () => void
  // Jump straight into a specific project / session in the Workspace.
  onOpenProject: (name: string) => void
  onOpenSession: (id: string) => void
}) {
  const m = machines.find((x) => x.id === selected) || machines[0]

  return (
    <div className="cr3">
      {/* ── machines rail ── */}
      <aside className="rail">
        <div className="rail-head">
          <span className="wm" style={{ fontSize: 16 }}>Fleet</span>
          <span className="net">
            <Dot status="live" />
            {connLabel}
          </span>
          <button
            type="button"
            className="iconbtn"
            title="Manage mesh"
            aria-label="Manage mesh"
            onClick={onManageMesh}
            style={{ marginLeft: 'auto' }}
          >
            <Icon name="settings" size={15} />
          </button>
        </div>
        <div className="rail-scroll">
          <div className="rail-sec">
            Machines <span className="ct">{machines.length}</span>
          </div>
          {machines.map((mc) => {
            const alive = mc.sessions.some((s) => s.status === 'live')
            const waking = mc.status === 'starting'
            return (
              <div
                key={mc.id}
                className={`mrow ${selected === mc.id ? 'sel' : ''} ${mc.offline ? 'off' : ''}`}
                role="button"
                tabIndex={0}
                aria-pressed={selected === mc.id}
                aria-label={`${mc.label} — ${STATUS_LABEL[mc.status]}`}
                onClick={() => onSelect(mc.id)}
                onKeyDown={(e) => {
                  // Don't hijack keys from a nested control (the wake button): its
                  // Enter/Space bubbles here and this preventDefault() would cancel
                  // the button's native activation, making wake mouse-only.
                  if (e.target !== e.currentTarget) return
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    onSelect(mc.id)
                  }
                }}
              >
                <Dot status={mc.status} />
                <Icon name={mc.kind} size={14} color="var(--fg-3)" />
                <span className="name">{mc.label}</span>
                {mc.offline ? (
                  <button
                    className="wake-mini"
                    title={mc.mac ? 'Wake machine' : 'no known MAC'}
                    onClick={(e) => {
                      e.stopPropagation()
                      onWake(mc)
                    }}
                  >
                    <Icon name="power" size={13} />
                  </button>
                ) : alive ? (
                  <span className="meta" style={{ color: 'var(--green)' }}>
                    {mc.sessions.filter((s) => s.status === 'live').length}●
                  </span>
                ) : (
                  <span className="meta">{waking ? 'waking' : STATUS_LABEL[mc.status]}</span>
                )}
              </div>
            )
          })}

          {recentProjects.length > 0 && (
            <>
              <div className="rail-sec" style={{ marginTop: 6 }}>
                Recent
              </div>
              {recentProjects.map((p) => (
                <div
                  className="mrow"
                  key={p}
                  role="button"
                  tabIndex={0}
                  aria-label={`open ${p}`}
                  onClick={() => onOpenProject(p)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      onOpenProject(p)
                    }
                  }}
                  title={`open ${p}`}
                >
                  <Icon name="folder" size={14} color="var(--fg-3)" />
                  <span className="name" style={{ fontWeight: 400, color: 'var(--fg-2)' }}>
                    {p}
                  </span>
                  <Icon name="arrow-right" size={13} color="var(--fg-3)" style={{ marginLeft: 'auto', opacity: 0.5 }} />
                </div>
              ))}
            </>
          )}
        </div>
        <div className="rail-foot">
          <button
            className="btn btn-run"
            style={{ width: '100%', justifyContent: 'center' }}
            onClick={() => m && onNewSession(m)}
          >
            <Icon name="plus" />
            New session
          </button>
        </div>
      </aside>

      <FleetMap fleet={machines} selected={m?.id ?? ''} onSelect={onSelect} />

      {m && (
        <SidePanel
          m={m}
          canWake={canWake}
          onWake={onWake}
          onPower={onPower}
          onNewSession={onNewSession}
          onOpenWorkspace={onOpenWorkspace}
          onOpenSession={onOpenSession}
        />
      )}
    </div>
  )
}

function SidePanel({
  m,
  canWake,
  onWake,
  onPower,
  onNewSession,
  onOpenWorkspace,
  onOpenSession,
}: {
  m: Machine
  canWake: boolean
  onWake: (m: Machine) => void
  onPower: (m: Machine, action: PowerAction) => void
  onNewSession: (m: Machine) => void
  onOpenWorkspace: () => void
  onOpenSession: (id: string) => void
}) {
  const alive = m.sessions.some((s) => s.status === 'live')
  const waking = m.status === 'starting'
  const memPct = m.memTotal ? Math.round((m.memUsed / m.memTotal) * 100) : 0
  const fit = fitScore(m)

  return (
    <div className="cr-side">
      <div className={`panel ${alive ? 'alive' : ''} ${waking ? 'waking' : ''}`}>
        <div className="panel-h">
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
            <Dot status={m.status} />
            <Icon name={m.kind} size={16} color="var(--fg-2)" />
            <span className="mono" style={{ fontSize: 15, color: 'var(--fg-1)', fontWeight: 500, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {m.label}
            </span>
          </div>
          <PowerMenu m={m} canWake={canWake} onWake={onWake} onPower={onPower} />
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {alive ? (
            <Chip kind="alive">
              <Dot status="live" />
              {m.sessions.filter((s) => s.status === 'live').length} alive
            </Chip>
          ) : (
            <Chip kind={m.offline ? 'dim' : waking ? 'yellow' : 'cool'}>
              <Dot status={waking ? 'starting' : m.status} />
              {waking ? 'waking' : STATUS_LABEL[m.status]}
            </Chip>
          )}
          <Chip kind="ghost">{m.cores} cores</Chip>
          <Chip kind="ghost">{m.memTotal.toFixed(0)} GB</Chip>
          <Chip kind="ghost">{m.locLabel}</Chip>
        </div>
      </div>

      {m.offline || waking ? (
        <div className={`wake-card ${waking ? 'is-waking' : ''}`}>
          <div className="ttl">
            <Icon name={waking ? 'refresh-cw' : 'power'} size={16} style={waking ? { animation: 'spin 1s linear infinite' } : {}} />
            {waking ? `Waking ${m.label}…` : `${m.label} is offline`}
          </div>
          <p>
            {waking
              ? "Routing wake-on-LAN through the mesh. It'll be woven back in a moment, then ready to place work."
              : m.mac
                ? `Last seen offline. Wake it to weave it back into the fabric — ${m.cores} cores, ${m.memTotal.toFixed(0)} GB waiting.`
                : `Offline, and no known MAC address to wake it. Bring it online manually to weave it back in.`}
          </p>
          <button
            className="btn btn-wake"
            style={{ justifyContent: 'center' }}
            disabled={waking || !m.mac || !canWake}
            onClick={() => onWake(m)}
            title={!m.mac ? 'no known MAC' : !canWake ? 'needs an online machine to broadcast' : 'wake over LAN'}
          >
            <Icon name={waking ? 'refresh-cw' : 'power'} style={waking ? { animation: 'spin 1s linear infinite' } : {}} />
            {waking ? 'Waking…' : 'Wake machine'}
          </button>
        </div>
      ) : !m.hasAgent ? (
        <ReachableCard m={m} />
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
                {Math.max(0, m.memTotal - m.memUsed).toFixed(0)} GB
              </div>
            </div>
            <div className="metric">
              <div className="lab">Uptime</div>
              <div className="val" style={{ fontSize: 18 }}>
                {m.uptime}
              </div>
            </div>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 9, marginTop: 14, paddingTop: 13, borderTop: '1px solid var(--border)' }}>
            <span className="mono" style={{ fontSize: 10, textTransform: 'uppercase', letterSpacing: '.12em', color: 'var(--fg-3)' }}>
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

      {!m.offline && !waking && m.hasAgent && (
        <div className="panel">
          <div className="panel-h">
            <span className="t">Sessions</span>
            <button className="btn btn-ghost" style={{ padding: '4px 8px', fontSize: 12 }} onClick={() => onNewSession(m)}>
              <Icon name="plus" size={13} />
              New
            </button>
          </div>
          {m.sessions.length === 0 ? (
            <div style={{ color: 'var(--fg-3)', fontSize: 13, padding: '6px 2px', lineHeight: 1.5 }}>
              No sessions here. Start one to put it to work.
            </div>
          ) : (
            m.sessions.map((s) => (
              <div className="sess" key={s.id}>
                <Dot status={s.status} />
                <span className="nm">{s.name}</span>
                <span className="du">{s.dur}</span>
                <button className="iconbtn" style={{ width: 26, height: 26 }} onClick={() => onOpenSession(s.id)} title="open in workspace">
                  <Icon name="arrow-right" size={15} />
                </button>
              </div>
            ))
          )}
        </div>
      )}

      {!m.offline && !waking && m.hasAgent && (
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="btn btn-secondary" style={{ flex: 1, justifyContent: 'center' }} onClick={onOpenWorkspace}>
            <Icon name="terminal" />
            Open
          </button>
          <button className="btn btn-run" style={{ flex: 1, justifyContent: 'center' }} onClick={() => onNewSession(m)}>
            <Icon name="play" />
            New session
          </button>
        </div>
      )}
    </div>
  )
}

// The four power controls, in escalation order. `confirm` marks the ones that are
// destructive enough to need an explicit yes naming the machine — reboot and
// shutdown drop the box off the mesh and take any unsaved work with it. Wake is
// listed here because the operator thinks of it as "power", but it's a different
// road: WoL relayed by a LAN peer (wakeAgent), not a frame to a sleeping agent.
const POWER_ITEMS: { action: PowerAction | 'wake'; label: string; icon: string; confirm: boolean }[] = [
  { action: 'wake', label: 'Wake', icon: 'zap', confirm: false },
  { action: 'sleep', label: 'Sleep', icon: 'circle-dot', confirm: false },
  { action: 'reboot', label: 'Reboot', icon: 'refresh-cw', confirm: true },
  { action: 'shutdown', label: 'Shut down', icon: 'power', confirm: true },
]

// Consequence copy for the confirm step, so the dialog says what actually happens
// to THIS machine rather than a generic "are you sure?".
const CONFIRM_BODY: Record<'reboot' | 'shutdown', string> = {
  reboot: 'It drops off the mesh, loses any unsaved work and every live session, and re-joins on its own once it boots.',
  shutdown: 'It drops off the mesh, loses any unsaved work and every live session, and can only come back via Wake — or in person.',
}

// PowerMenu — the per-machine power controls (Wake / Sleep / Reboot / Shut down)
// behind one small button in the machine panel's header. Availability is the
// inverse of reachability: Wake is the ONLY thing you can do to an offline box,
// and the only thing you can't do to an online one.
function PowerMenu({
  m,
  canWake,
  onWake,
  onPower,
}: {
  m: Machine
  canWake: boolean
  onWake: (m: Machine) => void
  onPower: (m: Machine, action: PowerAction) => void
}) {
  const [open, setOpen] = useState(false)
  // The action awaiting a confirm; null = showing the plain item list.
  const [pending, setPending] = useState<'reboot' | 'shutdown' | null>(null)

  // Esc backs out one step (confirm → list → closed), only while open.
  useEffect(() => {
    if (!open) return
    const h = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      if (pending) setPending(null)
      else setOpen(false)
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [open, pending])

  const close = () => {
    setOpen(false)
    setPending(null)
  }

  // A power_control frame only reaches a machine whose agent is actually checked
  // in; a Tailscale/SSH-only box has nothing to receive it.
  const canControl = !m.offline && m.hasAgent && !!m.agentId
  const reasonFor = (action: PowerAction | 'wake'): string => {
    if (action === 'wake') {
      if (!m.offline) return 'already online'
      if (!m.mac) return 'no known MAC address'
      if (!canWake) return 'needs an online machine on its subnet to broadcast'
      return 'wake over LAN'
    }
    if (m.offline) return 'offline — wake it first'
    if (!m.hasAgent || !m.agentId) return 'no lattice agent on this machine'
    return `${action} ${m.label}`
  }
  const enabled = (action: PowerAction | 'wake'): boolean =>
    action === 'wake' ? m.offline && !!m.mac && canWake : canControl

  const run = (action: PowerAction | 'wake') => {
    if (action === 'wake') onWake(m)
    else onPower(m, action)
    close()
  }

  return (
    <div className="pwr-wrap">
      <button
        type="button"
        className="iconbtn"
        title="Power"
        aria-label={`Power controls for ${m.label}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => (open ? close() : setOpen(true))}
      >
        <Icon name="power" size={15} />
      </button>

      {open && (
        <>
          <div className="prj-menu-backdrop" onClick={close} />
          <div className="pwr-menu" role="menu" aria-label={`Power — ${m.label}`}>
            {pending ? (
              <div className="pwr-confirm">
                <div className="ttl">
                  <Icon name={pending === 'reboot' ? 'refresh-cw' : 'power'} size={14} />
                  {pending === 'reboot' ? 'Reboot' : 'Shut down'} <span className="nm">{m.label}</span>?
                </div>
                <p>{CONFIRM_BODY[pending]}</p>
                <div className="row">
                  <button type="button" className="btn btn-danger" onClick={() => run(pending)}>
                    {pending === 'reboot' ? 'Reboot' : 'Shut down'}
                  </button>
                  <button type="button" className="btn btn-ghost" onClick={() => setPending(null)}>
                    Cancel
                  </button>
                </div>
              </div>
            ) : (
              POWER_ITEMS.map((it) => (
                <button
                  key={it.action}
                  type="button"
                  role="menuitem"
                  className={`pwr-item${it.confirm ? ' danger' : ''}`}
                  disabled={!enabled(it.action)}
                  title={reasonFor(it.action)}
                  onClick={() => {
                    if (it.confirm) setPending(it.action as 'reboot' | 'shutdown')
                    else run(it.action)
                  }}
                >
                  <Icon name={it.icon} size={14} />
                  {it.label}
                </button>
              ))
            )}
          </div>
        </>
      )}
    </div>
  )
}

// Online device with no lattice agent (Tailscale/SSH-reachable only): show how
// to reach it + how to enroll it, instead of agent-only metrics/sessions.
function ReachableCard({ m }: { m: Machine }) {
  return (
    <div className="wake-card">
      <div className="ttl">
        <Icon name="link" size={16} />
        {m.label} is reachable
      </div>
      <p>
        On the mesh via {m.sources.filter((s) => s !== 'agent').join(' + ') || 'the network'}, but it isn't running the
        lattice agent yet — so it can't host sessions. Install the agent to weave it fully into the fabric.
      </p>
      <div className="metric-grid" style={{ gridTemplateColumns: '1fr', gap: 8 }}>
        {m.sshAlias && (
          <div className="metric">
            <div className="lab">SSH</div>
            <div className="mono" style={{ fontSize: 13, color: 'var(--fg-1)', marginTop: 3 }}>
              ssh {m.sshAlias}
            </div>
          </div>
        )}
        {m.tailscaleIP && (
          <div className="metric">
            <div className="lab">Tailscale</div>
            <div className="mono" style={{ fontSize: 13, color: 'var(--fg-1)', marginTop: 3 }}>
              {m.tailscaleIP}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

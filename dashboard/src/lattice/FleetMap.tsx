/* FleetMap — the control-room map. Cool wireframe lattice; live nodes bloom green and
   breathe; waking nodes pull soft-yellow light along the mesh; warm travel toward active. */
import { useCallback, useEffect, useRef } from 'react'
import { Dot } from './primitives'
import { STATUS_LABEL } from './data'
import type { Machine } from './data'

interface Node {
  m: Machine
  x: number
  y: number
  alive: boolean
  starting: boolean
}
interface MapState {
  nodes: Node[]
  bg: { x: number; y: number }[]
  edges: [number, number][]
  w: number
  h: number
}

export function FleetMap({
  fleet,
  selected,
  onSelect,
}: {
  fleet: Machine[]
  selected: string
  onSelect: (id: string) => void
}) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const stateRef = useRef<MapState>({ nodes: [], bg: [], edges: [], w: 0, h: 0 })
  const fleetRef = useRef(fleet)
  fleetRef.current = fleet
  const selRef = useRef(selected)
  selRef.current = selected

  const layout = useCallback((w: number, h: number) => {
    const S = stateRef.current
    S.w = w
    S.h = h
    S.nodes = fleetRef.current.map((m) => ({
      m,
      x: m.x * w,
      y: m.y * h,
      alive: m.sessions.some((s) => s.status === 'live'),
      starting: m.status === 'starting',
    }))
    const bg: { x: number; y: number }[] = []
    let seed = 11
    const rnd = () => (seed = (seed * 9301 + 49297) % 233280) / 233280
    for (let i = 0; i < 26; i++) bg.push({ x: rnd() * w, y: rnd() * h })
    S.bg = bg
    const edges: [number, number][] = []
    S.nodes.forEach((a, i) => {
      const d = S.nodes
        .map((b, j) => ({ j, dist: Math.hypot(a.x - b.x, a.y - b.y) }))
        .filter((o) => o.j !== i)
        .sort((p, q) => p.dist - q.dist)
        .slice(0, 2)
      d.forEach((o) => {
        if (i < o.j) edges.push([i, o.j])
      })
    })
    S.edges = edges
  }, [])

  useEffect(() => {
    const cvs = canvasRef.current!
    const wrap = wrapRef.current!
    const ctx = cvs.getContext('2d')!
    let raf = 0
    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      const w = wrap.clientWidth
      const h = wrap.clientHeight
      cvs.width = w * dpr
      cvs.height = h * dpr
      cvs.style.width = w + 'px'
      cvs.style.height = h + 'px'
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      layout(w, h)
    }
    resize()
    const ro = new ResizeObserver(resize)
    ro.observe(wrap)

    const COOL = '#2DE2C0'
    const BLUE = '#38BDF8'
    const GREEN = '#2FD98A'
    const YELLOW = '#FFD37A'
    const EXIT = '#4A5560'
    const accentOf = (n: Node) => (n.alive ? GREEN : n.starting ? YELLOW : null)
    const colorFor = (n: Node) =>
      n.m.offline ? EXIT : n.starting ? YELLOW : n.m.status === 'detached' ? BLUE : n.alive ? GREEN : COOL

    const draw = (t: number) => {
      const S = stateRef.current
      const { w, h } = S
      ctx.clearRect(0, 0, w, h)
      const g = ctx.createRadialGradient(w * 0.4, h * 0.45, 0, w * 0.4, h * 0.45, Math.max(w, h) * 0.7)
      g.addColorStop(0, '#070A0E')
      g.addColorStop(1, '#040507')
      ctx.fillStyle = g
      ctx.fillRect(0, 0, w, h)

      // soft bloom behind active (alive=green, waking=yellow) nodes
      S.nodes.forEach((n) => {
        const acc = accentOf(n)
        if (!acc) return
        const pulse = 0.5 + 0.5 * Math.sin(t / (n.starting ? 600 : 1200))
        const r = 70 + pulse * 26
        const rgb = acc === GREEN ? '47,217,138' : '255,211,122'
        const rg = ctx.createRadialGradient(n.x, n.y, 0, n.x, n.y, r)
        rg.addColorStop(0, `rgba(${rgb},${0.16 + pulse * 0.08})`)
        rg.addColorStop(1, `rgba(${rgb},0)`)
        ctx.fillStyle = rg
        ctx.beginPath()
        ctx.arc(n.x, n.y, r, 0, 7)
        ctx.fill()
      })

      // background fabric lines
      ctx.lineWidth = 0.6
      ctx.strokeStyle = 'rgba(45,226,192,0.07)'
      ctx.beginPath()
      for (let i = 0; i < S.bg.length; i++) {
        for (let j = i + 1; j < S.bg.length; j++) {
          const a = S.bg[i]
          const b = S.bg[j]
          if (Math.hypot(a.x - b.x, a.y - b.y) < Math.min(w, h) * 0.26) {
            ctx.moveTo(a.x, a.y)
            ctx.lineTo(b.x, b.y)
          }
        }
      }
      ctx.stroke()
      ctx.fillStyle = 'rgba(56,189,248,0.28)'
      S.bg.forEach((p) => {
        ctx.beginPath()
        ctx.arc(p.x, p.y, 1.3, 0, 7)
        ctx.fill()
      })

      // machine edges + travelling pulse toward active ends
      S.edges.forEach(([i, j]) => {
        const a = S.nodes[i]
        const b = S.nodes[j]
        const aAcc = accentOf(a)
        const bAcc = accentOf(b)
        const active = aAcc || bAcc
        ctx.lineWidth = active ? 1.3 : 0.9
        ctx.strokeStyle = active
          ? active === YELLOW
            ? 'rgba(255,211,122,0.32)'
            : 'rgba(47,217,138,0.34)'
          : 'rgba(45,226,192,0.20)'
        ctx.beginPath()
        ctx.moveTo(a.x, a.y)
        ctx.lineTo(b.x, b.y)
        ctx.stroke()

        if (active && !a.m.offline && !b.m.offline) {
          const activeNode = aAcc ? a : b
          const other = aAcc ? b : a
          const acc = aAcc || bAcc
          const speed = acc === YELLOW ? 700 : 1400
          const prog = (t / speed) % 1
          const px = other.x + (activeNode.x - other.x) * prog
          const py = other.y + (activeNode.y - other.y) * prog
          const rgb = acc === YELLOW ? '255,211,122' : '47,217,138'
          ctx.fillStyle = `rgba(${rgb},${0.9 * (1 - prog) + 0.1})`
          ctx.beginPath()
          ctx.arc(px, py, 2.3, 0, 7)
          ctx.fill()
        }
      })

      // machine nodes
      S.nodes.forEach((n) => {
        const c = colorFor(n)
        const acc = accentOf(n)
        const isSel = selRef.current === n.m.id
        const breath = acc ? 0.5 + 0.5 * Math.sin(t / (n.starting ? 600 : 1200)) : 0
        if (acc) {
          ctx.shadowColor = acc
          ctx.shadowBlur = 16 + breath * 12
        } else if (!n.m.offline) {
          ctx.shadowColor = c
          ctx.shadowBlur = 8
        } else {
          ctx.shadowBlur = 0
        }

        if (isSel) {
          ctx.shadowBlur = 0
          ctx.strokeStyle = acc
            ? acc === YELLOW
              ? 'rgba(255,211,122,0.8)'
              : 'rgba(47,217,138,0.7)'
            : n.m.offline
              ? 'rgba(110,123,132,0.7)'
              : 'rgba(56,189,248,0.7)'
          ctx.lineWidth = 1.3
          ctx.beginPath()
          ctx.arc(n.x, n.y, 16, 0, 7)
          ctx.stroke()
          if (acc) {
            ctx.shadowColor = acc
            ctx.shadowBlur = 16
          }
        }
        const r = (acc ? 6 : 5) + breath * 1.5
        ctx.fillStyle = c
        ctx.globalAlpha = n.m.offline ? 0.6 : 1
        ctx.beginPath()
        ctx.arc(n.x, n.y, r, 0, 7)
        ctx.fill()
        ctx.globalAlpha = 1
        ctx.shadowBlur = 0
        if (n.alive) {
          ctx.fillStyle = '#C9F7E2'
          ctx.beginPath()
          ctx.arc(n.x, n.y, 2.4, 0, 7)
          ctx.fill()
        }
        if (n.starting) {
          ctx.fillStyle = '#FFF0CC'
          ctx.beginPath()
          ctx.arc(n.x, n.y, 2.2, 0, 7)
          ctx.fill()
        }

        // labels
        ctx.font = "12px 'IBM Plex Mono', monospace"
        ctx.textAlign = 'center'
        ctx.fillStyle = n.m.offline ? '#6E7B84' : '#A4B0B8'
        ctx.fillText(n.m.label, n.x, n.y + 26)
        ctx.font = "10px 'IBM Plex Mono', monospace"
        ctx.fillStyle = n.alive ? '#2FD98A' : n.starting ? '#FFD37A' : '#6E7B84'
        ctx.fillText(n.alive ? n.m.sessions[0].name : STATUS_LABEL[n.m.status], n.x, n.y + 40)
      })

      raf = requestAnimationFrame(draw)
    }
    raf = requestAnimationFrame(draw)
    return () => {
      cancelAnimationFrame(raf)
      ro.disconnect()
    }
  }, [layout, fleet])

  const handleClick = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const S = stateRef.current
    const rect = canvasRef.current!.getBoundingClientRect()
    const x = e.clientX - rect.left
    const y = e.clientY - rect.top
    let hit: Node | null = null
    let best = 28
    S.nodes.forEach((n) => {
      const d = Math.hypot(n.x - x, n.y - y)
      if (d < best) {
        best = d
        hit = n
      }
    })
    if (hit) onSelect((hit as Node).m.id)
  }

  const live = fleet.reduce((n, m) => n + m.sessions.filter((s) => s.status === 'live').length, 0)
  const woven = fleet.filter((m) => !m.offline).length
  const waking = fleet.some((m) => m.status === 'starting')

  return (
    <div className="cr-map" ref={wrapRef}>
      <canvas ref={canvasRef} onClick={handleClick} style={{ cursor: 'pointer' }} />
      <div className="cr-overlay">
        <h1>Your mesh</h1>
        <p>
          {woven} machines woven · {live} session{live === 1 ? '' : 's'} alive{waking ? ' · 1 waking' : ''}
        </p>
      </div>
      <div className="cr-legend">
        <span>
          <Dot status="live" />
          alive
        </span>
        <span>
          <Dot status="idle" />
          idle
        </span>
        <span>
          <Dot status="detached" />
          detached
        </span>
        <span>
          <Dot status="starting" />
          waking
        </span>
        <span>
          <Dot status="exited" />
          offline
        </span>
      </div>
    </div>
  )
}

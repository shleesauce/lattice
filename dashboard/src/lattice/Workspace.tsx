/* Workspace — themed editor (tab strip + code + terminal) | draggable divider | Claude chat */
import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'
import { Dot, Chip } from './primitives'

function CodeMesh() {
  const L = (n: number, sel: boolean, ...kids: React.ReactNode[]) => (
    <div key={n} className={sel ? 'selln' : ''}>
      <span className="ln">{n}</span>
      {kids}
    </div>
  )
  return (
    <div className="code">
      {L(1, false, <span className="cm">// place work on the warmest idle machine in the mesh</span>)}
      {L(2, false, <span className="kw">import</span>, <span className="var"> {'{ fleet }'} </span>, <span className="kw">from</span>, <span className="str"> "./fleet"</span>)}
      {L(3, false, '')}
      {L(4, false, <span className="kw">export async </span>, <span className="kw">function </span>, <span className="fn">place</span>, <span className="pn">(</span>, <span className="var">job</span>, <span className="pn">: </span>, <span className="ty">Job</span>, <span className="pn">) {'{'}</span>)}
      {L(5, true, '  ', <span className="kw">const</span>, <span className="var"> node </span>, <span className="pn">= </span>, <span className="fn">pickWarmest</span>, <span className="pn">(</span>, <span className="var">fleet</span>, <span className="pn">.</span>, <span className="fn">idle</span>, <span className="pn">())</span>)}
      {L(6, false, '  ', <span className="kw">if</span>, <span className="pn"> (!</span>, <span className="var">node</span>, <span className="pn">) </span>, <span className="var">node </span>, <span className="pn">= </span>, <span className="var">fleet</span>, <span className="pn">.</span>, <span className="fn">anyReachable</span>, <span className="pn">()</span>)}
      {L(7, false, '  ', <span className="kw">if</span>, <span className="pn"> (!</span>, <span className="var">node</span>, <span className="pn">) </span>, <span className="kw">throw new </span>, <span className="ty">MeshError</span>, <span className="pn">(</span>, <span className="str">"mesh is dark"</span>, <span className="pn">)</span>)}
      {L(8, false, '')}
      {L(9, false, '  ', <span className="cm">// survives sleep + disconnect; reattach from any node</span>)}
      {L(10, false, '  ', <span className="kw">return</span>, <span className="var"> node</span>, <span className="pn">.</span>, <span className="fn">run</span>, <span className="pn">(</span>, <span className="var">job</span>, <span className="pn">{', { '}</span>, <span className="var">surviveSleep</span>, <span className="pn">: </span>, <span className="num">true</span>, <span className="pn">{' })'}</span>, <span className="cur" />)}
      {L(11, false, <span className="pn">{'}'}</span>)}
    </div>
  )
}

function Terminal() {
  return (
    <div className="term">
      <div className="term-h">
        <Dot status="live" />
        <span className="t">build-watcher</span>
        <span className="mono" style={{ fontSize: 11, color: 'var(--fg-3)' }}>
          studio-mbp · 2h 14m
        </span>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 4 }}>
          <button className="iconbtn" style={{ width: 26, height: 26 }}>
            <Icon name="copy" size={14} />
          </button>
          <button className="iconbtn" style={{ width: 26, height: 26 }}>
            <Icon name="maximize-2" size={14} />
          </button>
        </div>
      </div>
      <div className="term-body">
        <span className="pr">studio-mbp</span> ~/lattice-core <span style={{ color: 'var(--teal)' }}>git:(main)</span> $ pnpm watch{'\n'}
        <span className="mut">›</span> watching for changes…{'\n'}
        <span className="ok">✓</span> mesh.ts compiled in 412ms{'\n'}
        <span className="ok">✓</span> placer.ts compiled in 88ms{'\n'}
        <span className="mut">›</span> 2 machines idle · 1 alive · throughput 41.2 GB/s
      </div>
    </div>
  )
}

function Editor({
  activeFile,
  onSelectFile,
  onStart,
}: {
  activeFile: string
  onSelectFile: (n: string) => void
  onStart: () => void
}) {
  const tabs = ['mesh.ts', 'placer.ts', 'fleet.ts']
  return (
    <div className="editor">
      <div className="etabs">
        {tabs.map((t) => (
          <div key={t} className={`etab ${t === activeFile ? 'on' : ''}`} onClick={() => onSelectFile(t)}>
            {t === activeFile && <span className="d" />}
            {t}
            <span className="x">
              <Icon name="x" size={12} />
            </span>
          </div>
        ))}
        <div className="etab etab-spacer">
          <button className="iconbtn" style={{ width: 28, height: 28 }} title="New session" onClick={onStart}>
            <Icon name="plus" size={14} />
          </button>
          <button className="iconbtn" style={{ width: 28, height: 28 }}>
            <Icon name="git-branch" size={14} />
          </button>
        </div>
      </div>
      <CodeMesh />
      <Terminal />
      <div className="editor-status">
        <span className="seg-g">
          <Dot status="live" />
          studio-mbp
        </span>
        <span className="seg-g">
          <Icon name="git-branch" size={12} color="var(--fg-3)" />
          main
        </span>
        <span className="sp" />
        <span>Ln 5, Col 41</span>
        <span>TypeScript</span>
        <span style={{ color: 'var(--green)' }}>↑ 41.2 GB/s</span>
      </div>
    </div>
  )
}

function ToolCall({
  icon,
  label,
  detail,
  status,
}: {
  icon: string
  label: string
  detail?: string
  status: string
}) {
  return (
    <div className="tool">
      <div className="tool-h">
        <Icon name={icon} size={14} />
        <span className="lab" dangerouslySetInnerHTML={{ __html: label }} />
        <span className={`st ${status === 'ok' ? 'ok' : 'run'}`}>
          {status === 'ok' ? (
            <>
              <Icon name="check" size={12} />
              done
            </>
          ) : (
            <>
              <Icon name="refresh-cw" size={12} style={{ animation: 'spin 1s linear infinite' }} />
              running
            </>
          )}
        </span>
      </div>
      {detail && <div className="tool-detail" dangerouslySetInnerHTML={{ __html: detail }} />}
    </div>
  )
}

interface ToolCallData {
  icon: string
  label: string
  status: string
  detail?: string
}
interface Message {
  who: string
  text?: string
  code?: string
  tools?: ToolCallData[]
}

function AIChat({ width }: { width: number }) {
  const [messages, setMessages] = useState<Message[]>([
    {
      who: 'user',
      text: 'Add a fallback in place() — if no machine is idle, hand the job to any reachable node, and only throw if the mesh is fully dark.',
    },
    {
      who: 'ai',
      tools: [
        { icon: 'file-code', label: 'Read <b>mesh.ts</b>', status: 'ok', detail: '11 lines · place() reads fleet.idle()' },
        { icon: 'file-code', label: 'Read <b>fleet.ts</b>', status: 'ok', detail: 'exports idle(), anyReachable(), allDark()' },
      ],
      text: "Got it. fleet.ts already exposes anyReachable(), so I added a last-resort branch and a clearer error. Here's the edit to mesh.ts:",
      code: "  const node = pickWarmest(fleet.idle())\n<span class='add'>+ if (!node) node = fleet.anyReachable()</span>\n<span class='add'>+ if (!node) throw new MeshError(\"mesh is dark\")</span>",
    },
    {
      who: 'ai',
      tools: [
        { icon: 'git-commit-horizontal', label: 'Edited <b>mesh.ts</b>', status: 'ok', detail: "<span class='add'>+2</span> · <span class='del'>−0</span> at lines 6–7" },
        { icon: 'terminal', label: 'Ran <b>pnpm test</b> on studio-mbp', status: 'ok', detail: "<span class='add'>✓</span> 24 passed · 1.8s · survived a tab-sleep mid-run" },
      ],
      text: 'Tests pass. The fallback runs on the warmest reachable node and survives sleep. Want me to place a watcher so it reattaches automatically?',
    },
  ])
  const [val, setVal] = useState('')
  const [thinking, setThinking] = useState(false)
  const bodyRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (bodyRef.current) bodyRef.current.scrollTop = bodyRef.current.scrollHeight
  }, [messages, thinking])

  const send = () => {
    const t = val.trim()
    if (!t) return
    setMessages((m) => [...m, { who: 'user', text: t }])
    setVal('')
    setThinking(true)
    setTimeout(() => {
      setThinking(false)
      setMessages((m) => [
        ...m,
        {
          who: 'ai',
          tools: [
            {
              icon: 'zap',
              label: 'Placed session on <b>studio-mbp</b>',
              status: 'ok',
              detail: 'warmest reachable node · survives sleep + disconnect',
            },
          ],
          text: "On it — running that on studio-mbp now. It'll survive if this tab sleeps; reattach from any node.",
        },
      ])
    }, 1500)
  }

  return (
    <div className="chat" style={{ width, flex: 'none' }}>
      <div className="chat-h">
        <span className="av">
          <Icon name="sparkles" size={14} />
        </span>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span className="nm">Claude</span>
          <span className="on-node">paired on studio-mbp · mesh.ts</span>
        </div>
        <button className="iconbtn" style={{ marginLeft: 'auto' }}>
          <Icon name="ellipsis" size={18} />
        </button>
      </div>

      <div className="chat-body" ref={bodyRef}>
        {messages.map((msg, i) => (
          <div className={`msg ${msg.who}`} key={i}>
            <span className="who">{msg.who === 'ai' ? 'Claude' : 'you'}</span>
            {msg.tools && msg.tools.map((tc, k) => <ToolCall key={k} {...tc} />)}
            {(msg.text || msg.code) && (
              <div className="bubble">
                {msg.text}
                {msg.code && <div className="codeblock" dangerouslySetInnerHTML={{ __html: msg.code }} />}
              </div>
            )}
          </div>
        ))}
        {thinking && (
          <div className="thinking">
            <span className="d" />
            thinking on studio-mbp…
          </div>
        )}
      </div>

      <div className="chat-foot">
        <div className="composer">
          <textarea
            rows={2}
            placeholder="Ask Claude, or describe a change…"
            value={val}
            onChange={(e) => setVal(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                send()
              }
            }}
          />
          <div className="composer-row">
            <div className="left">
              <button className="iconbtn" style={{ width: 28, height: 28 }}>
                <Icon name="folder" size={15} />
              </button>
              <button className="iconbtn" style={{ width: 28, height: 28 }}>
                <Icon name="terminal" size={15} />
              </button>
              <Chip kind="ghost">mesh.ts</Chip>
            </div>
            <button className="send" onClick={send} disabled={!val.trim()}>
              <Icon name="send" />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

export function Workspace({
  activeFile,
  onSelectFile,
  onStart,
}: {
  activeFile: string
  onSelectFile: (n: string) => void
  onStart: () => void
}) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const [chatW, setChatW] = useState(384)
  const [dragging, setDragging] = useState(false)

  useEffect(() => {
    if (!dragging) return
    const onMove = (e: MouseEvent) => {
      const rect = wrapRef.current!.getBoundingClientRect()
      const w = Math.min(Math.max(rect.right - e.clientX, 300), Math.min(620, rect.width - 360))
      setChatW(w)
    }
    const onUp = () => setDragging(false)
    document.body.style.userSelect = 'none'
    document.body.style.cursor = 'col-resize'
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    return () => {
      document.body.style.userSelect = ''
      document.body.style.cursor = ''
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
  }, [dragging])

  return (
    <div className="ws" ref={wrapRef}>
      <Editor activeFile={activeFile} onSelectFile={onSelectFile} onStart={onStart} />
      <div
        className={`divider ${dragging ? 'dragging' : ''}`}
        onMouseDown={() => setDragging(true)}
        title="Drag to resize"
      >
        <span className="grip" />
      </div>
      <AIChat width={chatW} />
    </div>
  )
}

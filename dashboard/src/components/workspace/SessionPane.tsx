import { useEffect, useState } from 'react'
import type { Agent, Session } from '../../types'
import { SessionTerminal } from './SessionTerminal'
import { SessionClaude } from './SessionClaude'
import { SessionEditor } from './SessionEditor'
import { SessionTranscript } from './SessionTranscript'
import { MachineChip } from './MachineChip'
import { showsTranscript } from './sessionMeta'

interface Props {
  session: Session
  agents: Agent[]
  onClose: () => void
  onReconnect: (agentId: string) => void
  onPickMachine: (agentId: string) => void
  // Editor-only (D29): the project's paired Claude session shown beside the
  // editor, and whether the editor's machine even has claude installed.
  pairedClaudeId?: string | null
  editorAgentHasClaude?: boolean
}

// Routes an open session to its pane. Editor sessions get the Cursor-style
// split (VS Code beside its project Claude, D29); terminal/claude keep the
// tabbed pane.
export function SessionPane(props: Props) {
  if (props.session.kind === 'editor') return <EditorPane {...props} />
  return <TabbedPane {...props} />
}

// ───────────────────────── editor pane: code ∣ Claude split ─────────────────
// VS Code (iframe) on the left, the project's Claude chat on the right, with a
// draggable divider. Split open by default (D29); the AI pane can be collapsed.
// Both panes stay mounted across collapse so the iframe never reloads and the
// Claude socket isn't dropped.
function EditorPane({ session, agents, onClose, onReconnect, onPickMachine, pairedClaudeId, editorAgentHasClaude }: Props) {
  const [aiOpen, setAiOpen] = useState(true)
  const [ratio, setRatio] = useState(0.62) // editor's share of the width
  const [dragging, setDragging] = useState(false)
  const [container, setContainer] = useState<HTMLDivElement | null>(null)

  // Track the divider drag at the window level so it keeps working even when the
  // cursor crosses the editor iframe (a transparent overlay swallows iframe
  // events while dragging — see below).
  useEffect(() => {
    if (!dragging) return
    const move = (e: MouseEvent) => {
      if (!container) return
      const r = container.getBoundingClientRect()
      const x = (e.clientX - r.left) / r.width
      setRatio(Math.min(0.82, Math.max(0.28, x)))
    }
    const up = () => setDragging(false)
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
    return () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
    }
  }, [dragging, container])

  const machine = agents.find((a) => a.id === session.agentId)
  const machineName = machine?.hostname || machine?.name || session.agentId

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* header */}
      <div className="flex items-center gap-2 border-b border-zinc-800 bg-zinc-900/50 px-3 py-2">
        <span className="font-display text-xs font-semibold uppercase tracking-wider text-sky-300">editor</span>
        {session.title && <span className="truncate font-mono text-[11px] text-zinc-500">{session.title}</span>}
        <div className="ml-auto flex items-center gap-2">
          {editorAgentHasClaude && (
            <button
              type="button"
              onClick={() => setAiOpen((o) => !o)}
              title={aiOpen ? 'hide Claude' : 'show Claude'}
              className={`flex items-center gap-1 rounded px-2 py-1 font-display text-[11px] font-semibold uppercase tracking-wider transition-colors ${
                aiOpen ? 'bg-emerald-500/15 text-emerald-300' : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              <SparkIcon /> ai
            </button>
          )}
          <MachineChip session={session} agents={agents} onReconnect={onReconnect} onPickMachine={onPickMachine} />
          <button
            type="button"
            onClick={onClose}
            title="close session tab"
            className="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-red-300"
          >
            <CloseIcon />
          </button>
        </div>
      </div>

      {/* split body */}
      <div ref={setContainer} className="relative flex min-h-0 flex-1">
        {/* editor (left, or full width when AI collapsed) */}
        <div className="relative min-w-0" style={{ width: aiOpen ? `${ratio * 100}%` : '100%' }}>
          <div className="absolute inset-0">
            <SessionEditor sessionId={session.id} />
          </div>
        </div>

        {/* divider */}
        <div
          onMouseDown={(e) => {
            e.preventDefault()
            setDragging(true)
          }}
          className={`w-1 shrink-0 cursor-col-resize bg-zinc-800 transition-colors hover:bg-emerald-500/50 ${aiOpen ? '' : 'hidden'}`}
        />

        {/* Claude (right) — kept mounted across collapse so the socket survives */}
        {editorAgentHasClaude && (
          <div className={`relative min-w-0 flex-1 border-l border-zinc-800 ${aiOpen ? '' : 'hidden'}`}>
            <div className="absolute inset-0">
              {pairedClaudeId ? <SessionClaude sessionId={pairedClaudeId} /> : <StartingClaude />}
            </div>
          </div>
        )}

        {/* transparent overlay: swallow iframe mouse events during a drag */}
        {dragging && <div className="absolute inset-0 z-50 cursor-col-resize" />}
      </div>

      {/* code-server present but this machine has no claude */}
      {!editorAgentHasClaude && (
        <NoClaudeNote machine={machineName} />
      )}
    </div>
  )
}

function StartingClaude() {
  return (
    <div className="grid h-full place-items-center bg-zinc-950/40">
      <div className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-wider text-zinc-500">
        <span className="h-1.5 w-1.5 rounded-full bg-amber-400 animate-breathe" />
        starting claude…
      </div>
    </div>
  )
}

function NoClaudeNote({ machine }: { machine: string }) {
  return (
    <div className="border-t border-zinc-800 bg-zinc-950/50 px-3 py-1.5 text-center font-mono text-[10px] text-zinc-600">
      // claude isn't installed on {machine} — editor only
    </div>
  )
}

// ───────────────────────── single-view pane: terminal OR claude ─────────────
// A session is exactly one live process (D35): a claude session is just Claude, a
// terminal session is just a shell. No tab pair — that only duplicated the same
// socket and stole the keystrokes.
function TabbedPane({ session, agents, onClose, onReconnect, onPickMachine }: Props) {
  const isClaude = session.kind === 'claude'
  // A claude session that's no longer a live PTY (exited/archived/trashed/orphaned)
  // renders its SAVED transcript instead of a blank xterm (F16 / fixes F15).
  const transcript = showsTranscript(session)
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b border-zinc-800 bg-zinc-900/50 px-3 py-2">
        <span className="rounded bg-emerald-500/15 px-3 py-1 font-display text-xs font-semibold uppercase tracking-wider text-emerald-300">
          {session.kind}
        </span>
        {session.title && <span className="truncate font-mono text-[11px] text-zinc-500">{session.title}</span>}
        <div className="ml-auto flex items-center gap-2">
          <MachineChip session={session} agents={agents} onReconnect={onReconnect} onPickMachine={onPickMachine} />
          <button
            type="button"
            onClick={onClose}
            title="close session tab"
            className="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-red-300"
          >
            <CloseIcon />
          </button>
        </div>
      </div>

      <div className="relative min-h-0 flex-1">
        {transcript ? (
          <SessionTranscript sessionId={session.id} />
        ) : isClaude ? (
          <SessionClaude sessionId={session.id} />
        ) : (
          <SessionTerminal sessionId={session.id} />
        )}
      </div>
    </div>
  )
}

function CloseIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M6 6l12 12M18 6 6 18" strokeLinecap="round" />
    </svg>
  )
}

function SparkIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5 18 18M18 6l-2.5 2.5M8.5 15.5 6 18" strokeLinecap="round" />
    </svg>
  )
}

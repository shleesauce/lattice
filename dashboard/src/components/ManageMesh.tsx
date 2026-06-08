/* Manage Mesh (Phase 4 / M3) — the first-class surface for running the fleet:
   list/rename/remove machines + their tokens, a guided add-machine flow, and an
   Integrations panel that DETECTS + GUIDES (never installs) SSH / Syncthing /
   Tailscale. Reuses the FirstRunWizard / NewProjectWizard chrome 1:1. */
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  createEnrollToken,
  listEnrollTokens,
  removeAgent,
  renameAgent,
  revokeEnrollToken,
} from '../api'
import type { CreateEnrollTokenResult, Device, EnrollToken } from '../types'
import { parseHubError } from '../lib/hubError'
import { Modal } from './Modal'
import { Icon } from '../lattice/Icon'
import { Dot } from '../lattice/primitives'

interface Props {
  devices: Device[]
  onClose: () => void
  onChanged: () => void
}

type Tab = 'machines' | 'add' | 'integrations'

const TABS: { id: Tab; label: string; icon: string }[] = [
  { id: 'machines', label: 'Machines', icon: 'server' },
  { id: 'add', label: 'Add machine', icon: 'plus' },
  { id: 'integrations', label: 'Integrations', icon: 'link' },
]

export function ManageMesh({ devices, onClose, onChanged }: Props) {
  const [tab, setTab] = useState<Tab>('machines')
  // Carries a freshly-minted token across the Add-machine flow so leaving and
  // returning to the tab keeps the live "waiting to join" indicator alive.
  const [pending, setPending] = useState<PendingJoin | null>(null)

  return (
    <Modal flush width={768} onClose={onClose} ariaLabel="Manage mesh">
        <header className="flex items-center gap-3 border-b border-zinc-800 px-5 py-4">
          <span className="grid h-8 w-8 place-items-center rounded-lg border border-zinc-800 bg-zinc-950">
            <Icon name="layers" size={16} color="var(--green)" />
          </span>
          <div className="min-w-0">
            <h3 className="font-display text-base font-semibold text-zinc-50">Manage mesh</h3>
            <p className="font-mono text-[11px] text-zinc-500">{devices.length} machine{devices.length === 1 ? '' : 's'} known</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="close"
            className="ml-auto grid h-8 w-8 place-items-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            <Icon name="x" size={16} />
          </button>
        </header>

        <div className="flex items-center gap-1 border-b border-zinc-800 px-4 pt-2">
          {TABS.map((t) => {
            const on = tab === t.id
            return (
              <button
                key={t.id}
                type="button"
                onClick={() => setTab(t.id)}
                aria-current={on}
                className={`flex items-center gap-1.5 rounded-t-md border-b-2 px-3 py-2 font-display text-[13px] transition-colors ${
                  on
                    ? 'border-emerald-500 text-zinc-100'
                    : 'border-transparent text-zinc-500 hover:text-zinc-300'
                }`}
              >
                <Icon name={t.icon} size={14} />
                {t.label}
              </button>
            )
          })}
        </div>

        <div className="term-scroll min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {tab === 'machines' ? (
            <MachinesTab
              devices={devices}
              onChanged={onChanged}
              onAddAgent={() => setTab('add')}
            />
          ) : tab === 'add' ? (
            <AddMachineTab devices={devices} pending={pending} setPending={setPending} />
          ) : (
            <IntegrationsTab devices={devices} />
          )}
        </div>
    </Modal>
  )
}

// ───────────────────────────── Machines tab ─────────────────────────────

function MachinesTab({
  devices,
  onChanged,
  onAddAgent,
}: {
  devices: Device[]
  onChanged: () => void
  onAddAgent: () => void
}) {
  if (devices.length === 0) {
    return (
      <div className="grid place-items-center py-12 text-center">
        <p className="font-display text-sm text-zinc-300">No machines yet</p>
        <p className="mt-1 font-mono text-[11px] text-zinc-500">Add your first machine to weave the mesh.</p>
        <button
          type="button"
          onClick={onAddAgent}
          className="mt-4 rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400"
        >
          Add machine
        </button>
      </div>
    )
  }
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        {devices.map((d) => (
          <MachineRow key={d.id} device={d} onChanged={onChanged} onAddAgent={onAddAgent} />
        ))}
      </div>
      <TokensSection onChanged={onChanged} />
    </div>
  )
}

// Join tokens minted by the Add-machine flow. Listed here so they can be revoked
// once a machine is enrolled (or to cut off a leaked token).
function TokensSection({ onChanged }: { onChanged: () => void }) {
  const [tokens, setTokens] = useState<EnrollToken[] | null>(null)
  const [revoking, setRevoking] = useState<string | null>(null)

  const load = () => {
    listEnrollTokens()
      .then(setTokens)
      .catch(() => setTokens([]))
  }
  useEffect(load, [])

  const revoke = async (token: string) => {
    setRevoking(token)
    try {
      await revokeEnrollToken(token)
      load()
      onChanged()
    } catch {
      /* leave the row; a failed revoke is non-destructive */
    } finally {
      setRevoking(null)
    }
  }

  if (tokens === null) {
    return <div className="h-9 animate-pulse rounded-md border border-zinc-800 bg-zinc-950/40" />
  }
  if (tokens.length === 0) return null

  return (
    <div>
      <p className="mb-1.5 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">join tokens</p>
      <div className="space-y-1.5">
        {tokens.map((t) => {
          const active = !t.revokedAt
          return (
            <div
              key={t.token}
              className="flex items-center gap-2 rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-2"
            >
              <Icon name="link" size={13} color="var(--fg-3)" />
              <div className="min-w-0 flex-1">
                <p className="truncate font-display text-[12px] text-zinc-200">{t.label || 'unlabeled'}</p>
                <p className="truncate font-mono text-[10px] text-zinc-600">
                  {t.token.slice(0, 12)}…
                  {t.revokedAt ? ' · revoked' : t.lastUsedAt ? ' · used' : ' · active'}
                </p>
              </div>
              {active ? (
                <button
                  type="button"
                  onClick={() => void revoke(t.token)}
                  disabled={revoking === t.token}
                  className="rounded border border-zinc-800 px-2 py-1 font-mono text-[11px] text-zinc-400 transition-colors hover:border-red-500/40 hover:text-red-300 disabled:opacity-50"
                >
                  {revoking === t.token ? 'revoking…' : 'Revoke'}
                </button>
              ) : (
                <span className="font-mono text-[10px] text-zinc-600">revoked</span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function statusOf(d: Device): string {
  if (!d.online) return 'exited'
  if (d.hasAgent && (d.agentLive ?? true)) return 'idle'
  return 'reachable'
}

function MachineRow({
  device,
  onChanged,
  onAddAgent,
}: {
  device: Device
  onChanged: () => void
  onAddAgent: () => void
}) {
  const [renaming, setRenaming] = useState(false)
  const [draft, setDraft] = useState(device.name)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const saveName = async () => {
    const name = draft.trim()
    if (!name || name === device.name) {
      setRenaming(false)
      setDraft(device.name)
      return
    }
    if (!device.agentId) return
    setBusy(true)
    setError(null)
    try {
      await renameAgent(device.agentId, name)
      setRenaming(false)
      onChanged()
    } catch (e) {
      setError(parseHubError(e, 'rename failed'))
    } finally {
      setBusy(false)
    }
  }

  const doRemove = async () => {
    if (!device.agentId) return
    setBusy(true)
    setError(null)
    try {
      await removeAgent(device.agentId)
      setConfirmRemove(false)
      onChanged()
    } catch (e) {
      setError(parseHubError(e, 'remove failed'))
      setBusy(false)
    }
  }

  return (
    <div className="rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-2.5">
      <div className="flex items-center gap-3">
        <Dot status={statusOf(device)} />
        <Icon name={device.kind || 'monitor'} size={15} color="var(--fg-3)" />
        <div className="min-w-0 flex-1">
          {renaming ? (
            <input
              value={draft}
              autoFocus
              disabled={busy}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  void saveName()
                } else if (e.key === 'Escape') {
                  e.stopPropagation()
                  setRenaming(false)
                  setDraft(device.name)
                }
              }}
              className="w-full max-w-[16rem] rounded-md border border-emerald-500/60 bg-zinc-950 px-2 py-1 font-display text-[13px] text-zinc-100 focus:outline-none"
            />
          ) : (
            <p className="truncate font-display text-[13px] text-zinc-100">{device.name}</p>
          )}
          <p className="font-mono text-[10px] text-zinc-500">
            {[device.os, device.kind].filter(Boolean).join(' · ')}
            {!device.online && ' · offline'}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-1">
          {device.sources.map((s) => (
            <span
              key={s}
              className="rounded border border-zinc-800 bg-zinc-900 px-1.5 py-0.5 font-mono text-[9px] uppercase tracking-[0.1em] text-zinc-400"
            >
              {s}
            </span>
          ))}
        </div>
      </div>

      <div className="mt-2 flex items-center gap-2">
        {device.hasAgent && device.agentId ? (
          renaming ? (
            <>
              <button
                type="button"
                onClick={() => void saveName()}
                disabled={busy}
                className="rounded border border-emerald-500/50 bg-emerald-500/10 px-2 py-1 font-mono text-[11px] text-emerald-300 transition-colors hover:bg-emerald-500/20 disabled:opacity-50"
              >
                {busy ? 'saving…' : 'Save'}
              </button>
              <button
                type="button"
                onClick={() => {
                  setRenaming(false)
                  setDraft(device.name)
                }}
                className="rounded px-2 py-1 font-mono text-[11px] text-zinc-500 transition-colors hover:text-zinc-300"
              >
                Cancel
              </button>
            </>
          ) : confirmRemove ? (
            <div className="flex w-full flex-col gap-1.5">
              <p className="font-mono text-[10px] leading-relaxed text-orange-300/90">
                Remove <span className="text-orange-200">{device.name}</span> from the mesh? A box still holding
                the shared join token can re-enroll and reappear — revoke the token in this tab to stop that.
              </p>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => void doRemove()}
                  disabled={busy}
                  className="rounded border border-red-500/50 bg-red-500/10 px-2 py-1 font-mono text-[11px] text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                >
                  {busy ? 'removing…' : 'Remove'}
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmRemove(false)}
                  className="rounded px-2 py-1 font-mono text-[11px] text-zinc-500 transition-colors hover:text-zinc-300"
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <>
              <button
                type="button"
                onClick={() => {
                  setDraft(device.name)
                  setRenaming(true)
                }}
                className="rounded border border-zinc-800 px-2 py-1 font-mono text-[11px] text-zinc-300 transition-colors hover:border-zinc-700 hover:text-zinc-100"
              >
                Rename
              </button>
              <button
                type="button"
                onClick={() => setConfirmRemove(true)}
                className="rounded border border-zinc-800 px-2 py-1 font-mono text-[11px] text-zinc-400 transition-colors hover:border-red-500/40 hover:text-red-300"
              >
                Remove
              </button>
            </>
          )
        ) : (
          <div className="flex w-full items-center justify-between gap-2">
            <span className="font-mono text-[10px] text-zinc-500">reachable — no agent</span>
            <button
              type="button"
              onClick={onAddAgent}
              className="flex items-center gap-1 rounded border border-zinc-800 px-2 py-1 font-mono text-[11px] text-emerald-300 transition-colors hover:border-emerald-500/40"
            >
              <Icon name="plus" size={12} />
              Add agent
            </button>
          </div>
        )}
      </div>

      {error && <p className="mt-1.5 font-mono text-[10px] text-red-400">{error}</p>}
    </div>
  )
}

// ───────────────────────────── Add machine tab ─────────────────────────────

interface PendingJoin {
  result: CreateEnrollTokenResult
  // Agent-backed device ids present when the token was minted — so a NEW one
  // appearing is the box that just joined.
  baselineIds: string[]
}

function AddMachineTab({
  devices,
  pending,
  setPending,
}: {
  devices: Device[]
  pending: PendingJoin | null
  setPending: (p: PendingJoin | null) => void
}) {
  const [label, setLabel] = useState('')
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const agentIds = useMemo(() => devices.filter((d) => d.hasAgent).map((d) => d.id), [devices])

  // Detect the new agent-backed device that wasn't present when we minted.
  const joined = useMemo(() => {
    if (!pending) return null
    const fresh = devices.find((d) => d.hasAgent && !pending.baselineIds.includes(d.id))
    return fresh ?? null
  }, [devices, pending])

  const create = async () => {
    const l = label.trim()
    if (!l) return
    setCreating(true)
    setError(null)
    try {
      const result = await createEnrollToken(l)
      setPending({ result, baselineIds: agentIds })
    } catch (e) {
      setError(parseHubError(e, 'could not create a join token'))
    } finally {
      setCreating(false)
    }
  }

  const reset = () => {
    setPending(null)
    setLabel('')
    setError(null)
  }

  if (!pending) {
    return (
      <div className="space-y-4">
        <div>
          <h2 className="font-display text-sm font-semibold text-zinc-100">Add a machine to the mesh</h2>
          <p className="mt-1 text-[12px] leading-relaxed text-zinc-400">
            Name the machine, mint a one-time join command, then run it on that box.
          </p>
        </div>
        <Field label="name this machine">
          <input
            value={label}
            autoFocus
            onChange={(e) => setLabel(e.target.value.slice(0, 40))}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && label.trim()) {
                e.preventDefault()
                void create()
              }
            }}
            placeholder="e.g. work-laptop"
            className="w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:border-emerald-500/60 focus:outline-none"
          />
          <p className="mt-1.5 font-mono text-[10px] text-zinc-600">{label.trim().length}/40</p>
        </Field>
        {error && (
          <div className="rounded-md border border-red-500/40 bg-red-500/[0.07] px-3 py-2 font-mono text-[11px] text-red-300">
            {error}
          </div>
        )}
        <button
          type="button"
          onClick={() => void create()}
          disabled={!label.trim() || creating}
          className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {creating ? 'minting…' : 'Create join command'}
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div>
        <h2 className="font-display text-sm font-semibold text-zinc-100">
          Run this on <span className="text-emerald-300">{pending.result.label}</span>
        </h2>
        <p className="mt-1 text-[12px] leading-relaxed text-zinc-400">
          Two steps — paste each in a terminal on that machine.
        </p>
      </div>

      {/* Step 1 — Tailscale */}
      <div className="space-y-1.5">
        <div className="flex items-baseline gap-2">
          <span className="shrink-0 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">Step 1</span>
          <span className="font-display text-[12px] font-semibold text-zinc-200">Get on your network</span>
        </div>
        <p className="font-mono text-[10px] leading-relaxed text-zinc-500">
          Skip this if the machine is already on your Tailscale tailnet or on the same Wi-Fi as this hub.
          Otherwise, this installs Tailscale and signs in — it opens a browser once.
        </p>
        <OneLinerTabs unix={pending.result.tailscaleUnix} windows={pending.result.tailscaleWindows} />
      </div>

      {/* Step 2 — Join Lattice */}
      <div className="space-y-1.5">
        <div className="flex items-baseline gap-2">
          <span className="shrink-0 font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">Step 2</span>
          <span className="font-display text-[12px] font-semibold text-zinc-200">Join Lattice</span>
        </div>
        <p className="font-mono text-[10px] leading-relaxed text-zinc-500">
          Installs the Lattice agent and connects it to this hub.
        </p>
        <OneLinerTabs unix={pending.result.unix} windows={pending.result.windows} />
      </div>

      <div className="rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-2">
        <p className="font-mono text-[10px] uppercase tracking-[0.16em] text-zinc-600">join token</p>
        <div className="mt-1 flex items-center gap-2">
          <code className="min-w-0 flex-1 truncate font-mono text-[11px] text-zinc-300">{pending.result.token}</code>
          <CopyButton text={pending.result.token} label="token" />
        </div>
        <p className="mt-1.5 font-mono text-[10px] leading-relaxed text-zinc-500">
          This token stays valid for more machines — revoke it any time from the Machines tab.
        </p>
      </div>

      {joined ? (
        <div className="flex items-center gap-2.5 rounded-md border border-emerald-500/40 bg-emerald-500/[0.08] px-3 py-2.5">
          <span className="grid h-6 w-6 place-items-center rounded-full border border-emerald-500/40 bg-emerald-500/10">
            <Icon name="check" size={13} color="var(--green)" />
          </span>
          <p className="font-display text-[13px] text-emerald-200">{joined.name} joined the mesh!</p>
        </div>
      ) : (
        <div className="flex items-center gap-2.5 rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-2.5">
          <Icon name="refresh-cw" size={14} color="var(--fg-3)" style={{ animation: 'spin 1.4s linear infinite' }} />
          <p className="font-mono text-[11px] text-zinc-400">Waiting for {pending.result.label} to join…</p>
        </div>
      )}

      <div className="flex items-center gap-2 pt-1">
        <button
          type="button"
          onClick={reset}
          className="rounded-md bg-emerald-500 px-4 py-1.5 font-display text-sm font-semibold text-emerald-950 transition-colors hover:bg-emerald-400"
        >
          {joined ? 'Done' : 'Add another'}
        </button>
        {!joined && (
          <button
            type="button"
            onClick={reset}
            className="rounded-md px-3 py-1.5 font-display text-sm text-zinc-400 transition-colors hover:text-zinc-200"
          >
            Cancel
          </button>
        )}
      </div>
    </div>
  )
}

function OneLinerTabs({ unix, windows }: { unix: string; windows: string }) {
  const [os, setOs] = useState<'unix' | 'windows'>('unix')
  const cmd = os === 'unix' ? unix : windows
  return (
    <div>
      <div className="flex items-center gap-1">
        {(['unix', 'windows'] as const).map((o) => {
          const on = os === o
          return (
            <button
              key={o}
              type="button"
              onClick={() => setOs(o)}
              className={`rounded-md px-2.5 py-1 font-mono text-[11px] transition-colors ${
                on ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {o === 'unix' ? 'macOS / Linux' : 'Windows'}
            </button>
          )
        })}
      </div>
      <div className="mt-1.5 flex items-start gap-2 rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2.5">
        <code className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-zinc-300">
          {cmd}
        </code>
        <CopyButton text={cmd} label="command" />
      </div>
    </div>
  )
}

// ───────────────────────────── Integrations tab ─────────────────────────────

function IntegrationsTab({ devices }: { devices: Device[] }) {
  return (
    <div className="space-y-4">
      <div className="rounded-md border border-zinc-800 bg-zinc-950/60 px-3 py-2 text-[11px] leading-relaxed text-zinc-400">
        <span className="text-zinc-300">Detect &amp; guide only.</span> Lattice never installs or configures
        these — it shows what&apos;s present on each machine and walks you through enabling the rest yourself.
      </div>
      {devices.length === 0 ? (
        <p className="font-mono text-[11px] text-zinc-600">// no machines to inspect</p>
      ) : (
        devices.map((d) => <DeviceIntegrations key={d.id} device={d} />)
      )}
    </div>
  )
}

type PillState = 'on' | 'off' | 'unknown'

function StatusPill({ state, on, off }: { state: PillState; on: string; off: string }) {
  if (state === 'unknown') {
    return (
      <span className="rounded-full border border-zinc-800 bg-zinc-900 px-2 py-0.5 font-mono text-[10px] text-zinc-500">
        — unknown
      </span>
    )
  }
  if (state === 'on') {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 font-mono text-[10px] text-emerald-300">
        <Icon name="check" size={10} />
        {on}
      </span>
    )
  }
  return (
    <span className="rounded-full border border-zinc-700 bg-zinc-900 px-2 py-0.5 font-mono text-[10px] text-zinc-400">
      {off}
    </span>
  )
}

function DeviceIntegrations({ device }: { device: Device }) {
  const isUnix = device.os !== 'windows'
  const sshState: PillState = device.sshAlias ? 'on' : 'off'
  const tsState: PillState = device.tailscaleIP ? 'on' : 'off'
  // Syncthing detection only means anything on an agent box (the agent reports it).
  const syncRunning = device.capabilities?.syncthingRunning
  const syncInstalled = device.capabilities?.syncthingInstalled
  const syncState: PillState = !device.hasAgent
    ? 'unknown'
    : syncRunning
      ? 'on'
      : 'off'

  return (
    <div className="rounded-md border border-zinc-800 bg-zinc-950/40">
      <div className="flex items-center gap-2.5 border-b border-zinc-800/70 px-3 py-2">
        <Icon name={device.kind || 'monitor'} size={14} color="var(--fg-3)" />
        <span className="font-display text-[13px] text-zinc-100">{device.name}</span>
        <span className="font-mono text-[10px] text-zinc-600">{device.os}</span>
      </div>
      <div className="divide-y divide-zinc-800/60">
        <IntegrationRow
          icon="terminal"
          name="SSH"
          pill={
            <StatusPill
              state={sshState}
              on={device.sshAlias ? `ssh ${device.sshAlias}` : 'configured'}
              off="not in ssh config"
            />
          }
          docLabel="OpenSSH"
          docHref="https://www.openssh.com/"
        >
          <p>SSH is provided by the OS — no install from Lattice.</p>
          <ul className="mt-1 list-disc space-y-0.5 pl-4 text-zinc-400">
            <li>macOS / Linux: enable Remote Login (macOS) or run <Code>sudo systemctl enable --now ssh</Code>.</li>
            <li>Windows: add the <Code>OpenSSH Server</Code> optional feature, then start the <Code>sshd</Code> service.</li>
            <li>Add a <Code>Host {device.name}</Code> block to <Code>~/.ssh/config</Code> so it shows here.</li>
          </ul>
        </IntegrationRow>

        <IntegrationRow
          icon="refresh-cw"
          name="Syncthing"
          pill={
            <StatusPill
              state={syncState}
              on={syncRunning ? 'running' : 'installed'}
              off={syncInstalled ? 'installed · stopped' : 'not installed'}
            />
          }
          docLabel="syncthing.net"
          docHref="https://syncthing.net/downloads/"
        >
          <p>Continuous, encrypted folder sync across the mesh. Install it yourself, then pair folders.</p>
          <SyncBrainCallout />
        </IntegrationRow>

        <IntegrationRow
          icon="zap"
          name="Tailscale"
          pill={
            <StatusPill
              state={tsState}
              on={device.tailscaleIP ? device.tailscaleIP : 'on tailnet'}
              off="not on tailnet"
            />
          }
          docLabel="tailscale.com"
          docHref="https://tailscale.com/download"
        >
          <p>Zero-config private network so machines reach each other anywhere.</p>
          <ul className="mt-1 list-disc space-y-0.5 pl-4 text-zinc-400">
            <li>Install from the link, then run <Code>tailscale up</Code>{isUnix ? '' : ' from an elevated prompt'}.</li>
            <li>Sign into the same tailnet on every machine — they appear here automatically.</li>
          </ul>
        </IntegrationRow>
      </div>
    </div>
  )
}

function SyncBrainCallout() {
  return (
    <div className="mt-2 rounded-md border border-emerald-500/30 bg-emerald-500/[0.05] px-3 py-2.5">
      <div className="flex items-center gap-1.5">
        <Icon name="sparkles" size={13} color="var(--green)" />
        <span className="font-display text-[12px] font-semibold text-emerald-200">Sync your brain</span>
      </div>
      <p className="mt-1 text-[11px] leading-relaxed text-zinc-400">
        Replicate the folders you choose — brain, AI, projects — across every machine so they stay in lockstep.
      </p>
      <ol className="mt-1.5 list-decimal space-y-0.5 pl-4 text-[11px] leading-relaxed text-zinc-400">
        <li>Open the Syncthing web UI on each machine.</li>
        <li>On the first machine, <span className="text-zinc-300">Add Folder</span> for the folder you want shared.</li>
        <li>Add the other machines as devices, then share that folder with them.</li>
        <li>Accept the share on each machine and pick a local path. Repeat per folder.</li>
      </ol>
      <a
        href="http://127.0.0.1:8384"
        target="_blank"
        rel="noreferrer"
        className="mt-2 inline-flex items-center gap-1 font-mono text-[11px] text-emerald-300 transition-colors hover:text-emerald-200"
      >
        <Icon name="link" size={11} />
        open Syncthing UI · 127.0.0.1:8384
      </a>
    </div>
  )
}

function IntegrationRow({
  icon,
  name,
  pill,
  docLabel,
  docHref,
  children,
}: {
  icon: string
  name: string
  pill: React.ReactNode
  docLabel: string
  docHref: string
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  return (
    <div className="px-3 py-2">
      <div className="flex items-center gap-2.5">
        <Icon name={icon} size={14} color="var(--fg-3)" />
        <span className="font-display text-[12px] text-zinc-200">{name}</span>
        {pill}
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="ml-auto flex items-center gap-1 font-mono text-[11px] text-zinc-500 transition-colors hover:text-zinc-300"
        >
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} />
          How to enable
        </button>
      </div>
      {open && (
        <div className="mt-2 space-y-1.5 border-l border-zinc-800 pl-3 text-[11px] leading-relaxed text-zinc-400">
          {children}
          <a
            href={docHref}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 font-mono text-[11px] text-emerald-300 transition-colors hover:text-emerald-200"
          >
            <Icon name="link" size={11} />
            {docLabel}
          </a>
        </div>
      )}
    </div>
  )
}

// ───────────────────────────── shared pieces ─────────────────────────────

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1.5 block font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">{label}</label>
      {children}
    </div>
  )
}

function Code({ children }: { children: React.ReactNode }) {
  return <code className="rounded bg-zinc-900 px-1 py-0.5 font-mono text-[10px] text-zinc-300">{children}</code>
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  const markCopied = () => {
    setCopied(true)
    clearTimeout(timer.current)
    timer.current = setTimeout(() => setCopied(false), 1600)
  }
  const copy = () => {
    // navigator.clipboard is undefined on insecure origins (plain http over a
    // tailnet — a normal Lattice deployment), so guard it and fall back to the
    // legacy execCommand path instead of throwing.
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(markCopied).catch(legacyCopy)
    } else {
      legacyCopy()
    }
    function legacyCopy() {
      try {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
        markCopied()
      } catch {
        // give up silently — nothing actionable for the user
      }
    }
  }
  return (
    <button
      type="button"
      onClick={copy}
      aria-label={`copy ${label}`}
      className={`flex shrink-0 items-center gap-1 rounded border px-2 py-1 font-mono text-[10px] transition-colors ${
        copied
          ? 'border-emerald-500/50 bg-emerald-500/10 text-emerald-300'
          : 'border-zinc-800 text-zinc-400 hover:border-zinc-700 hover:text-zinc-200'
      }`}
    >
      <Icon name={copied ? 'check' : 'copy'} size={11} />
      {copied ? 'copied' : 'copy'}
    </button>
  )
}

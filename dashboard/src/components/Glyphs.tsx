interface IconProps {
  className?: string
}

export function OSGlyph({ os, className = 'h-4 w-4' }: { os: string; className?: string }) {
  const k = os.toLowerCase()
  if (k.includes('darwin') || k.includes('mac')) return <AppleGlyph className={className} />
  if (k.includes('win')) return <WindowsGlyph className={className} />
  return <LinuxGlyph className={className} />
}

function AppleGlyph({ className }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <path d="M16.36 12.6c-.02-2.18 1.78-3.23 1.86-3.28-1.01-1.48-2.59-1.69-3.15-1.71-1.34-.14-2.62.79-3.3.79-.68 0-1.73-.77-2.84-.75-1.46.02-2.81.85-3.56 2.16-1.52 2.63-.39 6.52 1.09 8.66.72 1.05 1.58 2.22 2.71 2.18 1.09-.04 1.5-.7 2.82-.7 1.31 0 1.68.7 2.83.68 1.17-.02 1.91-1.07 2.62-2.12.83-1.22 1.17-2.4 1.19-2.46-.03-.01-2.28-.88-2.3-3.48zM14.2 5.94c.6-.73 1.01-1.74.9-2.75-.87.04-1.92.58-2.54 1.31-.56.64-1.05 1.67-.92 2.65.97.08 1.96-.49 2.56-1.21z" />
    </svg>
  )
}

function WindowsGlyph({ className }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <path d="M3 5.4 10.3 4.4v6.9H3V5.4zM11.2 4.3 21 3v8.3h-9.8V4.3zM3 12.2h7.3v6.9L3 18.1v-5.9zM11.2 12.2H21V21l-9.8-1.3v-7.5z" />
    </svg>
  )
}

function LinuxGlyph({ className }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <path d="M12 2c-2.1 0-3.3 1.9-3.3 4.3 0 1.4.5 2.2.5 3.4 0 1-.7 1.8-1.5 3-.9 1.3-2 2.7-2 4.6 0 .9.4 1.6 1.1 2 .3.6 1 1 1.9 1 .7 0 1.3-.3 1.7-.8.5.2 1 .3 1.6.3s1.1-.1 1.6-.3c.4.5 1 .8 1.7.8.9 0 1.6-.4 1.9-1 .7-.4 1.1-1.1 1.1-2 0-1.9-1.1-3.3-2-4.6-.8-1.2-1.5-2-1.5-3 0-1.2.5-2 .5-3.4C15.3 3.9 14.1 2 12 2zm-1.4 4.1c.4 0 .7.4.7.9s-.3.9-.7.9-.7-.4-.7-.9.3-.9.7-.9zm2.8 0c.4 0 .7.4.7.9s-.3.9-.7.9-.7-.4-.7-.9.3-.9.7-.9zM12 9.5c.8 0 1.7.5 1.7 1 0 .3-.8.7-1.7.7s-1.7-.4-1.7-.7c0-.5.9-1 1.7-1z" />
    </svg>
  )
}

export function Meter({ value, danger }: { value: number; danger?: boolean }) {
  const v = Math.max(0, Math.min(100, value || 0))
  const hot = danger ?? v >= 90
  const warm = v >= 75
  const color = hot ? 'bg-red-500' : warm ? 'bg-amber-400' : 'bg-emerald-400'
  return (
    <div className="h-1 w-full overflow-hidden rounded-full bg-zinc-800">
      <div className={`h-full rounded-full ${color} transition-[width] duration-500`} style={{ width: `${v}%` }} />
    </div>
  )
}

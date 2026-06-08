/* Icon system — inline SVGs from src/design/icons, stripped of fixed width/height
   so they scale to the requested size and inherit currentColor. */
import type { CSSProperties } from 'react'

const modules = import.meta.glob('../design/icons/*.svg', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const ICONS: Record<string, string> = {}
for (const path in modules) {
  const name = path.split('/').pop()!.replace('.svg', '')
  ICONS[name] = modules[path].replace(/\swidth="24"/, '').replace(/\sheight="24"/, '')
}

export function Icon({
  name,
  size = 15,
  color,
  style = {},
}: {
  name: string
  size?: number
  color?: string
  style?: CSSProperties
}) {
  return (
    <span
      aria-hidden="true"
      className="ic"
      style={{
        display: 'inline-flex',
        flex: 'none',
        width: size,
        height: size,
        color: color || 'currentColor',
        ...style,
      }}
      dangerouslySetInnerHTML={{ __html: ICONS[name] || '' }}
    />
  )
}

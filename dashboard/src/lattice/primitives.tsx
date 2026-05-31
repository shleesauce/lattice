/* Shared primitives — status dot, chip, button. */
import type { CSSProperties, ReactNode } from 'react'
import { Icon } from './Icon'

export function Dot({ status }: { status: string }) {
  return <span className={`dot ${status}`} />
}

export function Chip({ kind = 'ghost', children }: { kind?: string; children: ReactNode }) {
  return <span className={`chip ${kind}`}>{children}</span>
}

export function Btn({
  variant = 'secondary',
  icon,
  children,
  onClick,
  disabled,
  style,
}: {
  variant?: string
  icon?: string
  children?: ReactNode
  onClick?: () => void
  disabled?: boolean
  style?: CSSProperties
}) {
  return (
    <button className={`btn btn-${variant}`} onClick={onClick} disabled={disabled} style={style}>
      {icon && <Icon name={icon} />}
      {children}
    </button>
  )
}

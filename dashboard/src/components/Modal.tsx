import type { ReactNode } from 'react'
import { useEscape } from '../useEscape'

interface ModalProps {
  onClose: () => void
  children: ReactNode
  // Card width: omit for the standard 480px modal, 'wide' for 560px, or a pixel
  // number for a custom width (capped to the viewport so it stays responsive).
  width?: number | 'wide'
  // Drop the card's default padding and make it a flex column with hidden overflow
  // — for structured modals that own their header / body / internal scroll (e.g.
  // ManageMesh's tabbed layout).
  flush?: boolean
  ariaLabel?: string
  className?: string
}

// The one floating-dialog shell for the app, built on the canonical Living Mesh
// .scrim/.modal chrome (the prototype design contract). Owns the scrim, the
// centered card, click-outside-to-close (mousedown, so a text drag that starts
// inside and releases on the scrim doesn't dismiss), Esc-to-close, and the dialog
// a11y role. Callers supply only their own content.
export function Modal({ onClose, children, width, flush, ariaLabel, className }: ModalProps) {
  useEscape(onClose)
  const cls = ['modal']
  if (width === 'wide') cls.push('wide')
  if (flush) cls.push('flush')
  if (className) cls.push(className)
  const style = typeof width === 'number' ? { width, maxWidth: '100%' } : undefined
  return (
    <div className="scrim" onMouseDown={onClose}>
      <div
        className={cls.join(' ')}
        style={style}
        onMouseDown={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={ariaLabel}
      >
        {children}
      </div>
    </div>
  )
}

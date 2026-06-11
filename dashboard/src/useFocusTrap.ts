import { useEffect, useRef } from 'react'
import type { RefObject } from 'react'

// The standard "user can land here with Tab" selector. [tabindex="-1"] is
// programmatically focusable but NOT tab-reachable, so it's excluded.
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

// Keeps Tab/Shift+Tab inside the container while `active`, so focus can't escape
// behind a modal scrim to the page underneath. On open it pulls focus in (unless
// something inside already has it — the command palette focuses its own input);
// on close it restores focus to whatever was focused before. Mirrors the tiny
// single-purpose style of useEscape.
export function useFocusTrap(active: boolean): RefObject<HTMLDivElement> {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!active) return
    const container = ref.current
    if (!container) return
    const prev = document.activeElement as HTMLElement | null

    const focusables = () => Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE))

    // Move focus in — but don't steal it from a child that's already focused
    // (the palette's own rAF puts the caret in its input first).
    if (!container.contains(document.activeElement)) {
      const first = focusables()[0]
      ;(first ?? container).focus()
    }

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return
      const items = focusables()
      if (items.length === 0) {
        // Nothing tabbable inside — keep focus pinned to the container itself.
        e.preventDefault()
        container.focus()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const onEdge = e.shiftKey ? document.activeElement === first : document.activeElement === last
      // Also catch focus that has somehow slipped outside the container.
      if (onEdge || !container.contains(document.activeElement)) {
        e.preventDefault()
        ;(e.shiftKey ? last : first).focus()
      }
    }

    container.addEventListener('keydown', onKeyDown)
    return () => {
      container.removeEventListener('keydown', onKeyDown)
      prev?.focus?.()
    }
  }, [active])
  return ref
}

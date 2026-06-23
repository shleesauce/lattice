import { useEffect } from 'react'

// Calls onClose when Escape is pressed anywhere in the window. Shared by the
// modal/panel components that close on Escape (the element-level dialogs that
// also juggle Enter/stopPropagation keep their own inline handlers).
export function useEscape(onClose: () => void): void {
  useEffect(() => {
    const h = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])
}

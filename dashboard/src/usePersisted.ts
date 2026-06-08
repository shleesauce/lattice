import { useEffect, useState, type Dispatch, type SetStateAction } from 'react'

// useState that survives a page refresh by mirroring to localStorage. Same API
// as useState so it's a drop-in. JSON-serializable state only. A corrupt/blocked
// store degrades to the initial value rather than throwing.
export function usePersisted<T>(key: string, initial: T): [T, Dispatch<SetStateAction<T>>] {
  const [state, setState] = useState<T>(() => {
    try {
      const raw = localStorage.getItem(key)
      return raw != null ? (JSON.parse(raw) as T) : initial
    } catch {
      return initial
    }
  })

  useEffect(() => {
    try {
      localStorage.setItem(key, JSON.stringify(state))
    } catch {
      /* private mode / quota — persistence is best-effort */
    }
  }, [key, state])

  return [state, setState]
}

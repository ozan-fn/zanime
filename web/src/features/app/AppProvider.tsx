import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { Sub } from '@/lib/types'

// Pesan status: satu slot global karena semua fitur memakainya.
type StatusKind = 'info' | 'ok' | 'error'
export type StatusMessage = { text: string; kind: StatusKind } | null

// Playing: stream yang sedang diputar (src = URL siap src player).
export type Playing = {
  src: string
  title?: string
  sub?: string
  subs?: Sub[]
}

type AppContextValue = {
  status: StatusMessage
  setStatus: (text: string, kind?: StatusKind) => void
  clearStatus: () => void
  playing: Playing | null
  play: (p: Playing) => void
  stop: () => void
}

const AppContext = createContext<AppContextValue | null>(null)

export function useApp(): AppContextValue {
  const ctx = useContext(AppContext)
  if (!ctx) throw new Error('useApp harus dipakai di dalam AppProvider')
  return ctx
}

export function AppProvider({ children }: { children: ReactNode }) {
  const [status, setStatusRaw] = useState<StatusMessage>(null)
  const [playing, setPlaying] = useState<Playing | null>(null)

  const setStatus = useCallback((text: string, kind: StatusKind = 'info') => {
    setStatusRaw({ text, kind })
  }, [])
  const clearStatus = useCallback(() => setStatusRaw(null), [])

  const play = useCallback((p: Playing) => {
    if (p && p.src) setPlaying(p)
  }, [])
  const stop = useCallback(() => setPlaying(null), [])

  const value = useMemo<AppContextValue>(
    () => ({ status, setStatus, clearStatus, playing, play, stop }),
    [status, setStatus, clearStatus, playing, play, stop],
  )
  return <AppContext.Provider value={value}>{children}</AppContext.Provider>
}

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react'
import type { ReactNode } from 'react'
import { api } from '@/lib/api'
import type { TorrentState } from '@/lib/types'

// Pesan status kartu magnet: satu slot global karena semua fitur memakainya.
type StatusKind = 'info' | 'ok' | 'error'
export type StatusMessage = { text: string; kind: StatusKind } | null

type StatusSource = 'ws' | 'poll' | 'reconnect' | null

type TorrentContextValue = {
  state: TorrentState | null
  status: StatusMessage
  statusSource: StatusSource
  loadBusy: boolean
  // directPlaying: sedang memutar stream direct (/fetch), bukan HLS torrent.
  directPlaying: boolean
  // directSrc: sedang memutar stream direct (/fetch), bukan HLS torrent.
  directSrc: string
  directInfo: { title?: string; sub?: string } | null
  setDirect: (src: string, info?: { title?: string; sub?: string }) => void
  unsetDirect: () => void
  setStatus: (text: string, kind?: StatusKind) => void
  clearStatus: () => void
  applyState: (state: TorrentState) => void
  selectFile: (index: number) => Promise<void>
  loadMagnet: (body: { magnet: string; fileIndex?: number }, onProgress?: (state: TorrentState) => void) => Promise<TorrentState>
  cancelLoad: () => Promise<void>
}

const TorrentContext = createContext<TorrentContextValue | null>(null)

export function useTorrent(): TorrentContextValue {
  const ctx = useContext(TorrentContext)
  if (!ctx) throw new Error('useTorrent harus dipakai di dalam TorrentStateProvider')
  return ctx
}

export function TorrentStateProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<TorrentState | null>(null)
  const [status, setStatusRaw] = useState<StatusMessage>(null)
  const [statusSource, setStatusSource] = useState<StatusSource>(null)
  const [loadBusy, setLoadBusy] = useState(false)
  const [directSrc, setDirectSrc] = useState('')
  const [directInfo, setDirectInfo] = useState<{ title?: string; sub?: string } | null>(null)
  // loadPending: request /api/magnet dari tab ini masih berjalan.
  const loadPending = useRef(false)
  const magnetAbort = useRef<AbortController | null>(null)
  const wsLive = useRef(false)

  const setStatus = useCallback((text: string, kind: StatusKind = 'info') => {
    setStatusRaw({ text, kind })
  }, [])
  const clearStatus = useCallback(() => setStatusRaw(null), [])

  const applyState = useCallback((next: TorrentState) => {
    setState(next)
    setLoadBusy(loadPending.current || next.loading === true)
  }, [])

  const setDirect = useCallback((src: string, info?: { title?: string; sub?: string }) => {
    setDirectSrc(src)
    setDirectInfo(src ? info ?? null : null)
  }, [])
  const unsetDirect = useCallback(() => {
    setDirectSrc('')
    setDirectInfo(null)
  }, [])

  const selectFile = useCallback(async (index: number) => {
    applyState(await api('/api/select', {
      method: 'POST',
      body: JSON.stringify({ index }),
    }))
  }, [applyState])

  /* Memuat magnet = satu job di server.
   *   - selama job jalan, tombol Muat mati dan tombol Batalkan muncul; status
   *     ini ikut state server, jadi tab lain / halaman yang di-reload ikut benar
   *   - klik Batalkan: request tab ini di-abort, lalu POST /api/cancel yang
   *     menghentikan job-nya di server (torrent yang sedang aktif tidak diusir)
   *   - spam klik tidak menumpuk job: request kedua diabaikan selama request
   *     pertama dari tab ini masih jalan */
  const loadMagnet = useCallback(async (body: { magnet: string; fileIndex?: number }, onProgress?: (s: TorrentState) => void) => {
    if (loadPending.current) throw new Error('busy')
    loadPending.current = true
    magnetAbort.current = new AbortController()
    setLoadBusy(true)
    try {
      const next = await api('/api/magnet', {
        method: 'POST',
        body: JSON.stringify(body),
        signal: magnetAbort.current.signal,
      })
      applyState(next)
      onProgress?.(next)
      return next
    } finally {
      loadPending.current = false
      magnetAbort.current = null
      setLoadBusy(false)
    }
  }, [applyState])

  const cancelLoad = useCallback(async () => {
    loadPending.current = false
    magnetAbort.current?.abort()
    magnetAbort.current = null
    setLoadBusy(false)
    // Server membalas status terbaru; frame WS berikutnya menyusul sendiri.
    applyState(await api('/api/cancel', { method: 'POST' }))
  }, [applyState])

  // Cadangan kalau WebSocket diblokir reverse proxy (Upgrade dibuang): status
  // tetap terisi lewat /api/state tiap 2 dtk. Polling berhenti sendiri begitu
  // ada frame WS yang masuk.
  useEffect(() => {
    const poll = setInterval(async () => {
      if (wsLive.current) { clearInterval(poll); return }
      try {
        applyState(await api('/api/state'))
        setStatusSource('poll')
      } catch (err) {
        console.error('polling status gagal', err)
      }
    }, 2000)
    return () => clearInterval(poll)
  }, [applyState, setStatusSource])

  useEffect(() => {
    let socket: WebSocket | null = null
    let timer: ReturnType<typeof setTimeout> | null = null
    let disposed = false

    const connect = () => {
      const scheme = location.protocol === 'https:' ? 'wss' : 'ws'
      socket = new WebSocket(`${scheme}://${location.host}/api/ws`)
      socket.addEventListener('message', (event) => {
        // Tandai live dulu: frame yang isinya rusak tidak boleh membuat panel
        // terlihat "menyambung" terus.
        wsLive.current = true
        setStatusSource('ws')
        try {
          applyState(JSON.parse(event.data))
        } catch (err) {
          console.error('frame status gagal diterapkan', err)
        }
      })
      socket.addEventListener('close', () => {
        wsLive.current = false
        if (disposed) return
        setStatusSource('reconnect')
        timer = setTimeout(connect, 1000)
      })
      socket.addEventListener('error', () => socket?.close())
    }
    connect()
    return () => {
      disposed = true
      if (timer) clearTimeout(timer)
      socket?.close()
    }
  }, [applyState])

  const directPlaying = directSrc !== ''

  return (
    <TorrentContext.Provider value={{
      state, status, statusSource, loadBusy, directPlaying,
      directSrc, directInfo, setDirect, unsetDirect,
      setStatus, clearStatus, applyState, selectFile, loadMagnet, cancelLoad,
    }}>
      {children}
    </TorrentContext.Provider>
  )
}

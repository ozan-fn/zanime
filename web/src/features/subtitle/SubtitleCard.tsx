import { useCallback, useEffect, useRef, useState } from 'react'
import { Captions, RefreshCw } from 'lucide-react'
import { api } from '@/lib/api'
import { log } from '@/lib/logs'
import type { Subtitle } from '@/lib/types'

/* Kartu pencarian subtitle di bawah player (di atas panel log). Sumbernya
 * backend `/api/subtitles` yang mencari ulang ke subdl tiap kali dipanggil —
 * jadi tautan unduh yang dipakai adalah hasil resolve halaman terbaru, bukan
 * cache lama. Klik "pakai" → track VTT (`/api/subtitles/file`) langsung
 * dipasang lewat onUse. */

export type SubtitleMedia = {
  tab: 'movie' | 'tv'
  id: string
  name: string
  year?: string
  season?: number
  episode?: number
}

// Preferensi auto-pasang, sama dengan subTracks: Indonesia dulu, lalu Inggris.
const ID_RE = /indonesia|\bid\b|\bind\b/i
const EN_RE = /english|\ben\b|\beng\b/i

const preferred = (subs: Subtitle[]) =>
  subs.find((s) => ID_RE.test(`${s.language} ${s.lang}`)) ||
  subs.find((s) => EN_RE.test(`${s.language} ${s.lang}`))

export function SubtitleCard({
  media,
  auto,
  onUse,
}: {
  media?: SubtitleMedia
  /** Pasang otomatis (Indonesia→Inggris) selama belum ada track aktif. */
  auto?: boolean
  onUse: (src: string, label: string) => void
}) {
  const [items, setItems] = useState<Subtitle[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const applied = useRef('')
  const autoRef = useRef(auto)
  useEffect(() => {
    autoRef.current = auto
  }, [auto])

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!media) return
    setBusy(true)
    setError('')
    const what = media.tab === 'tv' ? `${media.name} S${media.season}E${media.episode}` : media.name
    log.info(`Mencari subtitle "${what}"…`, 'sub:search')
    try {
      let url = `/api/subtitles?tab=${media.tab}&id=${encodeURIComponent(media.id)}`
      if (media.tab === 'tv') url += `&s=${media.season}&e=${media.episode}`
      const body = await api<{ subtitles: Subtitle[] }>(url, signal)
      if (signal?.aborted) return
      const subs = body.subtitles || []
      setItems(subs)
      log.ok(`Subtitle siap · ${subs.length} pilihan`, 'sub:search')
      // On the fly: player belum punya track pilihan → pasang yang cocok tanpa
      // klik. applied mencegah pemasangan ulang tiap refresh.
      const pick = autoRef.current ? preferred(subs) : undefined
      if (pick && applied.current !== pick.id) {
        applied.current = pick.id
        onUse(pick.url, pick.language || pick.lang)
        log.ok(`Subtitle otomatis dipasang · ${pick.name}`, 'sub:auto')
      }
    } catch (err) {
      if (signal?.aborted) return
      setItems([])
      setError((err as Error).message)
      log.err(`Subtitle gagal dimuat · ${(err as Error).message}`, 'sub:search')
    } finally {
      if (!signal?.aborted) setBusy(false)
    }
  }, [media, onUse])

  useEffect(() => {
    applied.current = ''
    const c = new AbortController()
    void load(c.signal)
    return () => c.abort()
  }, [load])

  return (
    <div className="border-t border-border bg-card px-3 py-2 sm:px-3.5">
      <div className="flex items-center gap-2 text-[10px] text-zinc-400">
        <Captions className="h-3.5 w-3.5" />
        <span className="font-semibold uppercase tracking-wide text-zinc-300">Subtitle</span>
        <span className="text-zinc-600">{items.length}</span>
        <button
          type="button"
          aria-label="Cari subtitle ulang"
          title="Ambil ulang daftar subtitle (fresh)"
          disabled={!media || busy}
          onClick={() => void load()}
          className="ml-auto grid h-7 w-7 place-items-center rounded text-zinc-400 transition hover:bg-zinc-800 hover:text-white disabled:opacity-40 sm:h-6 sm:w-6"
        >
          <RefreshCw className="h-3 w-3" />
        </button>
      </div>

      {error && <p className="mt-1 rounded border border-rose-900 bg-rose-950/40 px-2 py-1 text-[10px] text-rose-300">{error}</p>}

      <div className="mt-1 max-h-40 overflow-y-auto font-mono text-[10px] sm:max-h-32">
        {busy ? (
          <p className="py-1.5 text-zinc-600">mencari subtitle…</p>
        ) : !media ? (
          <p className="py-1.5 text-zinc-600">Pilih judul dulu untuk mencari subtitle.</p>
        ) : !error && items.length === 0 ? (
          <p className="py-1.5 text-zinc-600">Tidak ada subtitle untuk judul/episode ini.</p>
        ) : (
          items.map((s) => (
            <div key={s.id} className="flex items-center gap-1.5 border-b border-border/60 py-1.5 last:border-0 sm:gap-2 sm:py-1">
              <span className="w-16 shrink-0 truncate text-[9px] uppercase text-zinc-500 sm:w-20 sm:text-[10px]" title={s.language}>
                {s.language || s.lang}
              </span>
              <span className="min-w-0 flex-1 truncate text-[11px] text-zinc-300 sm:text-[10px]" title={s.name}>
                {s.name}
              </span>
              <span className="shrink-0 text-zinc-600 max-[360px]:hidden">{s.downloads}</span>
              <button
                type="button"
                aria-label={`Pakai subtitle ${s.name}`}
                onClick={() => {
                  onUse(s.url, s.language || s.lang)
                  log.ok(`Subtitle dipakai · ${s.name}`, 'sub:pick')
                }}
                className="shrink-0 rounded border border-border px-2 py-1 text-[11px] text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800 sm:px-1.5 sm:py-0.5 sm:text-[10px]"
              >
                pakai
              </button>
            </div>
          ))
        )}
      </div>
    </div>
  )
}

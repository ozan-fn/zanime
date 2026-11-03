import { useEffect, useRef, useState } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { useApp } from '@/features/app/AppProvider'
import { Sq } from '@/features/text/Sq'
import type { Streams } from '@/features/search/BrowsePanel'

/* Sidebar kanan: pilihan server/kualitas untuk judul yang sedang dibuka.
 * Collapse sama dengan kartu subtitle: header tetap terlihat, isinya disembunyikan. */

export function ServerPanel({
  streams,
  onOpenChange,
}: {
  streams: Streams | null
  /** Diberitahu setiap kali panel dibuka/ditutup (dipakai layout desktop). */
  onOpenChange?: (open: boolean) => void
}) {
  const { play, setStatus, playing } = useApp()
  const [open, setOpen] = useState(true)
  // Server terakhir yang diputar; dipakai untuk melanjutkan episode berikutnya.
  const lastPlayed = useRef<{ id: string; season?: number; episode?: number; server: string } | null>(null)

  function toggle() {
    setOpen((v) => {
      onOpenChange?.(!v)
      return !v
    })
  }

  // Daftar server baru selesai di-scrape → panel terbuka sendiri.
  useEffect(() => {
    if (streams?.rows.length) {
      setOpen(true)
      onOpenChange?.(true)
    }
  }, [streams, onOpenChange])

  function pick(s: Streams['rows'][number]) {
    if (!streams || playing?.src === s.url) return
    // Backend sudah memberi slug pendek /s/<slug> (auto proxy + subtitle).
    play({
      src: s.url,
      title: streams.title,
      sub: [s.quality || s.type, s.name].filter(Boolean).join(' · '),
      subs: s.subs,
      server: s.name,
      media: {
        tab: streams.media?.tab ?? 'movie',
        id: streams.media?.id ?? '',
        season: streams.media?.season,
        episode: streams.media?.episode,
      },
    })
    if (streams.media) {
      lastPlayed.current = {
        id: streams.media.id,
        season: streams.media.season,
        episode: streams.media.episode,
        server: s.name,
      }
    }
    setStatus(`Memutar ${s.quality || s.type} dari ${s.name}.`, 'ok')
  }

  // Ganti episode pada seri yang sama → lanjutkan otomatis di server yang sama.
  // Selain itu (judul/season lain, movie, episode yang sama) tidak auto-play.
  useEffect(() => {
    const m = streams?.media
    const prev = lastPlayed.current
    if (!m || m.tab !== 'tv' || !prev || prev.id !== m.id) return
    if (prev.season === m.season && prev.episode === m.episode) return
    const row = streams?.rows.find((r) => r.name === prev.server)
    if (row) pick(row)
    // pick stabil secara perilaku; cukup dipicu saat daftar stream berganti.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [streams])

  return (
    <aside className={`flex min-h-0 flex-col gap-1.5 rounded-lg border border-border bg-card p-2.5 sm:rounded-xl sm:p-3 ${open ? 'lg:h-full lg:min-h-0' : ''}`}>
      <div className="flex items-center gap-2">
        <span className="text-[11px] font-medium uppercase tracking-wide text-zinc-500">Server</span>
        <span className="text-[10px] text-zinc-600">{streams?.rows.length ?? 0}</span>
        <button
          type="button"
          aria-label={open ? 'Tutup daftar server' : 'Buka daftar server'}
          onClick={toggle}
          className="ml-auto grid h-7 w-7 place-items-center rounded text-zinc-400 transition hover:bg-zinc-800 hover:text-white sm:h-6 sm:w-6"
        >
          {open ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
        </button>
      </div>

      {open && (
      <>
      <Sq text={streams?.title || ''} className="text-xs text-zinc-400" />

      {/* Ponsel/tablet: kartu server satu baris bisa digeser; desktop: kolom. */}
      <div className="min-h-0 flex-1">
        <div className="flex gap-2 overflow-x-auto pb-1 lg:h-full lg:flex-col lg:gap-1.5 lg:overflow-y-auto lg:overflow-x-hidden lg:pr-0.5 lg:pb-0">
        {!streams ? (
          <p className="p-3 text-center text-[11px] text-zinc-600">Pilih judul untuk melihat server.</p>
        ) : !streams.rows.length ? (
          <p className="p-3 text-center text-[11px] text-zinc-600">Tidak ada server stream yang bisa diputar.</p>
        ) : (
          streams.rows.map((s) => {
            const active = playing?.src === s.url
            return (
              <button key={`${s.name}:${s.quality}:${s.url.slice(-20)}`} type="button" onClick={() => pick(s)}
                className={`flex w-44 shrink-0 flex-col rounded-lg border px-2.5 py-2 text-left transition sm:w-48 lg:w-full lg:shrink lg:px-2 lg:py-1.5 ${
                  active ? 'border-zinc-400 bg-zinc-800' : 'border-border bg-zinc-900 hover:border-zinc-600 hover:bg-zinc-800'
                }`}>
                <span className="flex flex-wrap items-baseline gap-x-2 text-[11px]">
                  <span className="font-medium text-zinc-100">{s.quality || s.type.toUpperCase()}</span>
                  {s.quality && <span className="text-zinc-400">{s.type === 'hls' ? 'HLS' : 'MP4'}</span>}
                  {s.source === 'miruro' && (
                    <span className="rounded border border-border px-1 text-[9px] uppercase text-zinc-400">miruro</span>
                  )}
                  {s.subs.length > 0 && <span className="ml-auto text-zinc-500">{s.subs.length} sub</span>}
                </span>
                <Sq text={s.name} className="mt-0.5 text-[10px] text-zinc-500" />
              </button>
            )
          })
        )}
        </div>
      </div>
      </>
      )}
    </aside>
  )
}

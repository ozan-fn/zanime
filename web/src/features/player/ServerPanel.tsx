import { useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useApp } from '@/features/app/AppProvider'
import { Sq } from '@/features/text/Sq'
import type { Streams } from '@/features/search/BrowsePanel'

/* Sidebar kanan: pilihan server/kualitas untuk judul yang sedang dibuka.
 * Hanya panel ini yang bisa di-collapse; rail sempit tetap menyisakan tombol. */

export function ServerPanel({ streams }: { streams: Streams | null }) {
  const { play, setStatus, playing } = useApp()
  const [open, setOpen] = useState(true)

  // Daftar server baru selesai di-scrape → panel terbuka sendiri.
  useEffect(() => {
    if (streams?.rows.length) setOpen(true)
  }, [streams])

  function pick(s: Streams['rows'][number]) {
    if (!streams || playing?.src === s.url) return
    // Backend sudah memberi slug pendek /s/<slug> (auto proxy + subtitle).
    play({
      src: s.url,
      title: streams.title,
      sub: [s.quality || s.type, s.name].filter(Boolean).join(' · '),
      subs: s.subs,
    })
    setStatus(`Memutar ${s.quality || s.type} dari ${s.name}.`, 'ok')
  }

  if (!open) {
    return (
      <aside className="flex shrink-0 items-start justify-center rounded-lg border border-border bg-card p-1.5 sm:rounded-xl lg:w-11">
        <button type="button" aria-label="Buka daftar server" onClick={() => setOpen(true)}
          className="grid h-8 w-8 place-items-center rounded-lg border border-border text-zinc-400 transition hover:bg-zinc-800 hover:text-white sm:h-7 sm:w-7">
          <ChevronLeft className="h-4 w-4" />
        </button>
      </aside>
    )
  }

  return (
    <aside className="flex min-h-0 flex-col gap-1.5 rounded-lg border border-border bg-card p-2.5 sm:rounded-xl sm:p-3 lg:h-full lg:w-60 lg:shrink-0">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] font-medium uppercase tracking-wide text-zinc-500">Server</span>
        {/* Collapse hanya berguna di desktop; di ponsel daftar selalu tampil. */}
        <button type="button" aria-label="Tutup daftar server" onClick={() => setOpen(false)}
          className="hidden h-8 w-8 place-items-center rounded border border-border text-zinc-500 transition hover:bg-zinc-800 hover:text-white lg:grid">
          <ChevronRight className="h-3.5 w-3.5" />
        </button>
      </div>

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
                  <span className="text-zinc-400">{s.type === 'hls' ? 'HLS' : 'MP4'}</span>
                  {s.subs.length > 0 && <span className="ml-auto text-zinc-500">{s.subs.length} sub</span>}
                </span>
                <Sq text={s.name} className="mt-0.5 text-[10px] text-zinc-500" />
              </button>
            )
          })
        )}
        </div>
      </div>
    </aside>
  )
}

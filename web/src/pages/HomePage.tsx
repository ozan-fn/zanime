import { useState } from 'react'
import { Clapperboard } from 'lucide-react'
import { useApp } from '@/features/app/AppProvider'
import { Player } from '@/features/player/Player'
import { ServerPanel } from '@/features/player/ServerPanel'
import { Footer } from '@/features/home/Footer'
import { BrowsePanel, type Streams } from '@/features/search/BrowsePanel'
import { Sq } from '@/features/text/Sq'

/* Satu halaman, mobile-first:
 * - ponsel: player dulu, lalu server (chip horizontal), lalu pencarian;
 * - tablet (md): pencarian kiri + player kanan, server satu baris penuh;
 * - desktop (lg): pencarian | player | server seperti sebelumnya.
 * Header menempel di atas saat scroll di ponsel/tablet. */
export function HomePage() {
  const [streams, setStreams] = useState<Streams | null>(null)
  const { playing } = useApp()
  const focusSearch = () => {
    const el = document.getElementById('search-q')
    el?.focus()
    el?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }

  return (
    <div className="mx-auto flex min-h-screen max-w-8xl flex-col px-3 py-2 sm:px-5 sm:py-4 lg:h-screen lg:min-h-0 lg:overflow-hidden lg:px-8 lg:py-6">
      <header className="sticky top-0 z-20 -mx-3 flex shrink-0 items-center justify-between gap-3 border-b border-border/70 bg-[#09090b]/90 px-3 py-2 backdrop-blur sm:-mx-5 sm:px-5 lg:static lg:mx-0 lg:border-0 lg:bg-transparent lg:px-0 lg:py-0 lg:backdrop-blur-none">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-border bg-card text-zinc-200 sm:h-10 sm:w-10">
            <Clapperboard className="h-4 w-4 sm:h-5 sm:w-5" />
          </div>
          <div className="min-w-0">
            <h1 className="text-base font-semibold tracking-tight text-white sm:text-lg">kafuu</h1>
            <p className="truncate text-[11px] text-zinc-500 sm:text-xs">Streaming film &amp; seri</p>
          </div>
        </div>
        {/* Desktop: judul yang sedang diputar tetap terlihat walau panel kiri di-scroll. */}
        {playing?.title && (
          <div className="hidden min-w-0 items-center gap-2 lg:flex">
            <span className="shrink-0 text-[10px] uppercase tracking-wide text-zinc-600">Diputar</span>
            <Sq text={playing.title} className="max-w-[24rem] text-xs text-zinc-400" />
          </div>
        )}
        <button
          type="button"
          onClick={focusSearch}
          className="shrink-0 rounded-lg border border-border bg-card px-2.5 py-1.5 text-[11px] text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800 hover:text-white lg:hidden"
        >
          Cari
        </button>
      </header>

      <main className="mt-3 flex min-h-0 flex-1 flex-col gap-3 sm:mt-4 md:grid md:grid-cols-[17rem_minmax(0,1fr)] md:items-start md:gap-3 lg:grid-cols-[18rem_minmax(0,1fr)_16rem] lg:items-stretch lg:gap-5">
        <div className="order-3 min-w-0 md:order-1 md:min-h-0 lg:h-full">
          <BrowsePanel onStreams={setStreams} />
        </div>

        <div className="order-1 min-w-0 md:order-2 md:min-h-0 lg:h-full lg:overflow-y-auto lg:pr-1">
          <Player media={streams?.media} />
          {!playing && (
            <button
              type="button"
              onClick={focusSearch}
              className="mt-2 block w-full rounded-lg border border-border bg-card px-3 py-2 text-left text-[11px] text-zinc-500 transition hover:border-zinc-600 hover:text-zinc-300 sm:mt-3 sm:text-xs"
            >
              Cari judul lewat panel pencarian untuk mulai streaming.
            </button>
          )}
        </div>

        <div className="order-2 min-w-0 md:order-3 md:col-span-2 lg:order-3 lg:col-span-1 lg:min-h-0">
          <ServerPanel streams={streams} />
        </div>
      </main>

      <Footer />
    </div>
  )
}

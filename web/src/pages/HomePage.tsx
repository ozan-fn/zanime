import { useState } from 'react'
import { Clapperboard } from 'lucide-react'
import { useApp } from '@/features/app/AppProvider'
import { Player } from '@/features/player/Player'
import { ServerPanel } from '@/features/player/ServerPanel'
import { Header } from '@/features/home/Header'
import { Footer } from '@/features/home/Footer'
import { BrowsePanel, type Streams } from '@/features/search/BrowsePanel'

/* Satu halaman: sidebar kiri (cari + hasil/episode), player di tengah, sidebar
 * kanan (server) yang bisa di-collapse. Semua alur (search, meta, stream)
 * di-handle backend; browser hanya me-render hasilnya. */
export function HomePage() {
  const [streams, setStreams] = useState<Streams | null>(null)
  const { playing } = useApp()
  const focusSearch = () => document.getElementById('search-q')?.focus()

  return (
    <div className="mx-auto flex min-h-screen max-w-7xl flex-col px-4 py-6 sm:px-6 lg:h-screen lg:min-h-0 lg:overflow-hidden lg:px-8">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-lg border border-border bg-card text-zinc-200">
            <Clapperboard className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-lg font-semibold tracking-tight text-white">kafuu</h1>
            <p className="text-xs text-zinc-500">Streaming film & seri</p>
          </div>
        </div>
        <Header onOpenSearch={focusSearch} />
      </header>

      <main className="mt-6 flex min-h-0 flex-1 flex-col gap-4 lg:flex-row">
        <div className="order-2 min-w-0 lg:order-1 lg:min-h-0 lg:w-64 lg:shrink-0">
          <BrowsePanel onStreams={setStreams} />
        </div>
        <div className="order-1 flex min-w-0 flex-1 flex-col lg:order-2 lg:min-h-0">
          <Player />
          {!playing && (
            <button
              type="button"
              onClick={focusSearch}
              className="mt-3 text-xs text-zinc-500 transition hover:text-zinc-300"
            >
              Cari judul di sidebar kiri untuk mulai streaming.
            </button>
          )}
        </div>
        <div className="order-3 lg:min-h-0">
          <ServerPanel streams={streams} />
        </div>
      </main>

      <Footer />
    </div>
  )
}

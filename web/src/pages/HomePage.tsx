import { useState } from 'react'
import { Clapperboard, Search } from 'lucide-react'
import { useApp } from '@/features/app/AppProvider'
import { Player } from '@/features/player/Player'
import { Header } from '@/features/home/Header'
import { Footer } from '@/features/home/Footer'
import { SearchDialog } from '@/features/search/SearchDialog'

/* Satu halaman: player + kartu stream. Semua alur (search, meta, stream)
 * di-handle backend; browser hanya me-render hasilnya. */
export function HomePage() {
  const [searchOpen, setSearchOpen] = useState(false)
  const { playing } = useApp()

  return (
    <div className="mx-auto flex min-h-screen max-w-5xl flex-col px-4 py-6 sm:px-6 lg:px-8">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-lg border border-border bg-card text-zinc-200">
            <Clapperboard className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-lg font-semibold tracking-tight text-white">kafuu</h1>
            <p className="text-xs text-zinc-500">Streaming film & seri</p>
          </div>
        </div>
        <Header onOpenSearch={() => setSearchOpen(true)} />
      </header>

      <main className="mt-6 flex flex-1 flex-col gap-4">
        <Player />
        {!playing && (
          <button
            type="button"
            onClick={() => setSearchOpen(true)}
            className="mx-auto mt-8 flex items-center gap-2 rounded-lg border border-border bg-card px-4 py-3 text-sm text-zinc-300 transition hover:bg-zinc-800 hover:text-white"
          >
            <Search className="h-4 w-4" />
            Cari judul untuk mulai
          </button>
        )}
      </main>

      <Footer />
      <SearchDialog open={searchOpen} onClose={() => setSearchOpen(false)} />
    </div>
  )
}

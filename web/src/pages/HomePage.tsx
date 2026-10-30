import { useState } from 'react'
import { Clapperboard, Info } from 'lucide-react'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { MagnetForm } from '@/features/magnet/MagnetForm'
import { FilesCard } from '@/features/files/FilesCard'
import { MediaCard } from '@/features/media/MediaCard'
import { StatsCard } from '@/features/stats/StatsCard'
import { Player } from '@/features/player/Player'
import { Header } from '@/features/home/Header'
import { Footer } from '@/features/home/Footer'
import { SearchDialog } from '@/features/search/SearchDialog'

/* Satu halaman: sidebar (magnet → file → status) + player. State torrent
 * dari provider tetap hidup karena halaman ini tak unmount selama sesi. */
export function HomePage() {
  const [searchOpen, setSearchOpen] = useState(false)
  const { state } = useTorrent()
  const hasFiles = Boolean(state?.files?.length)

  return (
    <div className="mx-auto flex min-h-screen max-w-7xl flex-col px-4 py-6 sm:px-6 lg:px-8">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-lg border border-border bg-card text-zinc-200">
            <Clapperboard className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-lg font-semibold tracking-tight text-white">kafuu</h1>
            <p className="text-xs text-zinc-500">Streaming torrent lewat HLS</p>
          </div>
        </div>
        <Header onOpenSearch={() => setSearchOpen(true)} />
      </header>

      <main className="mt-6 grid flex-1 items-start gap-5 lg:grid-cols-[390px_minmax(0,1fr)]">
        <section className="flex min-w-0 flex-col gap-4">
          <MagnetForm />
          {hasFiles && <FilesCard />}
          <MediaCard />
          <StatsCard />
        </section>

        <section className="flex min-w-0 flex-col gap-3">
          <Player />
          <div className="flex items-start gap-2 rounded-xl border border-white/[0.06] bg-white/[0.02] px-3 py-2.5 text-[11px] leading-relaxed text-zinc-500">
            <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-zinc-600" />
            <span>Semua video di-remux mkvgo (pure Go, tanpa ffmpeg) dan dikirim ke player sebagai HLS. Tidak ada encode ulang, jadi browser harus mendukung codec sumbernya (H.264/HEVC/AV1 + AAC/Opus).</span>
          </div>
        </section>
      </main>

      <Footer />
      <SearchDialog open={searchOpen} onClose={() => setSearchOpen(false)} />
    </div>
  )
}

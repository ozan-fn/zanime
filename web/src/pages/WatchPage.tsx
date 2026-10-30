import { Link } from 'react-router-dom'
import { Info } from 'lucide-react'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { MagnetForm } from '@/features/magnet/MagnetForm'
import { Player } from '@/features/player/Player'

/* Halaman utama: player besar di kiri, panel mulai (magnet). List lengkap
 * video dipindah ke /video; detail teknis ke /status. */
export function WatchPage() {
  const { state } = useTorrent()
  const hasTorrent = Boolean(state?.files?.length)

  return (
    <div className="grid flex-1 items-start gap-5 lg:grid-cols-[minmax(0,1fr)_360px]">
      <section className="flex min-w-0 flex-col gap-3">
        <Player />
        <div className="flex items-start gap-2 rounded-xl border border-white/[0.06] bg-white/[0.02] px-3 py-2.5 text-[11px] leading-relaxed text-zinc-500">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-zinc-600" />
          <span>Semua video di-remux mkvgo (pure Go, tanpa ffmpeg) dan dikirim ke player sebagai HLS. Tidak ada encode ulang, jadi browser harus mendukung codec sumbernya (H.264/HEVC/AV1 + AAC/Opus).</span>
        </div>
      </section>

      <aside className="flex min-w-0 flex-col gap-4">
        <MagnetForm />
        {hasTorrent && (
          <Link to="/files" className="group rounded-xl border border-border bg-card p-4 transition hover:border-zinc-600">
            <h2 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-zinc-500">Lanjut menonton</h2>
            <p className="mt-1 truncate text-sm font-medium text-zinc-200">{state?.name}</p>
            <p className="mt-1 text-xs text-zinc-500 group-hover:text-zinc-300">Pilih episode lain →</p>
          </Link>
        )}
      </aside>
    </div>
  )
}

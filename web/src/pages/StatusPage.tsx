import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { StatsCard } from '@/features/stats/StatsCard'

/* Halaman Status: detail torrent, cache, net, dan server satu halaman
 * penuh — bebas dari kartu sempit di sidebar UI lama. */
export function StatusPage() {
  const { state } = useTorrent()
  if (!state?.name) {
    return (
      <div className="grid flex-1 place-items-center rounded-xl border border-border bg-card p-10 text-center text-sm text-zinc-500">
        Belum ada torrent aktif.
      </div>
    )
  }
  return <StatsCard />
}

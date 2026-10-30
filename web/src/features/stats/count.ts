import type { TorrentState } from '@/lib/types'

// "Torrent di disk": jumlah piece lengkap yang tinggal di cache disk.
// HTML lama memanggil torrentCount() tanpa definisi; ini pengisian yang
// masuk akal untuk label yang sama (lerang: x Menonton).
export function torrentCount(tor?: TorrentState['torrent']): string {
  if (!tor) return '—'
  const complete = tor.complete_pieces || 0
  if (!complete) return '—'
  return `${complete} piece`
}

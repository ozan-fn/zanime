import { Cpu, Search } from 'lucide-react'
import { api } from '@/lib/api'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { applyResume, invalidatePlaylist, resumeSeconds } from '@/features/player/ctl'

/* Header: tombol pencarian (buka dialog), toggle transcode (flag saja —
 * mkvgo tidak meng-encode), dan pilihan bitrate audio AAC; pindah bitrate
 * me-reload playlist sambil melanjutkan posisi putar. */
export function Header({ onOpenSearch }: { onOpenSearch: () => void }) {
  const { state, applyState, setStatus } = useTorrent()
  const transcode = Boolean(state?.transcode)
  const kbps = String(state?.audio_kbps || 128)

  const toggleTranscode = async () => {
    const enabled = !state?.transcode
    try {
      applyState(await api('/api/transcode', { method: 'POST', body: JSON.stringify({ enabled }) }))
      setStatus('mkvgo tidak meng-encode: playback tetap memakai track sumber (toggle tidak berpengaruh).', 'ok')
    } catch (err) {
      setStatus((err as Error).message, 'error')
    }
  }

  const changeAudioRate = async (event: React.ChangeEvent<HTMLSelectElement>) => {
    const kbpsNext = Number(event.target.value)
    try {
      // Playlist lama sudah berisi bitrate sebelumnya, jadi player dimuat
      // ulang dan melanjutkan dari posisi terakhir.
      const at = resumeSeconds()
      applyResume(at)
      invalidatePlaylist()
      applyState(await api('/api/audio', { method: 'POST', body: JSON.stringify({ kbps: kbpsNext }) }))
    } catch (err) {
      setStatus((err as Error).message, 'error')
    }
  }

  return (
    <div className="flex items-center gap-2">
      <button
        type="button"
        onClick={onOpenSearch}
        className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-border bg-card px-3 text-xs font-medium text-zinc-300 transition hover:bg-zinc-800 hover:text-white"
      >
        <Search className="h-3.5 w-3.5" />
        Cari
      </button>
      <button
        type="button"
        aria-pressed={transcode}
        onClick={() => void toggleTranscode()}
        className={[
          'inline-flex h-7 items-center gap-1.5 rounded-lg border px-2.5 text-[11px] transition',
          transcode
            ? 'border-emerald-400/40 bg-emerald-400/10 text-emerald-300'
            : 'border-border bg-card text-zinc-400 hover:bg-zinc-800 hover:text-white',
        ].join(' ')}
      >
        <Cpu className="h-3 w-3" />
        <span>{transcode ? 'transcode: on' : 'transcode: off'}</span>
      </button>
      <label htmlFor="audio-rate" className="sr-only">Bitrate audio</label>
      <select
        id="audio-rate"
        title="Bitrate audio AAC"
        value={kbps}
        onChange={(e) => void changeAudioRate(e)}
        className="h-7 rounded-lg border border-border bg-card px-2 text-[11px] text-zinc-300 outline-none transition hover:bg-zinc-800 focus:border-zinc-600"
      >
        <option value="128">audio 128k</option>
        <option value="192">audio 192k</option>
        <option value="256">audio 256k</option>
      </select>
    </div>
  )
}

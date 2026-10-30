import { useState } from 'react'
import type { TorrentFile } from '@/lib/types'
import { fmt } from '@/lib/fmt'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { Sq } from '@/features/text/Sq'
import { epThumbsState, setPickedThumb, type ThumbStore } from './thumbs'

/* Thumbnail episode per judul (kunci "season:episode") + thumbnail pilihan
 * terakhir; ikut disimpan bersama magnet supaya bertahan setelah reload. */
export function buildEpThumbs(videos: Array<{ thumbnail?: string; season?: number | null; episode?: number | null }>) {
  return Object.fromEntries(videos
    .filter((v) => v.thumbnail && v.season != null)
    .map((v) => [`${v.season}:${v.episode}`, v.thumbnail]))
}
export { resetEpThumbs, setPickedThumb, setStoreThumbs } from './thumbs'

/* Fallback thumbnail berbasis pola nama: kalau semua file identik kecuali satu
 * angka (mis. "[G] Show - 01 [1080p].mkv" beda hanya 01/02/03), angka itu nomor
 * episode → pinjam thumbnail episode nomor sama walau nama file tidak
 * berformat SxxEyy. Nama yang tidak seragam tidak ditebak: angka bisa berarti
 * resolusi/tahun. */
const skeletonOf = (name: string) => (name || '').replace(/\d+/g, '#')

function uniformEpisodeRunIndex(files: TorrentFile[]): number | null {
  if (files.length < 2) return null
  const runsOf = (f: TorrentFile) => (f.name || '').match(/\d+/g) || []
  const first = runsOf(files[0])
  if (!first.length || files.some((f) => {
    const runs = runsOf(f)
    return runs.length !== first.length || skeletonOf(f.name) !== skeletonOf(files[0].name)
  })) return null
  for (let i = 0; i < first.length; i++) {
    if (files.some((f) => runsOf(f)[i] !== first[i])) return i
  }
  return null
}

function thumbByEpisodeNumber(files: TorrentFile[], store: ThumbStore, file: TorrentFile): string | null {
  const runIndex = uniformEpisodeRunIndex(files)
  if (runIndex == null) return null
  const runs = (file.name || '').match(/\d+/g) || []
  const episode = runs[runIndex] != null ? +runs[runIndex] : null
  if (episode == null) return null
  const seasons = [...new Set(Object.keys(store.epThumbs).map((k) => k.split(':')[0]))].sort((a, b) => +a - +b)
  for (const season of seasons) {
    const thumb = store.epThumbs[`${season}:${episode}`]
    if (thumb) return thumb
  }
  return null
}

// Nama file SxxEyy → thumbnail episode; kalau tidak cocok (mis. nama file
// tanpa kode episode), coba pola nama seragam, lalu thumb terakhir.
export function epThumbOf(files: TorrentFile[], store: ThumbStore, file: TorrentFile, selectedIndex?: number): string {
  const m = (file.name || '').match(/S(\d{1,2})E(\d{1,3})/i)
  const hit = m && store.epThumbs[`${+m[1]}:${+m[2]}`]
  if (hit) return hit
  return thumbByEpisodeNumber(files, store, file) ?? (file.index === selectedIndex ? store.pickedThumb : '')
}

function Thumb({ src }: { src: string }) {
  const [failed, setFailed] = useState(false)
  if (src && !failed) {
    return (
      <img src={src} alt="" loading="lazy" onError={() => setFailed(true)}
        className="h-11 w-20 shrink-0 rounded-md border border-border bg-zinc-900 object-cover" />
    )
  }
  // Kalau tidak ada / gagal muat, pakai kotak kosong supaya baris tetap rapi.
  return <span className="h-11 w-20 shrink-0 rounded-md border border-border bg-zinc-900" />
}

type RowProps = {
  file: TorrentFile
  files: TorrentFile[]
  active: boolean
  store: ThumbStore
  onPick: (f: TorrentFile) => void
}

function Row({ file, files, active, store, onPick }: RowProps) {
  return (
    // div + role=button supaya tombol Cache bisa bersarang (button di
    // dalam button tidak valid).
    <div
      role="button"
      tabIndex={0}
      aria-current={active ? 'true' : 'false'}
      data-index={file.index}
      onClick={() => onPick(file)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onPick(file)
        }
      }}
      className={[
        'file-row group flex w-full cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition focus-visible:outline focus-visible:outline-1 focus-visible:outline-zinc-500',
        active ? 'border-border bg-zinc-800/60' : 'border-transparent hover:border-border hover:bg-zinc-800/40',
      ].join(' ')}
    >
      {/* active = file ini yang terpilih → boleh peluk thumbnail terakhir. */}
      <Thumb src={epThumbOf(files, store, file, active ? file.index : undefined)} />
      <span className="min-w-0 flex-1">
        <Sq text={file.name} className={active ? 'block text-sm font-medium text-white' : 'block text-sm text-zinc-300'} />
        <Sq
          text={file.path && file.path !== file.name ? file.path : fmt(file.size)}
          className="mt-0.5 block text-[11px] text-zinc-500"
        />
      </span>
      <span className="ml-2 shrink-0 text-[11px] tabular-nums text-zinc-500">{fmt(file.size)}</span>
    </div>
  )
}


export function FilesCard() {
  const { state, selectFile, setStatus } = useTorrent()
  const files = state?.files || []
  const selected = state?.selected
  const store = epThumbsState()

  if (!files.length) return null

  const onSelect = async (file: TorrentFile) => {
    try {
      setPickedThumb('')
      await selectFile(file.index)
    } catch (err) {
      setStatus((err as Error).message, 'error')
    }
  }

  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-zinc-500">Pilih video</h2>
          <p title={state?.name || ''} className="mt-1 truncate text-sm font-medium text-zinc-200">{state?.name}</p>
        </div>
        <span className="shrink-0 rounded-full border border-border bg-zinc-900 px-2 py-0.5 text-[11px] text-zinc-400">
          {files.length} file
        </span>
      </div>
      <div className="mt-3 max-h-[380px] space-y-1.5 overflow-y-auto pr-1">
        {files.map((file) => (
          <Row key={file.index} file={file} files={files} active={file.index === selected} store={store} onPick={onSelect} />
        ))}
      </div>
    </div>
  )
}

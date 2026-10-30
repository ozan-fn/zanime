import { ListVideo, Play } from 'lucide-react'
import { fmtDur } from '@/lib/fmt'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { ctl } from '@/features/player/ctl'

/* Kartu metadata MKV: deskripsi/rating/genre dan info teknis track. Chapter
 * ikut ditampilkan sebagai daftar lompat; marker-nya juga sudah otomatis di
 * progress bar MoviPlayer. */
export function MediaCard() {
  const { state } = useTorrent()
  const m = state?.media
  if (!m || (!m.description && !m.rating && !m.genre && !m.chapters?.length && !m.video?.length)) return null

  const video = m.video || []
  const audio = m.audio || []
  const subs = m.subs || []
  const chapters = m.chapters || []
  const rate = (hz?: number) => (hz ? `${(hz / 1000).toFixed(hz % 1000 ? 1 : 0)} kHz` : '')
  const langs = (tracks: typeof audio) =>
    tracks.map((t) => [t.lang?.toUpperCase(), t.name].filter(Boolean).join(' ')).filter(Boolean).join(', ')
  const tech = [
    ...video.map((t, i) => [
      video.length > 1 ? `Video ${i + 1}` : 'Video',
      t.codec?.toUpperCase(),
      t.width ? `${t.width}×${t.height}` : '',
      t.fps ? `${t.fps.toFixed(2)} fps` : '',
      t.bit_depth ? `${t.bit_depth}-bit` : '',
    ].filter(Boolean).join(' · ')),
    ...audio.map((t, i) => [
      audio.length > 1 ? `Audio ${i + 1}` : 'Audio',
      t.lang?.toUpperCase(),
      t.name,
      t.codec?.toUpperCase(),
      rate(t.sample_rate),
      t.channels ? `${t.channels} ch` : '',
    ].filter(Boolean).join(' · ')),
    subs.length > 0 && `Subtitle · ${subs.length}${langs(subs) ? ` · ${langs(subs)}` : ''}`,
  ].filter(Boolean)

  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <div className="flex items-start gap-3">
        {m.poster && (
          <img src={m.poster} alt="" className="h-16 w-12 shrink-0 rounded-md border border-border bg-zinc-900 object-cover" />
        )}
        <div className="min-w-0 flex-1">
          <h2 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-zinc-500">Info media</h2>
          {m.title && <p className="mt-1 truncate text-sm font-medium text-zinc-200" title={m.title}>{m.title}</p>}
          <div className="mt-1.5 flex flex-wrap gap-1.5">
            {[m.rating && `★ ${m.rating}`, m.genre, m.date].filter(Boolean).map((tag) => (
              <span key={String(tag)} className="rounded-full border border-border bg-zinc-900 px-2 py-0.5 text-[10px] text-zinc-400">
                {tag}
              </span>
            ))}
          </div>
        </div>
      </div>

      {m.description && <p className="mt-3 line-clamp-4 text-[11px] leading-relaxed text-zinc-500">{m.description}</p>}

      {tech.length > 0 && (
        <div className="mt-3 space-y-1 border-t border-white/[0.06] pt-3 text-[11px] text-zinc-500">
          {tech.map((t) => <p key={String(t)}>{t}</p>)}
        </div>
      )}

      {chapters.length > 0 && (
        <div className="mt-3 border-t border-white/[0.06] pt-3">
          <p className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-600">
            <ListVideo className="h-3 w-3" /> Chapter
          </p>
          <div className="mt-1.5 max-h-40 space-y-0.5 overflow-y-auto pr-1">
            {chapters.map((ch, i) => (
              <button
                key={`${ch.start}-${i}`}
                type="button"
                onClick={() => {
                  const el = ctl.el
                  if (!el) return
                  el.currentTime = ch.start
                  el.play?.()
                }}
                className="group flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-left text-[11px] text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200"
              >
                <Play className="h-2.5 w-2.5 shrink-0 opacity-0 group-hover:opacity-100" />
                <span className="w-10 shrink-0 tabular-nums text-zinc-600">{fmtDur(ch.start)}</span>
                <span className="truncate">{ch.title}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

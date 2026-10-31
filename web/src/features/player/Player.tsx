import { useCallback, useEffect, useRef } from 'react'
import { MoviPlayer, type MoviElement } from 'movi-player/react'
import { fmtDur } from '@/lib/fmt'
import { Sq } from '@/features/text/Sq'
import { useApp } from '@/features/app/AppProvider'
import { ctl, resetPlayback, setPlayerSrc } from './ctl'

/* Wrapper movi-player: src pasang imperatif; subtitle (track VTT dari
 * embedmaster) didaftarkan ke elemen saat stream terpilih punya subs.
 * Stream macet (Timeout) di-retry melanjutkan posisi terakhir. */
export function Player() {
  const { status, playing } = useApp()
  const ref = useRef<MoviElement | null>(null)
  const current = playing?.src || ''

  /* Satu useEffect per perubahan stream: pasang src + track subtitle. */
  useEffect(() => {
    const el = ref.current
    if (!el) return
    ctl.el = el

    // Ganti sumber → buang playback incumben dulu.
    if (current !== ctl.wanted) {
      resetPlayback()
      ctl.wanted = current
    }
    if (!current) return
    setPlayerSrc(el as any as HTMLElement, current)
    void el.load?.()

    // Track subtitle: VTT di-proxy agar tidak ditolak CORS host video.
    const video = el.querySelector?.('video') as HTMLVideoElement | null
    if (video) {
      video.querySelectorAll('track[data-em]').forEach((t) => t.remove())
      for (const [i, s] of (playing?.subs || []).entries()) {
        const tr = document.createElement('track')
        tr.setAttribute('data-em', String(i))
        tr.kind = 'subtitles'
        tr.label = s.label || s.lang
        tr.srclang = s.lang
        tr.src = `/fetch?url=${encodeURIComponent(s.url)}`
        video.appendChild(tr)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current, playing])

  // 'playing' → lepas startat (seek awal sudah terjadi).
  const handlePlay = useCallback(() => {
    const el = ref.current
    if (!ctl.resumeStartAt || !el) return
    ctl.resumeStartAt = 0
    el.removeAttribute?.('startat')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Error player: "Timeout" berarti byte tidak datang; muat ulang src lalu
  // lanjut dari posisi terakhir, bukan dari detik 0.
  const handleError = useCallback((detail?: { message?: string } | unknown) => {
    const message = (detail as { message?: string } | undefined)?.message
    if (!message) return
    const el = ref.current as any
    if (!/Timeout/.test(message) || !el?.getAttribute('src')) return
    const now = Date.now()
    if (now - ctl.lastRetry > 60_000) ctl.retries = 0
    if (ctl.retries >= 3) return
    ctl.retries++
    ctl.lastRetry = now
    const at = Math.max(0, Math.floor(el.currentTime || 0))
    ctl.resumeStartAt = at > 1 ? at : 0
    if (ctl.resumeStartAt) el.setAttribute?.('startat', String(at))
    setTimeout(() => {
      const el2 = ref.current
      const src = el2?.getAttribute?.('src')
      if (src) setPlayerSrc(el2 as any as HTMLElement, src)
      void el2?.load?.()
    }, 1500)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="overflow-hidden rounded-xl border border-border bg-black">
      {playing ? (
        <MoviPlayer
          ref={ref}
          controls
          autoplay
          playsinline
          thumb
          theme="dark"
          buffersize={64}
          bindav="false"
          subtitlepicker
          showtitle
          persist="loop muted volume speed ambient stablevolume hdr aspect audiolang subtitlelang"
          persistkey="zanime"
          onPlay={handlePlay}
          onError={handleError}
        />
      ) : (
        <div className="grid aspect-video place-items-center text-sm text-zinc-600">
          Pilih judul lewat pencarian untuk mulai streaming.
        </div>
      )}
      {(playing || status) && (
        <div className="flex items-center justify-between gap-3 border-t border-border bg-card px-4 py-3">
          <div className="min-w-0">
            {playing?.title && <Sq text={playing.title} className="text-sm font-medium text-zinc-100" />}
            {(playing?.sub || status) && (
              <Sq
                text={playing?.sub || status?.text || `Melanjutkan dari ${fmtDur(ctl.resumeStartAt)}…`}
                className="mt-0.5 text-[11px] text-zinc-500"
              />
            )}
          </div>
          <span className="hidden shrink-0 items-center gap-1 rounded-full border border-border bg-zinc-900 px-2.5 py-1 text-[11px] text-zinc-400 sm:inline-flex">
            <svg viewBox="0 0 24 24" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
              <circle cx="12" cy="12" r="2" fill="currentColor" stroke="none" />
              <path d="M16.2 3.8a9 9 0 0 1 0 16.4M7.8 20.2a9 9 0 0 1 0-16.4M19.8 7.2a12 12 0 0 1 0 9.6M4.2 16.8a12 12 0 0 1 0-9.6" />
            </svg>
            streaming
          </span>
        </div>
      )}
    </div>
  )
}

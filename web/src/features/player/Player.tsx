import { useCallback, useEffect, useRef } from 'react'
import { MoviPlayer, type MoviElement } from 'movi-player/react'
import { fmtDur } from '@/lib/fmt'
import { log } from '@/lib/logs'
import { Sq } from '@/features/text/Sq'
import { useApp } from '@/features/app/AppProvider'
import { ctl, resetPlayback, selectFirstSub, setPlayerSource, setPlayerSrc } from './ctl'
import { LogOverlay } from './LogOverlay'

/* Wrapper movi-player: src pasang imperatif; subtitle (track VTT dari
 * embedmaster) didaftarkan lewat API source() saat stream dipasang.
 * Stream macet (Timeout) di-retry melanjutkan posisi terakhir. */

// Event elemen yang dicatat ke panel log saat video dimuat (memuat manifest,
// track, buffering, error codec). Ringkas: satu nama event = satu baris log.
const MEDIA_EVENTS = [
  'loadstart', 'loadedmetadata', 'loadeddata', 'canplay', 'canplaythrough',
  'playing', 'waiting', 'stalled', 'seeking', 'seeked', 'durationchange',
  'encrypted', 'waitingforkey', 'qualitychange', 'trackschange',
  'subtitletrackchange', 'nativefallback', 'smoothwarning',
] as const

// detailText merangkum CustomEvent.detail jadi cuplikan pendek untuk log.
function detailText(e: Event): string {
  const d = (e as CustomEvent).detail
  if (d == null) return ''
  if (typeof d !== 'object') return ` · ${String(d)}`
  try {
    const s = JSON.stringify(d)
    return s && s !== '{}' ? ` · ${s.slice(0, 100)}` : ''
  } catch {
    return ''
  }
}

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

    const subs = playing?.subs || []
    const before = (el as any).player
    log.info(`pasang sumber · ${playing?.sub ?? current}`, 'player:src')
    try {
      setPlayerSource(el as any as HTMLElement, current, subs)
      log.ok(`sumber terpasang · ${subs.length} subtitle` + (subs.length ? '' : ' · menunggu player…'), 'player:src')
    } catch (err) {
      log.err(`gagal memasang sumber · ${(err as Error).message}`, 'player:src')
    }
    void el.load?.()

    // Aktifkan track pertama setelah player hasil load baru siap (maks ~6 s).
    let timer: number | undefined
    let tries = 0
    const mountSubs = () => {
      if (selectFirstSub(ref.current, subs, before) || ++tries > 24) return
      timer = window.setTimeout(mountSubs, 250)
    }
    if (subs.length) mountSubs()
    return () => {
      if (timer !== undefined) window.clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current, playing])

  /* Catat event media (loading manifest/segment, buffering, kualitas) ke log.
   * Satu id per nama event supaya event berulang (waiting/playing) memperbarui
   * baris yang sama, bukan menumpuk. */
  useEffect(() => {
    const el = ref.current as any
    if (!el) return
    const offs = MEDIA_EVENTS.map((name) => {
      const fn = (e: Event) => log.info(name + detailText(e), `player:evt:${name}`)
      el.addEventListener(name, fn)
      return () => el.removeEventListener(name, fn)
    })
    return () => offs.forEach((off) => off())
  }, [current])

  // 'playing' → lepas startat (seek awal sudah terjadi).
  const handlePlay = useCallback(() => {
    const el = ref.current
    log.ok('pemutaran mulai', 'player:play')
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
    if (!/Timeout/.test(message) || !el?.getAttribute('src')) {
      log.err(`player error · ${message}`, 'player:err')
      return
    }
    const now = Date.now()
    if (now - ctl.lastRetry > 60_000) ctl.retries = 0
    if (ctl.retries >= 3) {
      log.err(`retry habis · ${message}`, 'player:err')
      return
    }
    ctl.retries++
    ctl.lastRetry = now
    const at = Math.max(0, Math.floor(el.currentTime || 0))
    ctl.resumeStartAt = at > 1 ? at : 0
    log.info(`timeout · muat ulang dari ${at}s (percobaan ${ctl.retries}/3)`, 'player:err')
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
    <div className="relative overflow-hidden rounded-xl border border-border bg-black">
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
          onReady={() => log.info('elemen player siap', 'player:ready')}
          onQoe={(e) => log.info(`qoe · ${JSON.stringify(e).slice(0, 120)}`, 'player:qoe')}
          onPlay={handlePlay}
          onError={handleError}
          onErrorDisplay={(info) =>
            log.err(`error layar · ${info.message || info.title || 'tidak diketahui'}`, 'player:errdisplay')}
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
      {/* Log realtime di depan video: scrape, uji stream, pemuatan video. */}
      <LogOverlay />
    </div>
  )
}

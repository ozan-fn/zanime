import { useCallback, useEffect, useRef } from 'react'
import { MoviPlayer, type MoviElement } from 'movi-player/react'
import { fmt, fmtDur } from '@/lib/fmt'
import { Sq } from '@/features/text/Sq'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { epThumbsState } from '@/features/files/thumbs'
import { epThumbOf } from '@/features/files/FilesCard'
import { ctl, resetPlayback, setPlayerSrc } from './ctl'

/* Wrapper movi-player (paket npm, wrapper React resmi): src dan startat
 * diatur imperatif via ref begitu playlist HLS untuk file terpilih siap;
 * subtitle diambil + didaftarkan ke player; stream macet (error Timeout)
 * di-retry melanjutkan posisi terakhir. */
export function Player() {
  const { state, setStatus, directSrc, directInfo } = useTorrent()
  const ref = useRef<MoviElement | null>(null)
  const files = state?.files || []
  const selected = state?.selected
  const current = files.find((f) => f.index === selected)
  const store = epThumbsState()
  const live = !directSrc && state?.hls?.index === selected && Boolean(state?.hls?.live)
  const thumb = directSrc || !current ? '' : epThumbOf(files, store, current, selected)

  /* Satu useEffect per frame status: pemasangan src dan subtitle berjalan
   * imperatif terhadap elemen, jadi begitu playlist siap (atau elemen ter-
   * upgrade di frame berikutnya), src dipasang tanpa state React tambahan. */
  useEffect(() => {
    const el = ref.current
    if (!el || !state) return

    // Ganti file → buang playback incumben (src lama menyebabkan error
    // namespace / media tidak diganti).
    if ((selected ?? null) !== ctl.lastSelected) {
      ctl.lastSelected = selected ?? null
      resetPlayback()
    }

    if (directSrc) {
      if (el.getAttribute('src') !== directSrc) setPlayerSrc(el as any as HTMLElement, directSrc)
      return
    }

    const h = state.hls
    if (!h || h.index !== state.selected) return
    const name = files.find((f) => f.index === state.selected)?.name || 'video'
    if (h.ready) {
      const url = `/hls/${state.selected}/${h.playlist || 'index.m3u8'}`
      if (ctl.wanted !== url) {
        ctl.wanted = url
        setPlayerSrc(el as any as HTMLElement, url)
        void el.load?.()
        setStatus(ctl.resumeStartAt
          ? `Melanjutkan ${name} dari ${fmtDur(ctl.resumeStartAt)}…`
          : `Memutar ${name}.`, 'ok')
      }
      void applySubtitles(el, state.selected ?? -1, h.subs, setStatus)
    } else if (h.error) {
      setStatus(`Gagal menyiapkan video: ${h.error}`, 'error')
    } else {
      setStatus(`Membaca durasi ${name}…`, 'info')
    }
        // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, directSrc])

  // 'playing' → lepas startat (seek awal sudah terjadi).
  const handlePlay = useCallback(() => {
    const el = ref.current
    if (!ctl.resumeStartAt || !el) return
    ctl.resumeStartAt = 0
    el.removeAttribute?.('startat')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Error player: "Timeout" berarti byte belum sempat datang dari swarm:
  // muat ulang src yang sama, tapi lanjutkan dari posisi terakhir, bukan
  // dari detik 0.
  const handleError = useCallback((detail?: { message?: string } | unknown) => {
    const message = (detail as { message?: string } | undefined)?.message
    if (!message) return
    const el = ref.current as any
    if (!/Timeout/.test(message) || !el?.getAttribute('src')) {
      setStatus(`Player: ${message}`, 'error')
      return
    }
    const now = Date.now()
    if (now - ctl.lastRetry > 60_000) ctl.retries = 0 // macet lagi setelah sempat lancar
    if (ctl.retries >= 3) {
      setStatus('Stream macet: sumber tidak mengirim data. Coba lagi atau pilih kualitas lain.', 'error')
      return
    }
    ctl.retries++
    ctl.lastRetry = now
    const at = Math.max(0, Math.floor(el.currentTime || 0))
    ctl.resumeStartAt = at > 1 ? at : 0
    if (ctl.resumeStartAt) el.setAttribute?.('startat', String(at))
    setStatus(ctl.resumeStartAt
      ? `Stream macet, melanjutkan dari ${fmtDur(at)}…`
      : 'Stream macet, memuat ulang…', 'info')
    setTimeout(() => {
      const el2 = ref.current
      const src = el2?.getAttribute?.('src')
      if (src) setPlayerSrc(el2 as any as HTMLElement, src)
      void el2?.load?.()
    }, 1500)
  }, [setStatus])

  return (
    <div className="overflow-hidden rounded-xl border border-border bg-black">
      {/* MoviPlayer memutar HLS (menu audio per track + subtitle). */}
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
        persist="loop muted volume speed ambient stablevolume hdr aspect audiolang subtitlelang"
        persistkey="zanime"
        onPlay={handlePlay}
        onError={handleError}
      />
      <div
        className={[
          live ? 'flex' : 'hidden',
          'items-start gap-2 border-t border-amber-400/30 bg-amber-400/10 px-4 py-2.5 text-[11px] leading-relaxed text-amber-300',
        ].join(' ')}
      >
        <svg viewBox="0 0 24 24" className="mt-0.5 h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
          <path d="M12 9v4M12 17h.01M10.3 3.9 2 18a2 2 0 0 0 1.7 3h16.6A2 2 0 0 0 22 18L13.7 3.9a2 2 0 0 0-3.4 0Z" />
        </svg>
        <span>Durasi video tidak terbaca, jadi diputar sebagai <strong className="font-medium">stream live</strong>: durasi dan lompat ke akhir tidak tersedia.</span>
      </div>
      <div
        className={[
          files.length ? 'flex' : 'hidden',
          'items-center justify-between gap-3 border-t border-border bg-card px-4 py-3',
        ].join(' ')}
      >
        {thumb ? (
          <img src={thumb} alt="" className="h-11 w-20 shrink-0 rounded-md border border-border bg-zinc-900 object-cover" />
        ) : null}
        <div className="min-w-0">
          <Sq text={directInfo?.title || current?.name || ''} className="text-sm font-medium text-zinc-100" />
          <Sq
            text={directInfo?.sub || (current ? `${fmt(current.size)} · ${current.path || current.name}` : '')}
            className="mt-0.5 text-[11px] text-zinc-500"
          />
        </div>
        <span className="hidden shrink-0 items-center gap-1 rounded-full border border-border bg-zinc-900 px-2.5 py-1 text-[11px] text-zinc-400 sm:inline-flex">
          <svg viewBox="0 0 24 24" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
            <circle cx="12" cy="12" r="2" fill="currentColor" stroke="none" />
            <path d="M16.2 3.8a9 9 0 0 1 0 16.4M7.8 20.2a9 9 0 0 1 0-16.4M19.8 7.2a12 12 0 0 1 0 9.6M4.2 16.8a12 12 0 0 1 0-9.6" />
          </svg>
          streaming
        </span>
      </div>
    </div>
  )
}

/* Subtitle di dalam file: server mengekstrak track teks jadi WebVTT saat
 * diminta, lalu player memuatnya sebagai track subtitle biasa (termasuk
 * menu subtitle di player). */
async function applySubtitles(el: MoviElement, index: number, subs: any[] | undefined, setStatus: (t: string, k?: 'info' | 'ok' | 'error') => void) {
  if (!subs?.length) return
  const api = (el as any).player as {
    addSubtitleTrack?: (entry: { url: string; lang: string; label: string; format?: string }) => void
    getSubtitleLangs?: () => Array<{ lang: string }>
    selectSubtitleLang?: (lang: string | null) => Promise<boolean>
  } | null
  if (!api?.addSubtitleTrack) return // player belum siap; dicoba lagi pada frame berikutnya
  const have = (api.getSubtitleLangs?.() || []).map((track) => track.lang)
  const missing = subs.filter((sub) => !have.includes(sub.lang))
  if (!missing.length) return

  const key = `${index}:${subs.map((sub) => sub.url || sub.file || sub.lang).join(',')}`
  if (ctl.subsSession === key) return // sudah didaftarkan untuk sesi ini
  ctl.subsSession = key

  // Server mengekstrak subtitle dari seluruh file (bisa lama untuk file
  // besar). Tunggu isinya siap dulu, baru daftarkan + pilih; kalau tidak,
  // track tampak terpilih tapi tidak ada teks sampai ekstraksi selesai.
  const first = subs[0]
  setStatus(`Menyiapkan subtitle ${first.name || first.lang}…`, 'info')
  try {
    const res = await fetch(first.url, { cache: 'no-store' })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    await res.text() // isi masuk cache server: permintaan player berikutnya instan
  } catch (err) {
    if (ctl.subsSession === key) ctl.subsSession = ''
    setStatus(`Subtitle gagal dimuat: ${(err as Error).message}`, 'error')
    return
  }
  if (ctl.subsSession !== key) return // sesi sudah berganti saat menunggu

  for (const sub of subs) {
    api.addSubtitleTrack({ url: sub.url, lang: sub.lang, label: sub.name || sub.lang, format: sub.format || 'vtt' })
  }
  ;(el as any).updateSubtitleTrackMenu?.()
  await api.selectSubtitleLang?.(first.lang)
  setStatus(`Subtitle ${first.name || first.lang} aktif.`, 'ok')
}

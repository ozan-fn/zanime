import { useCallback, useEffect, useRef, useState } from 'react'
import '@vidstack/react/player/styles/default/theme.css'
import '@vidstack/react/player/styles/default/layouts/video.css'
import {
  MediaPlayer,
  MediaProvider,
  Track,
  isHLSProvider,
  type MediaErrorDetail,
  type MediaPlayerInstance,
  type MediaProviderAdapter,
} from '@vidstack/react'
import { DefaultVideoLayout, defaultLayoutIcons } from '@vidstack/react/player/layouts/default'
import Hls from 'hls.js'
import { Upload } from 'lucide-react'
import { log } from '@/lib/logs'
import { Sq } from '@/features/text/Sq'
import { useApp } from '@/features/app/AppProvider'
import { LogPanel } from '@/features/log/LogPanel'
import type { SubtitleMedia } from '@/features/subtitle/SubtitleCard'
import { cueCount, shiftCues, subTracks, toVtt, uniqueLabel } from './tracks'

/* Pemutar: Vidstack Default Layout di atas <video> native, dengan HLS lewat
 * hls.js yang dibundel lokal (default Vidstack mengambilnya dari CDN, jadi
 * library dipasang sendiri di provider-change). Subtitle dipasang sebagai
 * <track> same-origin `/s/<slug>`; proxy server sudah menormalkan VTT, jadi
 * tidak ada URL host video yang menyentuh browser. */

// Delay subtitle: langkah 0,5 detik, maksimal 10 detik ke dua arah.
const DELAY_STEP = 0.5
const DELAY_MAX = 10

const delayLabel = (d: number) => `Sub ${d > 0 ? '+' : d < 0 ? '−' : ''}${Math.abs(d).toFixed(1)}s`

// Track subtitle tambahan (hasil kartu subdl / file sendiri).
export type PlayerSubtitle = { src: string; label: string }

export function Player({
  media,
  subtitle,
  onSubtitle,
  log: showLog = true,
}: {
  media?: SubtitleMedia
  subtitle?: PlayerSubtitle | null
  onSubtitle?: (sub: PlayerSubtitle | null) => void
  /** Panel log di dalam kartu player (desktop). Ponsel memakai tab. */
  log?: boolean
}) {
  const { status, playing } = useApp()
  const current = playing?.src || ''
  const player = useRef<MediaPlayerInstance | null>(null)

  // Identitas konten yang diputar. Ganti judul/episode/movie = konten baru,
  // selalu mulai dari 0; ganti server pada konten yang sama = lanjut menit.
  const content = playing?.media
    ? `${playing.media.tab}:${playing.media.id}:${playing.media.season ?? ''}:${playing.media.episode ?? ''}`
    : playing?.title || ''
  // Posisi terakhir media yang tampil (detik). Nilai 0 diabaikan supaya
  // sumber yang baru dipasang tidak menimpa posisi sebelum sempat dilanjutkan.
  const at = useRef(0)
  const handleTime = useCallback(() => {
    const p = player.current
    if (p && p.currentTime > 0) at.current = p.currentTime
  }, [])
  // Konten baru: buang posisi lama, termasuk saat slug stream kebetulan sama.
  useEffect(() => {
    at.current = 0
    if (player.current) player.current.currentTime = 0
  }, [content])

  // Delay subtitle digeser langsung di cue track (bukan lewat file), jadi
  // berlaku untuk subtitle server maupun file kiriman pengguna. Waktu asli
  // cue disimpan terpisah supaya geseran bisa dinaik-turunkan.
  const [delay, setDelay] = useState(0)
  const delayRef = useRef(0)
  const cueBase = useRef(new WeakMap<object, { startTime: number; endTime: number }>())
  const wired = useRef(new WeakSet<object>())

  const applyDelay = useCallback(() => {
    const p = player.current
    if (!p) return
    for (const track of p.textTracks) {
      if (track.readyState !== 2) continue
      shiftCues(track.cues, delayRef.current, cueBase.current)
    }
  }, [])

  // Track dimuat belakangan (dan cue bisa datang bertahap): pasang ulang delay
  // setiap track selesai dimuat atau menerima cue baru.
  const wireTracks = useCallback(() => {
    const p = player.current
    if (!p) return
    for (const track of p.textTracks) {
      if (wired.current.has(track)) continue
      wired.current.add(track)
      track.addEventListener('load', applyDelay)
      track.addEventListener('add-cue', applyDelay)
    }
  }, [applyDelay])

  // Subtitle tambahan dipegang HomePage (kartunya di bawah panel server);
  // player hanya merawat blob unggahan yang dibuatnya sendiri.
  const blobSrc = useRef('')

  useEffect(
    () => () => {
      if (blobSrc.current) URL.revokeObjectURL(blobSrc.current)
    },
    [],
  )

  // Ganti judul/episode: subtitle lama tidak relevan lagi, jadi dilepas dari
  // player (termasuk blob unggahan). Ganti server tetap mempertahankannya.
  useEffect(() => {
    onSubtitle?.(null)
    if (blobSrc.current) {
      URL.revokeObjectURL(blobSrc.current)
      blobSrc.current = ''
    }
  }, [media, onSubtitle])

  // Blob unggahan yang digantikan subtitle lain (mis. dari kartu) dilepas.
  useEffect(() => {
    if (blobSrc.current && blobSrc.current !== subtitle?.src) {
      URL.revokeObjectURL(blobSrc.current)
      blobSrc.current = ''
    }
  }, [subtitle])

  const loadSub = useCallback(async (file: File) => {
    try {
      const vtt = toVtt(await file.text())
      const cues = cueCount(vtt)
      if (!cues) {
        log.err(`Subtitle tidak berisi cue · ${file.name}`, 'player:sub')
        return
      }
      if (blobSrc.current) URL.revokeObjectURL(blobSrc.current)
      const src = URL.createObjectURL(new Blob([vtt], { type: 'text/vtt' }))
      blobSrc.current = src
      onSubtitle?.({ src, label: file.name.replace(/\.[^.]+$/, '') })
      log.ok(`Subtitle sendiri dimuat · ${file.name} · ${cues} cue`, 'player:sub')
    } catch (err) {
      log.err(`Subtitle gagal dibaca · ${err instanceof Error ? err.message : String(err)}`, 'player:sub')
    }
  }, [onSubtitle])

  // Subtitle sendiri menang: track otomatis dilepas dari default agar tidak
  // rebutan dengan pilihan Indonesia/Inggris. Namanya dibuat unik karena
  // subtitle subdl sering berlabel sama dengan track bawaan server.
  const auto = subTracks(playing?.subs)
  const subLabel = uniqueLabel(
    subtitle?.label || 'Subtitle',
    auto.map((t) => t.label),
  )
  const tracks = subtitle
    ? [
        ...auto.map((t) => ({ ...t, default: false })),
        { src: subtitle.src, kind: 'subtitles' as const, label: subLabel, lang: 'und', default: true },
      ]
    : auto

  const prevDelay = useRef(0)
  useEffect(() => {
    delayRef.current = delay
    if (delay !== prevDelay.current) {
      prevDelay.current = delay
      log.info(delay ? `Subtitle delay ${delay > 0 ? '+' : '−'}${Math.abs(delay).toFixed(1)}s` : 'Subtitle delay kembali normal', 'player:delay')
    }
    applyDelay()
  }, [delay, applyDelay])

  // Klik "pakai" (kartu subtitle atau file sendiri) = track langsung tampil.
  // Atribut `default` pada <Track> hanya berlaku saat elemen dipasang, jadi
  // mode-nya diset eksplisit. Track dicari lewat `src` yang diklik (bukan
  // lewat label/lang), karena label bisa sama dengan track bawaan server.
  const showSub = useCallback(() => {
    const p = player.current
    if (!p || !subtitle) return
    const list = p.textTracks
    const target = list.toArray().find((t) => t.src === subtitle.src)
    if (!target) return
    for (const track of list) track.mode = track === target ? 'showing' : 'disabled'
  }, [subtitle])

  useEffect(() => {
    showSub()
  }, [showSub, current])

  // Elemen player di-mount ulang saat ganti server, jadi track dan player-nya
  // baru: pasang ulang pendengar delay pada instance yang sekarang.
  useEffect(() => {
    const tracks = player.current?.textTracks
    if (!tracks) return
    const onAdd = () => {
      wireTracks()
      applyDelay()
      // Track subtitle pengguna bisa terdaftar belakangan; pasang mode-nya
      // begitu track itu muncul, bukan hanya saat `subtitle` berubah.
      showSub()
    }
    tracks.addEventListener('add', onAdd)
    onAdd()
    return () => tracks.removeEventListener('add', onAdd)
  }, [current, wireTracks, applyDelay, showSub])

  const bumpDelay = useCallback((step: number) => {
    setDelay((v) => Math.max(-DELAY_MAX, Math.min(DELAY_MAX, Math.round((v + step) * 10) / 10)))
  }, [])

  // hls.js dari bundle, bukan jsDelivr.
  const handleProviderChange = useCallback((provider: MediaProviderAdapter | null) => {
    if (isHLSProvider(provider)) provider.library = Hls
  }, [])

  const prevSrc = useRef('')
  useEffect(() => {
    // Ganti server = elemen lama di-unmount (hls.js lama di-destroy).
    if (prevSrc.current && prevSrc.current !== current) log.info('Membatalkan stream sebelumnya', 'player:abort')
    prevSrc.current = current
    if (current) log.info('Memasang sumber video…', 'player:src')
  }, [current])

  const handlePlay = useCallback(() => log.ok('Pemutaran mulai', 'player:play'), [])
  const handleReady = useCallback(() => {
    // Sumber baru untuk konten yang sama (ganti server) melanjutkan posisi
    // terakhir; konten baru sudah membuat `at` = 0.
    const p = player.current
    if (p && Math.abs(p.currentTime - at.current) > 1) {
      p.currentTime = at.current
      const m = Math.floor(at.current / 60)
      const s = String(Math.floor(at.current % 60)).padStart(2, '0')
      log.info(`Ganti server: lanjut dari ${m}:${s}`, 'player:resume')
    }
    log.ok('Video siap diputar', 'player:ready')
  }, [])
  const handleError = useCallback(
    (detail: MediaErrorDetail) => log.err(`Video gagal diputar · ${detail.message || detail.code}`, 'player:err'),
    [],
  )

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-black sm:rounded-xl">
      {playing ? (
        <MediaPlayer
          // key = src: ganti server me-mount ulang elemen, jadi hls.js lama
          // dilepas (abort) dan stream baru mulai dari nol, bukan menunggu
          // buffer lama yang macet.
          key={current}
          ref={player}
          className="aspect-video w-full"
          src={current}
          title={playing.title}
          autoPlay
          playsInline
          storage="zanime"
          onProviderChange={handleProviderChange}
          onPlay={handlePlay}
          onCanPlay={handleReady}
          onTimeUpdate={handleTime}
          onError={handleError}
        >
          <MediaProvider>
            {tracks.map((t) => (
              <Track
                key={t.src}
                src={t.src}
                kind={t.kind}
                label={t.label}
                lang={t.lang}
                type="vtt"
                default={t.default}
              />
            ))}
          </MediaProvider>
          <DefaultVideoLayout icons={defaultLayoutIcons} colorScheme="dark" />
        </MediaPlayer>
      ) : (
        <div className="grid aspect-video place-items-center px-4 text-center text-xs text-zinc-600 sm:text-sm">
          Pilih judul lewat pencarian untuk mulai streaming.
        </div>
      )}
      {(playing || status) && (
        <div className="flex flex-wrap items-center gap-2 border-t border-border bg-card px-3 py-2 sm:flex-nowrap sm:justify-between sm:gap-3 sm:px-4 sm:py-3">
          <div className="min-w-0 flex-1 pr-1">
            {playing?.title && (
              <Sq text={playing.title} className="block text-sm font-medium text-zinc-100" />
            )}
            {(playing?.sub || status) && (
              <Sq
                text={playing?.sub || status?.text || ''}
                className="mt-0.5 block text-[11px] text-zinc-500"
              />
            )}
          </div>
          {playing && (
            <div className="flex shrink-0 items-center rounded-full border border-border bg-zinc-900 text-[11px] text-zinc-300">
              <button
                type="button"
                aria-label="Subtitle 0,5 detik lebih cepat"
                onClick={() => bumpDelay(-DELAY_STEP)}
                disabled={delay <= -DELAY_MAX}
                className="grid h-8 w-8 place-items-center rounded-full transition hover:bg-zinc-800 disabled:opacity-40 sm:h-6 sm:w-6"
              >
                −
              </button>
              <button
                type="button"
                title="Klik untuk mengembalikan delay ke 0"
                onClick={() => setDelay(0)}
                className="min-w-[58px] tabular-nums transition hover:text-white sm:min-w-[62px]"
              >
                {delayLabel(delay)}
              </button>
              <button
                type="button"
                aria-label="Subtitle 0,5 detik lebih lambat"
                onClick={() => bumpDelay(DELAY_STEP)}
                disabled={delay >= DELAY_MAX}
                className="grid h-8 w-8 place-items-center rounded-full transition hover:bg-zinc-800 disabled:opacity-40 sm:h-6 sm:w-6"
              >
                +
              </button>
            </div>
          )}
          {playing && (
          <label
            title={subtitle?.src.startsWith('blob:') ? 'Ganti subtitle sendiri' : 'Subtitle sendiri'}
            aria-label={subtitle?.src.startsWith('blob:') ? 'Ganti subtitle sendiri' : 'Subtitle sendiri'}
            className="grid h-8 w-8 shrink-0 cursor-pointer place-items-center rounded-full border border-border bg-zinc-900 text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800 sm:h-7 sm:w-7"
          >
            <input
              type="file"
              accept=".vtt,.srt,text/vtt,application/x-subrip"
              className="sr-only"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) void loadSub(f)
                e.target.value = ''
              }}
            />
            <Upload className="h-3.5 w-3.5" />
          </label>
          )}
          <span className="hidden shrink-0 items-center gap-1 rounded-full border border-border bg-zinc-900 px-2.5 py-1 text-[11px] text-zinc-400 sm:inline-flex">
            <svg viewBox="0 0 24 24" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
              <circle cx="12" cy="12" r="2" fill="currentColor" stroke="none" />
              <path d="M16.2 3.8a9 9 0 0 1 0 16.4M7.8 20.2a9 9 0 0 1 0-16.4M19.8 7.2a12 12 0 0 1 0 9.6M4.2 16.8a12 12 0 0 1 0-9.6" />
            </svg>
            streaming
          </span>
        </div>
      )}
      {showLog && (
        <LogPanel className="border-t border-border" panelClassName="rounded-none" maxHeight="max-h-32" />
      )}
    </div>
  )
}

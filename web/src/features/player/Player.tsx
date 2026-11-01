import { useCallback, useEffect } from 'react'
import '@vidstack/react/player/styles/default/theme.css'
import '@vidstack/react/player/styles/default/layouts/video.css'
import { MediaPlayer, MediaProvider, Track, isHLSProvider, type MediaProviderAdapter, type MediaErrorDetail } from '@vidstack/react'
import { DefaultVideoLayout, defaultLayoutIcons } from '@vidstack/react/player/layouts/default'
import Hls from 'hls.js'
import { log } from '@/lib/logs'
import { Sq } from '@/features/text/Sq'
import { useApp } from '@/features/app/AppProvider'
import { LogPanel } from '@/features/log/LogPanel'
import { subTracks } from './tracks'

/* Pemutar: Vidstack Default Layout di atas <video> native, dengan HLS lewat
 * hls.js yang dibundel lokal (default Vidstack mengambilnya dari CDN, jadi
 * library dipasang sendiri di provider-change). Subtitle dipasang sebagai
 * <track> same-origin `/s/<slug>`; proxy server sudah menormalkan VTT, jadi
 * tidak ada URL host video yang menyentuh browser. */

export function Player() {
  const { status, playing } = useApp()
  const current = playing?.src || ''
  const tracks = subTracks(playing?.subs)

  // hls.js dari bundle, bukan jsDelivr.
  const handleProviderChange = useCallback((provider: MediaProviderAdapter | null) => {
    if (isHLSProvider(provider)) provider.library = Hls
  }, [])

  useEffect(() => {
    if (current) log.info('Memasang sumber video…', 'player:src')
  }, [current, playing])

  const handlePlay = useCallback(() => log.ok('Pemutaran mulai', 'player:play'), [])
  const handleReady = useCallback(() => log.ok('Video siap diputar', 'player:ready'), [])
  const handleError = useCallback(
    (detail: MediaErrorDetail) => log.err(`Video gagal diputar · ${detail.message || detail.code}`, 'player:err'),
    [],
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-black">
      {playing ? (
        <MediaPlayer
          // key = src: ganti server me-mount ulang elemen, jadi hls.js lama
          // dilepas (abort) dan stream baru mulai dari nol, bukan menunggu
          // buffer lama yang macet.
          key={current}
          className="aspect-video w-full lg:aspect-auto lg:min-h-0 lg:flex-1"
          src={current}
          title={playing.title}
          autoPlay
          playsInline
          storage="zanime"
          onProviderChange={handleProviderChange}
          onPlay={handlePlay}
          onCanPlay={handleReady}
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
        <div className="grid aspect-video place-items-center text-sm text-zinc-600 lg:aspect-auto lg:flex-1">
          Pilih judul lewat pencarian untuk mulai streaming.
        </div>
      )}
      {(playing || status) && (
        <div className="flex shrink-0 items-center justify-between gap-3 border-t border-border bg-card px-4 py-3">
          <div className="min-w-0">
            {playing?.title && <Sq text={playing.title} className="text-sm font-medium text-zinc-100" />}
            {(playing?.sub || status) && (
              <Sq
                text={playing?.sub || status?.text || ''}
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
      {/* Log realtime di bawah player: pemuatan stream + video oleh player. */}
      <LogPanel className="shrink-0 border-t border-border" panelClassName="rounded-none" maxHeight="max-h-40 lg:max-h-32" />
    </div>
  )
}

import { useEffect, useRef, useState } from 'react';
import shaka from 'shaka-player/dist/shaka-player.ui';
import 'shaka-player/dist/controls.css';
import type { SubtitleStatus } from '../lib/types';
import { Icon } from './ui/Icon';

interface PlayerProps {
  src: string;
  subtitle?: string;
  status: SubtitleStatus | null;
}

// Antarmuka shaka dideklarasikan lokal (tipe paket tidak diekspor rapi untuk
// bundler). Hanya anggota yang dipakai.
interface ShakaControlsLike {
  getPlayer(): ShakaPlayerLike;
  addEventListener(type: 'showingui' | 'hidingui', listener: () => void): void;
}
interface ShakaOverlayLike {
  configure(config: Record<string, unknown>): void;
  getControls(): ShakaControlsLike;
  destroy(): Promise<void>;
}
interface ShakaPlayerLike {
  attach(video: HTMLVideoElement): Promise<void>;
  load(src: string, start: number, mime: string): Promise<void>;
  addTextTrackAsync(
    uri: string,
    lang: string,
    kind: string,
    mime: string,
    codecs?: string,
    label?: string,
  ): Promise<unknown>;
  selectTextTrack(track: unknown): void;
  unload(): Promise<void>;
  destroy(): Promise<void>;
}

// Player: shaka.ui.Overlay (docs resmi "Programmatic UI setup") — player + kontrol
// dibuat dari satu container, jadi menu resolusi (quality) dan subtitle on/off
// (captions) bawaan shaka ikut terpasang. Overlay menaruh kontrol di dalam box;
// video elemen dibuat sendiri supaya React punya ref-nya.
// addTextTrackAsync hanya boleh dipanggil SETELAH load() selesai — docs shaka:
// tanpa konten yang sudah dimuat ia melempar CONTENT_NOT_LOADED (7004). Karena itu
// mount subtitle dibelokan lewat state `loaded`, bukan hanya status subtitle.
export function Player({ src, subtitle, status }: PlayerProps) {
  const boxRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const playerRef = useRef<ShakaPlayerLike | null>(null);
  const [failed, setFailed] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState(false);
  const [badge, setBadge] = useState(false);
  const [pseudoCapable, setPseudoCapable] = useState(false);
  const [pseudoFs, setPseudoFs] = useState(false);
  const [uiVisible, setUiVisible] = useState(true);

  useEffect(() => {
    const video = videoRef.current;
    const box = boxRef.current;
    if (!video || !box) return;
    let alive = true;

    shaka.polyfill.installAll();
    if (!shaka.Player.isBrowserSupported()) {
      setFailed('Browser ini tidak mendukung pemutar video.');
      return;
    }
    const localPlayer = new shaka.Player();
    // iPhone Safari: document.fullscreenEnabled false → shaka (docs
    // ui/controls.js: shouldUseDocumentFullscreen_) jatuh ke
    // video.webkitEnterFullscreen = player native iOS, yang menggambar subtitle
    // sendiri di posisi 0px dan kebal CSS. Tidak ada opsi UI untuk mematikan
    // jalur itu (shaka.extern.UIConfiguration), jadi serah ke player native
    // dicegah: tombol fullscreen + dblclick + rotasi dimatikan, lalu container
    // dibuat fullscreen lewat CSS (pseudo-fullscreen) supaya UITextDisplayer +
    // gap .shaka-text-container tetap berlaku.
    const pseudo = !document.fullscreenEnabled && 'webkitEnterFullscreen' in video;
    setPseudoCapable(pseudo);
    setPseudoFs(false);
    const ui: ShakaOverlayLike = new (shaka.ui.Overlay as unknown as new (
      p: unknown,
      container: HTMLElement,
      vid: HTMLVideoElement,
    ) => ShakaOverlayLike)(localPlayer, box, video);
    ui.configure({
      controlPanelElements: [
        'play_pause',
        'time_and_duration',
        'spacer',
        'mute_volume',
        'captions',
        'quality',
        ...(pseudo ? [] : ['fullscreen']),
      ],
      overflowMenuButtons: ['quality', 'captions', 'playback_rate'],
      doubleClickForFullscreen: !pseudo,
      enableFullscreenOnRotation: !pseudo,
      // Docs UIConfiguration: "The delay (in seconds) before fading out the
      // controls" — default 0, jadi 3 detik sesuai harapan.
      fadeDelay: 3,
    });
    const controls = ui.getControls();
    // Docs ui/controls.js: Controls men-dispatch 'showingui' / 'hidingui'
    // setiap visibilitas bar berubah — tombol fullscreen ikut event itu.
    controls.addEventListener('showingui', () => {
      if (alive) setUiVisible(true);
    });
    controls.addEventListener('hidingui', () => {
      if (alive) setUiVisible(false);
    });
    const p = controls.getPlayer();
    setLoaded(false);
    setFailed('');
    p.attach(video)
      .then(() => {
        if (alive) return p.load(src, 0, 'application/vnd.apple.mpegurl');
      })
      .then(() => {
        if (alive) setLoaded(true);
      })
      .catch((e: { code?: number; message?: string }) => {
        if (alive) setFailed(`Stream gagal dimuat: kode ${e.code || e.message || e}.`);
      });
    playerRef.current = p;

    return () => {
      alive = false;
      p.unload().catch(() => {});
      ui.destroy().catch(() => {});
      p.destroy().catch(() => {});
      playerRef.current = null;
    };
  }, [src, attempt]);

  // Halaman di belakang tidak boleh bergulir saat pseudo-fullscreen aktif.
  useEffect(() => {
    document.body.style.overflow = pseudoFs ? 'hidden' : '';
    return () => {
      document.body.style.overflow = '';
    };
  }, [pseudoFs]);

  useEffect(() => {
    const p = playerRef.current;
    if (!p || !subtitle || !loaded || status?.state !== 'ready') return;
    let alive = true;
    p.addTextTrackAsync(subtitle, 'id', 'subtitles', 'text/vtt', undefined, 'Indonesia')
      .then((track) => {
        if (!alive) return;
        p.selectTextTrack(track);
        setBadge(true);
        setTimeout(() => setBadge(false), 2500);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [subtitle, loaded, status?.state]);

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card shadow-2xl shadow-black/50">
      <div
        ref={boxRef}
        style={
          pseudoFs
            ? { position: 'fixed', inset: 0, zIndex: 60, aspectRatio: 'auto', width: '100vw', height: '100dvh' }
            : undefined
        }
        className="relative aspect-video w-full bg-black"
      >
        <video ref={videoRef} playsInline preload="auto" className="h-full w-full object-contain" />
        {pseudoCapable && (
          <button
            type="button"
            aria-label={pseudoFs ? 'Keluar dari layar penuh' : 'Layar penuh'}
            onClick={() => setPseudoFs((v) => !v)}
            className={`absolute right-3 top-3 z-30 flex size-9 items-center justify-center rounded-lg bg-black/60 text-white backdrop-blur transition-opacity duration-300 hover:bg-black/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground ${
              uiVisible ? 'opacity-100' : 'pointer-events-none opacity-0'
            }`}
          >
            <Icon name={pseudoFs ? 'minimize' : 'maximize'} className="size-5" />
          </button>
        )}
        <div
          role="status"
          style={{ opacity: badge ? 1 : 0 }}
          className={`pointer-events-none absolute right-3 z-30 flex size-9 items-center justify-center rounded-lg bg-black/60 text-white backdrop-blur transition-opacity duration-300 ${
            pseudoCapable ? 'top-14' : 'top-3'
          }`}
        >
          <Icon name="cc" className="size-5" />
          <span className="sr-only">Subtitle Indonesia aktif</span>
        </div>
      </div>
      {failed && (
        <div
          className="flex flex-wrap items-center gap-3 border-t border-border p-3 text-sm text-destructive"
          role="alert"
        >
          <span>{failed}</span>
          <button
            type="button"
            onClick={() => setAttempt((n) => n + 1)}
            className="ml-auto inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-card px-2.5 text-foreground hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
          >
            <Icon name="rotate-cw" className="size-3.5" /> Coba lagi
          </button>
        </div>
      )}
    </div>
  );
}

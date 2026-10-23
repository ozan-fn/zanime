<script lang="ts">
  import '@videojs/html/video/player';
  import '@videojs/html/video/skin';
  import '@videojs/html/media/hlsjs-video';
  import { abs } from '../../../lib/api';
  import { shiftVtt } from '../../../lib/vtt';

  let {
    src,
    sub,
    subId = '',
    subDelay = 0,
    startAt = 0,
    onProgress = () => {},
    nextAt = -1,
    onNeedMore = () => {},
    onSeek = () => {},
    onIdActive = () => {},
    onIdInactive = () => {},
    autoSkip = false,
    skips = [],
    idSub = false,
    subUntil = 0,
    subDone = false,
    onEnded = () => {},
    onDuration = (d: number) => {},
  }: { src: string; sub: string; subId?: string; subDelay?: number; startAt?: number; onProgress?: (t: number) => void; nextAt?: number; onNeedMore?: (t: number) => void; onSeek?: (t: number) => void; onIdActive?: () => void; onIdInactive?: () => void; autoSkip?: boolean; skips?: Array<{ interval: { startTime: number; endTime: number } }>; idSub?: boolean; subUntil?: number; subDone?: boolean; onEnded?: () => void; onDuration?: (d: number) => void } = $props();
  const url = $derived(abs(src));
  const subUrl = $derived(sub ? abs(sub) : '');
  const idUrl = $derived(subId ? abs(subId) : '');

  let playerEl: any = $state(null);
  let mediaEl: any = $state(null);
  let idTrackEl: HTMLTrackElement | null = $state(null);

  // Delay subtitle: timestamp VTT digeser di klien, jadi tak ada yang perlu diubah di backend.
  // Saat delay 0 URL asli dipakai apa adanya (tanpa fetch tambahan).
  // Kosong saat delay 0 → src memakai URL asli langsung (tanpa nunggu effect).
  let enSrc = $state('');
  let idSrc = $state('');
  const blobs = new Set<string>();
  async function trackUrl(u: string, d: number): Promise<string> {
    if (!u || !d) return u;
    const t = await (await fetch(u)).text();
    const b = URL.createObjectURL(new Blob([shiftVtt(t, d)], { type: 'text/vtt' }));
    blobs.add(b);
    return b;
  }
  // shortcut: blob lama hanya di-revoke saat player dibongkar — tiap ganti delay/batch
  // meninggalkan satu blob kecil (~25 KB) sampai itu.
  $effect(() => () => {
    for (const b of blobs) URL.revokeObjectURL(b);
    blobs.clear();
  });
  $effect(() => {
    const u = subUrl;
    const d = subDelay;
    let dead = false;
    void trackUrl(u, d).then((x) => {
      if (!dead) enSrc = x;
    });
    return () => {
      dead = true;
    };
  });

  /** Batas buffer hls.js (default 30 dtk / 60 MB di depan → preload langsung tarik puluhan MB). */
  const HLS_LIMITS = { maxBufferLength: 10, maxMaxBufferLength: 20, maxBufferSize: 8 * 1000 * 1000 };

  // Lewat `source` (bukan atribut `src`): opsi engine hanya dibaca dari sini, dan engine
  // dibangun ulang saat config berubah — jadi batas ini berlaku sejak load pertama.
  $effect(() => {
    const el = mediaEl;
    if (!el) return;
    el.source = { src: url, engine: { hlsJs: HLS_LIMITS } };
    // Media diganti di player yang sama saat ganti episode: `autoplay` hanya berlaku
    // untuk elemen saat pertama dimuat, jadi episode berikutnya harus diputar ulang manual.
    playerEl?.store?.play?.()?.catch(() => {});
  });

  // src tak sama → reload sinkron ke fetch terbaru (URL stabil, dibedakan &v=).
  // Delay subtitle ikut memicu ulang (teks digeser lalu dipasang sebagai blob baru).
  $effect(() => {
    const u = idUrl;
    const d = subDelay;
    let dead = false;
    void trackUrl(u, d).then((x) => {
      if (dead) return;
      idSrc = x;
      const tr = idTrackEl;
      if (!tr || !x) return;
      const mode = tr.track?.mode;
      tr.src = x;
      (tr as any).load?.();
      if (mode === 'showing') tr.track.mode = 'showing';
    });
    return () => {
      dead = true;
    };
  });

  // Satu timer 500ms untuk durasi + auto-skip OP/ED + lanjut-dari-posisi-terakhir (dulu 2 interval).
  let lastDur = $state(0); // $state: tanda skip baru tergambar setelah durasi diketahui
  let resumed = false;
  // Subtitle ID: tahan video hanya ketika cue terjemahan benar-benar habis (fetch
  // berikutnya belum tiba). Prefetch yang masih punya cadangan cue tidak menjeda.
  let heldSub = false;
  let heldWasPlaying = false;
  /** Interval OP/ED yang memuat titik resume → jangan di-skip (posisi tersimpan menang). */
  let skipResumeStart = -1;
  $effect(() => {
    const u = url;
    resumed = false; // src baru (episode lain) → boleh lanjut dari posisi tersimpan lagi
    heldSub = false;
    heldWasPlaying = false;
    skipResumeStart = -1;
  });
  $effect(() => {
    const list = skips;
    const on = autoSkip;
    const start = startAt;
    const iv = setInterval(() => {
      const st = playerEl?.store?.state;
      if (!st) return;
      if (isFinite(st.duration) && st.duration > 0) {
        if (lastDur === 0) lastDur = st.duration;
        onDuration(st.duration);
      }
      if (!resumed && start > 5 && st.duration > start + 10) {
        resumed = true;
        playerEl.store.seek?.(start);
        skipResumeStart = list.find(
          (s) => start >= s.interval.startTime && start < s.interval.endTime,
        )?.interval.startTime ?? -1;
      }
      if (st.currentTime > 0) onProgress(st.currentTime);
      // subUntil 0 = belum ada cue sama sekali → jangan tahan (kuota mati = video beku).
      const hold = idSub && !subDone && subUntil > 0 && (st.currentTime ?? 0) >= subUntil - 0.2;
      if (hold && !heldSub) {
        heldSub = true;
        heldWasPlaying = !st.paused;
        playerEl.store.pause?.();
      } else if (!hold && heldSub) {
        heldSub = false;
        if (heldWasPlaying) playerEl.store.play?.();
        heldWasPlaying = false;
      }
      if (!on || !list.length) return;
      // Resume tersimpan belum diterapkan → biarkan posisi itu yang menang, bukan skip.
      if (start > 5 && !resumed) return;
      const t = st.currentTime ?? 0;
      const hit = list.find((s) => t >= s.interval.startTime && t < s.interval.endTime);
      if (hit && hit.interval.startTime !== skipResumeStart) {
        playerEl.store.seek?.(hit.interval.endTime + 0.05);
      }
    }, 500);
    return () => clearInterval(iv);
  });

  // Tanda kuning OP/ED di seek bar (overlay pada media-time-slider).
  $effect(() => {
    const dur = lastDur;
    const list = skips;
    const slider = document.querySelector('media-time-slider');
    if (!slider || dur <= 0) return;
    (slider as HTMLElement).style.position = (slider as HTMLElement).style.position || 'relative';
    slider.querySelectorAll('[data-skipmark]').forEach((n) => n.remove());
    for (const s of list) {
      const m = document.createElement('div');
      m.setAttribute('data-skipmark', '1');
      const l = (s.interval.startTime / dur) * 100;
      const w = ((s.interval.endTime - s.interval.startTime) / dur) * 100;
      m.style.cssText = `position:absolute;top:50%;translate:0 -50%;left:${l}%;width:${Math.max(w, 0.5)}%;height:6px;background:rgb(250 204 21);border-radius:3px;pointer-events:none;z-index:3;`;
      slider.appendChild(m);
    }
  });
  let endedState = false;
  $effect(() => {
    const p = playerEl;
    if (!p?.store) return;
    const un = p.store.subscribe(() => {
      if (p.store.state?.ended && !endedState) {
        endedState = true;
        onEnded();
      } else if (!p.store.state?.ended) endedState = false;
    });
    return un;
  });

  // Mode trek ID dari player store; sekalian cek pemicu batch (time-based).
  // Trek terakhir terpilih disimpan ke localStorage dan dipulihkan saat list tiba.
  // Bergantung pada `url`: player tak lagi di-mount ulang per episode (container tetap agar
  // fullscreen tak putus), jadi pemulihan harus di-arm ulang tiap sumber.
  $effect(() => {
    const p = playerEl;
    if (!p?.store || !url) return;
    let idOn = false;
    let restored = false;
    let lastShown: string | null = null;
    let lastT = -1;
    const un = p.store.subscribe(() => {
      const tracks = (p.store.state?.textTrackList ?? []) as any[];
      const subs = tracks.filter((t) => t.kind === 'subtitles' || t.kind === 'captions');
      if (!restored && subs.length > 0) {
        restored = true;
        const saved = localStorage.getItem('zanime-sub');
        if (saved === 'off') p.store.selectSubtitlesTrack?.(null);
        else if (saved) {
          const m = subs.find((t) => t.language === saved);
          if (m) p.store.selectSubtitlesTrack?.(m.id);
        }
      }
      const showing = subs.find((t) => t.mode === 'showing');
      if (restored) {
        if (showing && showing.language !== lastShown) {
          lastShown = showing.language;
          localStorage.setItem('zanime-sub', showing.language);
        } else if (!showing && lastShown !== null && lastShown !== 'off') {
          lastShown = 'off';
          localStorage.setItem('zanime-sub', 'off');
        }
      }
      const idShowing = subs.some((t) => t.language === 'id' && t.mode === 'showing');
      if (idShowing !== idOn) {
        idOn = idShowing;
        idShowing ? onIdActive() : onIdInactive();
      }
      const t = p.store.state?.currentTime ?? 0;
      if (idOn) {
        if (lastT >= 0 && Math.abs(t - lastT) > 3) onSeek(t);
        if (nextAt >= 0 && t >= nextAt - 8) onNeedMore(t);
      }
      lastT = t;
    });
    return un;
  });

  // Kualitas: pulihkan pilihan terakhir tiap sumber, lalu simpan perubahan user.
  // Yang disimpan tinggi rendisi — id rendisi hls.js cuma indeks level, artinya beda antar episode.
  $effect(() => {
    const p = playerEl;
    if (!p?.store || !url) return;
    let restoring = true;
    let last = '';
    const un = p.store.subscribe(() => {
      const list = (p.store.state?.videoRenditionList ?? []) as Array<{ id: string; height?: number; selected: boolean }>;
      if (!list.length) return;
      if (restoring) {
        restoring = false;
        const saved = localStorage.getItem('zanime-quality') ?? 'auto';
        const hit = saved === 'auto' ? undefined : list.find((r) => String(r.height) === saved);
        p.store.selectVideoRendition?.(hit?.id ?? 'auto');
        last = hit ? saved : 'auto'; // hasil pemulihan jangan ditulis balik
        return;
      }
      const sel = list.find((r) => r.selected);
      const v = sel?.height != null ? String(sel.height) : 'auto';
      if (v !== last) {
        last = v;
        localStorage.setItem('zanime-quality', v);
      }
    });
    return un;
  });
</script>

<video-player bind:this={playerEl}>
  <!-- Lebar dibatasi 16:9 dari tinggi layar: video selalu utuh (tanpa terpotong),
       dan container/track subtitle ikut pas di dasar gambar seperti mode fullscreen.
       Sudut siku (radius 0) karena video ini menempel ke layar. -->
  <video-skin style="--media-video-border-radius: 0; display: block; width: min(100%, calc(100dvh * 16 / 9)); aspect-ratio: 16 / 9;">
    <!-- Kunci hanya media, bukan <video-player>: elemen fullscreen (media-container di dalam
         skin) tetap ter-mount saat ganti episode, jadi fullscreen tak putus di auto next. -->
    {#key url}
      <!-- hlsjs-video, bukan hls-video: sumber CDN MPEG-TS, dan hls-video (SPF) tak mendukung TS -->
      <hlsjs-video bind:this={mediaEl} autoplay preload="metadata" playsinline crossorigin="anonymous" style="width: 100%;">
        {#if subUrl}
          <track kind="subtitles" src={subDelay ? enSrc : subUrl} srclang="en" label="English" default />
        {/if}
        {#if idUrl}
          <track kind="subtitles" src={subDelay ? idSrc : idUrl} srclang="id" label="Indonesia" bind:this={idTrackEl} />
        {/if}
      </hlsjs-video>
    {/key}
  </video-skin>
</video-player>

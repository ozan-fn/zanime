<script lang="ts">
  import '@videojs/html/video/player';
  import '@videojs/html/video/skin';
  import '@videojs/html/media/hlsjs-video';
  import { abs } from '../../../lib/api';

  let {
    src,
    sub,
    subId = '',
    nextAt = -1,
    onNeedMore = () => {},
    onSeek = () => {},
    onIdActive = () => {},
    onIdInactive = () => {},
    autoSkip = false,
    skips = [],
    busy = false,
    onEnded = () => {},
    onDuration = (d: number) => {},
  }: { src: string; sub: string; subId?: string; nextAt?: number; onNeedMore?: (t: number) => void; onSeek?: (t: number) => void; onIdActive?: () => void; onIdInactive?: () => void; autoSkip?: boolean; skips?: Array<{ interval: { startTime: number; endTime: number } }>; busy?: boolean; onEnded?: () => void; onDuration?: (d: number) => void } = $props();
  const url = $derived(abs(src));
  const subUrl = $derived(sub ? abs(sub) : '');
  const idUrl = $derived(subId ? abs(subId) : '');

  let playerEl: any = $state(null);
  let idTrackEl: HTMLTrackElement | null = $state(null);

  // src tak sama → reload sinkron ke fetch terbaru (URL stabil, dibedakan &v=).
  $effect(() => {
    const tr = idTrackEl;
    const u = idUrl;
    if (tr && u) {
      const mode = tr.track?.mode;
      tr.src = u;
      (tr as any).load?.();
      if (mode === 'showing') tr.track.mode = 'showing';
    }
  });

  let lastDur = 0;
  $effect(() => {
    const iv = setInterval(() => {
      const st = playerEl?.store?.state;
      if (st && isFinite(st.duration) && st.duration > 0) {
        if (lastDur === 0) lastDur = st.duration;
        onDuration(st.duration);
      }
    }, 1000);
    return () => clearInterval(iv);
  });

  // Tanda kuning OP/ED di seek bar (overlay pada media-time-slider).
  // Auto skip OP/ED (polling langsung; autoSkip/skips dari prop).
  $effect(() => {
    const list = skips;
    const on = autoSkip;
    const iv = setInterval(() => {
      if (!on || !list.length) return;
      const st = playerEl?.store?.state;
      if (!st) return;
      const t = st.currentTime ?? 0;
      const hit = list.find((s) => t >= s.interval.startTime && t < s.interval.endTime);
      if (hit) playerEl.store.seek?.(hit.interval.endTime + 0.05);
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

  // Jeda hanya saat fetch subtitle akibat seek; fetch otomatis (nextAt) tidak menjeda.
  let wasPlaying = false;
  let seekBusy = false;
  $effect(() => {
    const p = playerEl;
    if (!p?.store) return;
    if (busy && seekBusy) {
      wasPlaying = !p.store.state?.paused;
      p.store.pause?.();
    } else if (!busy && seekBusy) {
      seekBusy = false;
      if (wasPlaying) p.store.play?.();
      wasPlaying = false;
    }
  });

  // Mode trek ID dari player store; sekalian cek pemicu batch (time-based).
  // Trek terakhir terpilih disimpan ke localStorage dan dipulihkan saat list tiba.
  $effect(() => {
    const p = playerEl;
    if (!p?.store) return;
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
        console.log('[9] idShowing=' + idShowing);
        idOn = idShowing;
        idShowing ? onIdActive() : onIdInactive();
      }
      const t = p.store.state?.currentTime ?? 0;
      if (idOn) {
        if (lastT >= 0 && Math.abs(t - lastT) > 3) {
          console.log('[10] seek -> needMore force');
          const tr = document.querySelector<HTMLTrackElement>('track[srclang="id"]');
          console.log('[10b] id track mode=' + tr?.track?.mode + ' cues=' + (tr?.track?.cues?.length ?? 'n/a') + ' active=' + (tr?.track?.activeCues?.length ?? 'n/a'));
          seekBusy = true;
          onSeek(t);
        }
        if (nextAt >= 0 && t >= nextAt - 8) { console.log('[10] t=' + t.toFixed(1) + ' >= nextAt-8=' + (nextAt - 8)); onNeedMore(t); }
      }
      lastT = t;
    });
    return un;
  });
</script>

{#key url}
  <video-player bind:this={playerEl}>
    <video-skin style="display: block; width: 100%; aspect-ratio: 16 / 9;">
      <!-- hlsjs-video, bukan hls-video: sumber CDN MPEG-TS, dan hls-video (SPF) tak mendukung TS -->
      <hlsjs-video src={url} autoplay playsinline crossorigin="anonymous" style="width: 100%;">
        {#if subUrl}
          <track kind="subtitles" src={subUrl} srclang="en" label="English" default />
        {/if}
        {#if idUrl}
          <track kind="subtitles" src={idUrl} srclang="id" label="Indonesia" bind:this={idTrackEl} />
        {/if}
      </hlsjs-video>
    </video-skin>
  </video-player>
{/key}

<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { untrack } from 'svelte';
  import { ChevronLeft, ChevronRight, LoaderCircle, TriangleAlert } from '@lucide/svelte';
  import { abs } from '../lib/api';
  import { detailTitle, fetchDetail, imgOf, type AnimeDetail } from '../features/episodes';
  import Player from '../features/watch/components/Player.svelte';
  import { error, fetchSkiptimes, getSubStatus, loadStream, loading, postSubBatch, stream, subResultUrl, type SkipTime } from '../features/watch';
  import { FastForward } from '@lucide/svelte';

  let { params = {} }: { params?: Record<string, string> } = $props();
  const id = $derived(params.id ?? '');
  const ep = $derived(Number(params.ep ?? 1));

  let subId = $state('');
  let subProg = $state('');
  let nextAt = $state(-1);
  let jobId = '';
  let subDone = false;
  let fetching = $state(false);
  let wantFs = false;
  let lastTry = 0;
  let lastDone = -1;

  let vttCache = 'WEBVTT\n\n';
  let lastBlob = '';

  /** Gabungkan delta VTT → blob URL baru; trek di-reload via src baru. */
  function pushVtt(delta: string): void {
    const d = delta?.trim();
    if (!d) return;
    vttCache += '\n' + d + '\n';
    if (lastBlob) URL.revokeObjectURL(lastBlob);
    lastBlob = URL.createObjectURL(new Blob([vttCache], { type: 'text/vtt' }));
    console.log('[1] pushVtt vttCache=' + vttCache.length);
    subId = lastBlob;
  }

  /** Fetch hasil terjemahan sejauh ini (mis. cache disk) sekali di awal. */
  async function loadBaseVtt(): Promise<void> {
    try {
      const r = await fetch(subResultUrl(jobId));
      const t = await r.text();
      console.log('[7] loadBaseVtt len=' + t.length);
      if (t.trim().length > 'WEBVTT'.length) {
        vttCache = t.trimEnd() + '\n';
        if (lastBlob) URL.revokeObjectURL(lastBlob);
        lastBlob = URL.createObjectURL(new Blob([vttCache], { type: 'text/vtt' }));
        subId = lastBlob;
      }
    } catch {}
  }

  /** Batch pertama langsung; berikut saat putar sisa 3 cue. Cooldown 15 dtk
   *  cegah loop timeupdate (gagal/tanpa progres tak dipanggil berulang). */
  async function needMore(at?: number, force = false): Promise<void> {
    if (!idActive || fetching || subDone || !jobId) { console.log('[3] needMore blocked idActive=' + idActive + ' fetching=' + fetching + ' subDone=' + subDone + ' jobId=' + !!jobId); return; }
    const now = Date.now();
    if (!force && now - lastTry < 15000) { console.log('[3] needMore cooldown'); return; }
    console.log('[3] needMore at=' + at + ' force=' + force);
    lastTry = now;
    fetching = true;
    try {
      let st: Awaited<ReturnType<typeof postSubBatch>> | null = null;
      for (let i = 0; i < 3 && !st; i++) {
        try {
          st = await postSubBatch(jobId, at);
        } catch (e) {
          if (i === 2) throw e;
          subProg = `Sedang antre, coba lagi (${i + 1}/2)…`;
          await new Promise((r) => setTimeout(r, 1000 * (i + 1)));
        }
      }
      if (st) {
      console.log('[4] batch done=' + st.done + ' state=' + st.state + ' next_at=' + st.next_at + ' vtt=' + (st.vtt?.length ?? 0));
      if (st.done !== lastDone) lastDone = st.done;
      if (st.vtt) pushVtt(st.vtt);
      subDone = st.state === 'done';
      nextAt = st.next_at;
      subProg = '';
      }
    } catch (e) {
      subProg = `Subtitle gagal: ${e instanceof Error ? e.message : 'unknown'}`;
    }
    fetching = false;
  }

  let started = false;
  let idActive = $state(false);
  let gen = 0;

  /** Dipicu saat user pilih trek Indonesia di player → mulai alur batch. */
  function startId(): void {
    console.log('[5] startId');
    idActive = true;
    if (started) return;
    started = true;
    void loadBaseVtt();
    const g = gen;
    (async () => {
      try {
        for (let i = 0; i < 30 && gen === g; i++) {
          const st = await getSubStatus(jobId);
          if (st.state === 'failed') {
            subProg = `Subtitle gagal: ${st.error ?? 'unknown'}`;
            return;
          }
          if (st.total > 0 || st.state === 'done') break;
          subProg = st.queue > 0 ? `Antri subtitle Indonesia… #${st.queue} dari ${st.queue_total} (±${Math.ceil(st.eta_sec / 60)} mnt)` : 'Menyiapkan subtitle…';
          await new Promise((r) => setTimeout(r, 1000));
        }
        if (gen === g) await needMore(0);
      } catch (e) {
        if (gen === g) subProg = `Subtitle gagal: ${e instanceof Error ? e.message : 'unknown'}`;
      }
    })();
  }

  function stopId(): void {
    console.log('[6] stopId');
    idActive = false;
  }

  $effect(() => {
    const s = $stream;
    subId = '';
    subProg = '';
    nextAt = -1;
    jobId = '';
    subDone = false;
    fetching = false;
    lastTry = 0;
    lastDone = -1;
    started = false;
    idActive = false;
    vttCache = 'WEBVTT\n\n';
    if (lastBlob) URL.revokeObjectURL(lastBlob);
    lastBlob = '';
    subId = '';
    if (!s?.sub_id) return;
    jobId = s.sub_id;
    subId = subResultUrl(jobId);
    console.log('[8] stream sub_id=' + jobId);
    gen += 1;
  });

  let anime = $state<AnimeDetail | null>(null);
  let skips = $state<SkipTime[]>([]);
  let mal = 0;
  let dur = 0;
  function trySkip(): void {
    if (!mal) return;
    // Fallback ke durasi dari detail anime (menit) bila stream tak melaporkan durasi (TS/VOD).
    const len = dur > 0 ? dur : (anime?.duration ? anime.duration * 60 : 0);
    if (len > 0) void fetchSkiptimes(mal, ep, len).then((sks) => (skips = sks)).catch(() => {});
  }
  let autoSkip = $state(localStorage.getItem('zanime-autoskip') === '1');
  let autoNext = $state(localStorage.getItem('zanime-autonext') !== '0');
  let cover = $derived(anime ? abs(imgOf(anime.coverImage)) : '');
  let chips = $derived(
    anime
      ? [
          anime.format,
          anime.season ? `${anime.season} ${anime.seasonYear ?? ''}`.trim() : anime.seasonYear?.toString(),
          anime.status,
          anime.duration ? `${anime.duration} mnt` : '',
          anime.episodeCount ? `${anime.episodeCount} eps` : '',
          anime.averageScore ? `★ ${(anime.averageScore / 10).toFixed(1)}` : '',
          ...(anime.studios ?? []),
          ...(anime.genres ?? []),
        ].filter((c): c is string => !!c)
      : [],
  );

  $effect(() => {
    if (id) void loadStream(id, ep);
  });
  $effect(() => {
    if ($stream && wantFs) {
      wantFs = false;
      let n = 0;
      const iv = setInterval(() => {
        const el = document.querySelector('video-player');
        if (el) {
          clearInterval(iv);
          el.requestFullscreen?.().catch(() => {});
        } else if (++n > 20) clearInterval(iv);
      }, 500);
    }
  });
  $effect(() => {
    const i = id;
    if (!i) return;
    let alive = true;
    skips = [];
    fetchDetail(i)
      .then((v) => {
        if (alive) {
          anime = v;
          mal = v.malId ?? 0;
          trySkip();
        }
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  });
</script>

<div class="mx-auto max-w-5xl space-y-4 p-4">
  <div class="flex items-center justify-between">
    <a href="/anime/{id}" use:link class="inline-flex items-center gap-1 text-sm text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">
      <ChevronLeft class="size-4" /> episode
    </a>
    <div class="flex gap-2">
      {#if ep > 1}
        <a href="/watch/{id}/{ep - 1}" use:link class="inline-flex items-center gap-1 rounded border border-zinc-300 px-3 py-1 text-sm dark:border-zinc-700">
          <ChevronLeft class="size-4" /> {ep - 1}
        </a>
      {/if}
      <a href="/watch/{id}/{ep + 1}" use:link class="inline-flex items-center gap-1 rounded border border-zinc-300 px-3 py-1 text-sm dark:border-zinc-700">
        {ep + 1} <ChevronRight class="size-4" />
      </a>
    </div>
  </div>

  <div class="flex gap-4">
    {#if cover}
      <img src={cover} alt="" class="w-24 rounded object-cover" />
    {/if}
    <div class="min-w-0">
      <h1 class="text-xl font-semibold">{anime ? detailTitle(anime) : `Episode ${ep}`}</h1>
      {#if anime?.titleRomaji && anime.titleRomaji !== detailTitle(anime)}
        <p class="text-sm text-zinc-500">{anime.titleRomaji} · Episode {ep}</p>
      {:else}
        <p class="text-sm text-zinc-500">Episode {ep}</p>
      {/if}
      {#if chips.length}
        <div class="mt-2 flex flex-wrap gap-1">
          {#each chips as c}
            <span class="rounded border border-zinc-300 px-2 py-0.5 text-xs text-zinc-600 dark:border-zinc-700 dark:text-zinc-400">{c}</span>
          {/each}
        </div>
      {/if}
      {#if anime?.description}
        <p class="mt-2 line-clamp-3 text-sm text-zinc-600 dark:text-zinc-400">{@html anime.description}</p>
      {/if}
    </div>
  </div>

  {#if $loading}
    <p class="flex items-center gap-2 text-sm text-zinc-500"><LoaderCircle class="size-4 animate-spin" /> memuat stream…</p>
  {:else if $error}
    <p class="flex items-center gap-2 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
      <TriangleAlert class="size-4" /> {$error}
    </p>
  {:else if $stream}
    <Player src={$stream.stream} sub={$stream.sub_en} subId={subId} nextAt={nextAt} autoSkip={autoSkip} skips={skips} busy={fetching && idActive} onDuration={(d: number) => { if (!dur) { dur = Math.round(d); trySkip(); } }} onEnded={() => { if (autoNext) { wantFs = !!document.fullscreenElement; location.hash = `/watch/${id}/${ep + 1}`; } }} onNeedMore={(t: number) => needMore(t)} onSeek={(t: number) => needMore(t, true)} onIdActive={startId} onIdInactive={stopId} />
    <div class="mt-3 flex items-center justify-between gap-2">
      <div class="flex gap-2">
      <button onclick={() => { autoSkip = !autoSkip; localStorage.setItem('zanime-autoskip', autoSkip ? '1' : '0'); }} class="inline-flex items-center gap-1 rounded border px-3 py-1 text-sm {autoSkip ? 'border-zinc-900 text-zinc-900 dark:border-zinc-100 dark:text-zinc-100' : 'border-zinc-300 text-zinc-500 dark:border-zinc-700'}" title="Auto skip OP/ED">
        <FastForward class="size-4" /> Auto skip
      </button>
      <button onclick={() => { autoNext = !autoNext; localStorage.setItem('zanime-autonext', autoNext ? '1' : '0'); }} class="inline-flex items-center gap-1 rounded border px-3 py-1 text-sm {autoNext ? 'border-zinc-900 text-zinc-900 dark:border-zinc-100 dark:text-zinc-100' : 'border-zinc-300 text-zinc-500 dark:border-zinc-700'}" title="Auto next episode">
        <ChevronRight class="size-4" /> Auto next
      </button>
      </div>
      <div class="flex justify-end">
      {#if subProg}
        <p class="flex items-center gap-2 text-sm text-zinc-500"><LoaderCircle class="size-4 animate-spin" /> {subProg}</p>
      {/if}
      </div>
    </div>
  {/if}
</div>

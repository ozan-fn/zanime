<script lang="ts">
  import { untrack } from 'svelte';
  import { link, push, router } from 'svelte-spa-router';
  import { ChevronLeft, ChevronRight, LoaderCircle, RotateCw, TriangleAlert } from '@lucide/svelte';
  import AnimeCard from '../features/search/components/AnimeCard.svelte';
  import { fetchCatalog } from '../features/home/api';
  import { sectionByKey, type SectionDef } from '../features/home/hooks';
  import type { Anime } from '../features/search/types';

  let { params = {} }: { params?: Record<string, string> } = $props();

  const PER_PAGE = 30; // batas keras upstream: limit > 30 → `items: null`
  const section = $derived(sectionByKey(params.key ?? ''));

  // Halaman aktif = `?page=N` di hash; navigasi lewat push() supaya tombol
  // back/forward dan refresh tetap di halaman yang sama.
  const page = $derived.by(() => {
    const p = Number(new URLSearchParams(router.querystring ?? '').get('page'));
    return Number.isFinite(p) && p >= 1 ? Math.floor(p) : 1;
  });

  let items = $state<Anime[]>([]);
  let loading = $state(false);
  let error = $state('');
  // Halaman terjauh yang kita tahu ada: halaman penuh → setidaknya ada halaman berikutnya.
  let last = $state(1);
  let top = $state<HTMLElement | null>(null);

  async function load(s: SectionDef, p: number) {
    loading = true;
    error = '';
    try {
      const r = await fetchCatalog({ ...s.params, limit: PER_PAGE, offset: (p - 1) * PER_PAGE });
      items = r;
      last = r.length < PER_PAGE ? Math.max(1, p) : Math.max(last, p + 1);
      top?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } catch (e) {
      items = [];
      error = e instanceof Error ? e.message : 'gagal memuat';
    }
    loading = false;
  }

  // Ganti section/halaman → muat. Bacaan state di-untrack supaya efek hanya ikut
  // `section`/`page` (bukan `items`/`last`) dan tidak memuat berulang.
  $effect(() => {
    const s = section;
    const p = page;
    untrack(() => {
      if (s) void load(s, p);
    });
  });

  function go(p: number) {
    if (!section || p < 1 || p > last || p === page) return;
    void push(`/browse/${section.key}?page=${p}`);
  }

  // Nomor halaman dengan elipsis; `last` tumbuh saat halaman baru ketemu penuh.
  const nums = $derived.by(() => {
    const t = last;
    const c = Math.min(page, t);
    const out: (number | '…')[] = [];
    if (t <= 7) {
      for (let i = 1; i <= t; i++) out.push(i);
      return out;
    }
    out.push(1);
    if (c > 3) out.push('…');
    for (let i = Math.max(2, c - 1); i <= Math.min(t - 1, c + 1); i++) out.push(i);
    if (c < t - 2) out.push('…');
    out.push(t);
    return out;
  });
</script>

<div class="mx-auto max-w-5xl space-y-4 p-4">
  <a href="/" use:link class="inline-flex items-center gap-1 text-sm text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">
    <ChevronLeft class="size-4" /> kembali
  </a>

  {#if !section}
    <p class="flex items-center gap-2 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
      <TriangleAlert class="size-4" /> kategori “{params.key}” tidak dikenal
    </p>
  {:else}
    <div bind:this={top} class="flex flex-wrap items-baseline justify-between gap-2">
      <h1 class="text-xl font-semibold">{section.title}</h1>
      <span class="text-xs text-zinc-500">
        halaman {Math.min(page, last)}{#if last > 1} dari {last}{/if}
      </span>
    </div>

    {#if loading && !items.length}
      <p class="flex items-center justify-center gap-2 py-10 text-sm text-zinc-500">
        <LoaderCircle class="size-4 animate-spin" /> memuat…
      </p>
    {:else if error}
      <div class="flex flex-col items-center gap-3 rounded border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
        <p class="flex items-center gap-2"><TriangleAlert class="size-4" /> {error}</p>
        <button
          onclick={() => section && load(section, page)}
          class="inline-flex items-center gap-1 rounded border border-current px-2 py-1 text-xs"
        >
          <RotateCw class="size-3.5" /> coba lagi
        </button>
      </div>
    {:else if !items.length}
      <p class="py-10 text-center text-sm text-zinc-500">belum ada judul di kategori ini.</p>
    {:else}
      {#if loading}
        <p class="flex items-center gap-2 text-xs text-zinc-500">
          <LoaderCircle class="size-3.5 animate-spin" /> memuat halaman {page}…
        </p>
      {/if}

      <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
        {#each items as a (a.id)}
          <AnimeCard anime={a} />
        {/each}
      </div>
    {/if}

    {#if last > 1}
      <nav class="flex flex-wrap items-center justify-between gap-2 pt-1">
        <button
          disabled={page <= 1}
          onclick={() => go(page - 1)}
          class="inline-flex items-center gap-1 rounded border border-zinc-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-zinc-700"
        >
          <ChevronLeft class="size-4" /> prev
        </button>
        <div class="flex flex-wrap items-center gap-1">
          {#each nums as n}
            {#if n === '…'}
              <span class="px-1 text-xs text-zinc-500">…</span>
            {:else}
              <button
                onclick={() => go(n)}
                class="rounded border px-2 py-0.5 text-xs {n === page
                  ? 'border-zinc-900 bg-zinc-900 text-white dark:border-zinc-100 dark:bg-zinc-100 dark:text-zinc-900'
                  : 'border-zinc-300 dark:border-zinc-700'}"
              >
                {n}
              </button>
            {/if}
          {/each}
        </div>
        <button
          disabled={page >= last}
          onclick={() => go(page + 1)}
          class="inline-flex items-center gap-1 rounded border border-zinc-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-zinc-700"
        >
          next <ChevronRight class="size-4" />
        </button>
      </nav>
      <p class="text-center text-xs text-zinc-500">
        judul {(page - 1) * PER_PAGE + 1}–{(page - 1) * PER_PAGE + items.length}
      </p>
    {/if}
  {/if}
</div>

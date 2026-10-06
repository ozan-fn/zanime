<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { ChevronLeft, ChevronRight, Play, Search } from '@lucide/svelte';
  import { abs } from '../../../lib/api';
  import type { Episode } from '../types';

  let { episodes, animeId }: { episodes: Episode[]; animeId: string } = $props();

  const PAGE_SIZE = 50;
  let q = $state('');
  let page = $state(1);
  let list = $state<HTMLElement | null>(null);

  // "episode 5"/"ep5" → "5"; nomor dicocokkan sebagai awalan, judul sebagai substring.
  const term = $derived(q.trim().toLowerCase().replace(/^ep(?:isode)?s?\s*/, ''));
  const filtered = $derived(
    term
      ? episodes.filter((e) => String(e.number).startsWith(term) || e.title.toLowerCase().includes(term))
      : episodes,
  );
  const totalPages = $derived(Math.max(1, Math.ceil(filtered.length / PAGE_SIZE)));
  const current = $derived(Math.min(page, totalPages));
  const shown = $derived(filtered.slice((current - 1) * PAGE_SIZE, current * PAGE_SIZE));
  const from = $derived(filtered.length ? (current - 1) * PAGE_SIZE + 1 : 0);
  const to = $derived(Math.min(current * PAGE_SIZE, filtered.length));

  const nums = $derived.by(() => {
    const t = totalPages;
    const c = current;
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

  // ganti query → kembali ke halaman 1
  $effect(() => {
    q;
    page = 1;
  });

  function go(p: number) {
    page = Math.min(Math.max(1, p), totalPages);
    list?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
</script>

<section class="space-y-3" bind:this={list}>
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h2 class="text-base font-semibold">
      Episode
      <span class="text-sm font-normal text-zinc-500">
        ({filtered.length}{term ? ` dari ${episodes.length}` : ''})
      </span>
    </h2>
    <label class="relative">
      <Search class="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-zinc-400" />
      <input
        bind:value={q}
        type="search"
        placeholder="cari nomor / judul…"
        class="w-44 rounded border border-zinc-300 bg-transparent py-1 pl-8 pr-2 text-sm sm:w-64 dark:border-zinc-700"
      />
    </label>
  </div>

  {#if filtered.length === 0}
    <p class="text-sm text-zinc-500">Tidak ada episode yang cocok.</p>
  {:else}
    <ol class="grid gap-2 sm:grid-cols-2">
      {#each shown as e (e.number)}
        <li>
          <a
            href="/watch/{animeId}/{e.number}"
            use:link
            class="flex items-center gap-3 rounded border border-zinc-200 bg-white p-2 hover:border-zinc-400 dark:border-zinc-800 dark:bg-zinc-900 dark:hover:border-zinc-600"
          >
            {#if e.img}
              <img src={abs(e.img)} alt="" loading="lazy" class="h-12 w-20 shrink-0 rounded object-cover" />
            {/if}
            <span class="flex size-8 shrink-0 items-center justify-center rounded bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900">
              <Play class="size-4" />
            </span>
            <span class="min-w-0">
              <span class="block text-sm font-medium">Episode {e.number}</span>
              <span class="block truncate text-xs text-zinc-500">{e.title}</span>
            </span>
          </a>
        </li>
      {/each}
    </ol>

    {#if totalPages > 1}
      <nav class="flex flex-wrap items-center justify-between gap-2 pt-1">
        <button
          disabled={current === 1}
          onclick={() => go(current - 1)}
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
                class="rounded border px-2 py-0.5 text-xs {n === current
                  ? 'border-zinc-900 bg-zinc-900 text-white dark:border-zinc-100 dark:bg-zinc-100 dark:text-zinc-900'
                  : 'border-zinc-300 dark:border-zinc-700'}"
              >
                {n}
              </button>
            {/if}
          {/each}
        </div>
        <button
          disabled={current === totalPages}
          onclick={() => go(current + 1)}
          class="inline-flex items-center gap-1 rounded border border-zinc-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-zinc-700"
        >
          next <ChevronRight class="size-4" />
        </button>
      </nav>
      <p class="text-center text-xs text-zinc-500">{from}–{to} dari {filtered.length} episode</p>
    {/if}
  {/if}
</section>

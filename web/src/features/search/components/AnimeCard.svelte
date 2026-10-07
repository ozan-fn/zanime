<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { abs } from '../../../lib/api';
  import { statusLabel, title, type Anime } from '../types';

  let { anime }: { anime: Anime } = $props();
  const status = $derived(statusLabel(anime.status));
</script>

<a
  href="/anime/{anime.id}"
  use:link
  class="group block overflow-hidden rounded border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900"
>
  {#if anime.cover}
    <img src={abs(anime.cover)} alt={title(anime)} loading="lazy" class="aspect-[3/4] w-full object-cover" />
  {:else}
    <div class="flex aspect-[3/4] w-full items-center justify-center bg-zinc-100 text-xs text-zinc-400 dark:bg-zinc-800">
      tanpa gambar
    </div>
  {/if}
  <div class="space-y-1 p-2">
    <p class="truncate text-sm font-medium">{title(anime)}</p>
    <p class="flex flex-wrap items-center gap-x-1 text-xs text-zinc-500">
      {#if anime.seasonYear}
        <span class="font-medium text-zinc-700 dark:text-zinc-300">{anime.seasonYear}</span>
      {/if}
      {#if anime.format}<span>{anime.format}</span>{/if}
      {#if anime.episodeCount}<span>· {anime.episodeCount} eps</span>{/if}
      {#if !anime.seasonYear && !anime.format && !anime.episodeCount}<span>–</span>{/if}
    </p>
    {#if anime.averageScore || status}
      <p class="flex items-center justify-between gap-1 text-xs">
        {#if anime.averageScore}
          <span class="text-amber-600 dark:text-amber-400">★ {(anime.averageScore / 10).toFixed(1)}</span>
        {:else}
          <span></span>
        {/if}
        {#if status}
          <span class="shrink-0 rounded bg-zinc-100 px-1.5 py-0.5 text-[10px] text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">
            {status}
          </span>
        {/if}
      </p>
    {/if}
  </div>
</a>

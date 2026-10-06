<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { abs } from '../../../lib/api';
  import { title, type Anime } from '../types';

  let { anime }: { anime: Anime } = $props();
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
  <div class="p-2">
    <p class="truncate text-sm font-medium">{title(anime)}</p>
    <p class="text-xs text-zinc-500">
      {#if anime.format}{anime.format}{/if}{#if anime.seasonYear} {anime.seasonYear}{/if}{#if anime.episodeCount} · {anime.episodeCount} eps{/if}{#if !anime.format && !anime.seasonYear && !anime.episodeCount}–{/if}
    </p>
    {#if anime.averageScore}
      <p class="text-xs text-amber-600 dark:text-amber-400">★ {(anime.averageScore / 10).toFixed(1)}</p>
    {/if}
  </div>
</a>

<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { abs } from '../../../lib/api';
  import { statusLabel } from '../../search/types';
  import type { Relation } from '../types';
  import { imgOf } from '../types';

  let { items }: { items: Relation[] } = $props();
  const img = (r: Relation) => abs(imgOf(r.coverImage) || imgOf(r.image));
</script>

{#snippet isi(r: Relation)}
  {#if img(r)}
    <img src={img(r)} alt={r.title} loading="lazy" class="aspect-[3/4] w-full object-cover" />
  {/if}
  <div class="space-y-1 p-2">
    <p class="truncate text-sm font-medium">{r.title}</p>
    <p class="flex flex-wrap items-center justify-between gap-1 text-xs text-zinc-500">
      <span class="flex flex-wrap items-center gap-x-1">
        {#if r.seasonYear}
          <span class="font-medium text-zinc-700 dark:text-zinc-300">{r.seasonYear}</span>
        {/if}
        {#if r.type}<span>{r.type}</span>{/if}
        {#if r.episodeCount}<span>· {r.episodeCount} eps</span>{/if}
        {#if !r.seasonYear && !r.type && !r.episodeCount}<span>–</span>{/if}
      </span>
      {#if r.status}
        <span class="shrink-0 rounded bg-zinc-100 px-1.5 py-0.5 text-[10px] text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">
          {statusLabel(r.status)}
        </span>
      {/if}
    </p>
  </div>
{/snippet}

{#if items.length}
  <section class="space-y-2">
    <h2 class="text-base font-semibold">Terkait</h2>
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
      {#each items as r (r.animeId || r.anilistId)}
        {@const kelas = 'overflow-hidden rounded border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900'}
        {#if r.animeId}
          <a href="/anime/{r.animeId}" use:link class={kelas}>
            {@render isi(r)}
          </a>
        {:else}
          <div class={kelas}>
            {@render isi(r)}
          </div>
        {/if}
      {/each}
    </div>
  </section>
{/if}

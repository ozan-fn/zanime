<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { ChevronLeft, LoaderCircle, TriangleAlert } from '@lucide/svelte';
  import { abs } from '../lib/api';
  import EpisodeList from '../features/episodes/components/EpisodeList.svelte';
  import RelatedList from '../features/episodes/components/RelatedList.svelte';
  import { detail, episodes, error, loadAnime, loading, relations } from '../features/episodes';
  import { altTitles, detailTitle, imgOf } from '../features/episodes/types';

  let { params = {} }: { params?: Record<string, string> } = $props();
  const id = $derived(params.id ?? '');
  $effect(() => {
    if (id) void loadAnime(id);
  });
  const banner = $derived($detail ? abs(imgOf($detail.backdropUrl) || imgOf($detail.bannerImage)) : '');
  const cover = $derived($detail ? abs(imgOf($detail.coverImage)) : '');
  const alts = $derived($detail ? altTitles($detail) : []);
</script>

<div class="mx-auto max-w-5xl space-y-4 p-4">
  <a href="/" use:link class="inline-flex items-center gap-1 text-sm text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">
    <ChevronLeft class="size-4" /> kembali
  </a>
  {#if $loading}
    <p class="flex items-center gap-2 text-sm text-zinc-500"><LoaderCircle class="size-4 animate-spin" /> memuat…</p>
  {:else if $error}
    <p class="flex items-center gap-2 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
      <TriangleAlert class="size-4" /> {$error}
    </p>
  {:else if $detail}
    {#if banner}
      <img src={banner} alt="" class="h-40 w-full rounded object-cover sm:h-56" />
    {/if}
    <div class="flex gap-4">
      {#if cover}
        <img src={cover} alt="" class="w-24 rounded object-cover" />
      {/if}
      <div class="min-w-0">
        <h1 class="text-xl font-semibold">{detailTitle($detail)}</h1>
        {#each alts as t (t)}
          <p class="text-sm text-zinc-500">{t}</p>
        {/each}
        {#if $detail.episodeCount}
          <p class="text-sm text-zinc-500">{$detail.episodeCount} episode</p>
        {/if}
        {#if $detail.description}
          <p class="mt-2 line-clamp-4 text-sm text-zinc-600 dark:text-zinc-400">{@html $detail.description}</p>
        {/if}
      </div>
    </div>
    <EpisodeList episodes={$episodes} animeId={id} />
    <RelatedList items={$relations} />
  {/if}
</div>

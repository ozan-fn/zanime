<script lang="ts">
  import { LoaderCircle, TriangleAlert } from '@lucide/svelte';
  import SearchBar from '../features/search/components/SearchBar.svelte';
  import AnimeCard from '../features/search/components/AnimeCard.svelte';
  import HomeSection from '../features/home/components/HomeSection.svelte';
  import { loadHome, loading as homeLoading, sections, error as homeError } from '../features/home';
  import { error, lastQuery, loading, results, runSearch } from '../features/search';

  $effect(() => {
    void loadHome();
  });
</script>

<div class="mx-auto max-w-5xl space-y-6 p-4">
  <SearchBar onSearch={runSearch} />

  {#if $loading}
    <p class="flex items-center gap-2 text-sm text-zinc-500"><LoaderCircle class="size-4 animate-spin" /> mencari…</p>
  {:else if $error}
    <p class="flex items-center gap-2 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
      <TriangleAlert class="size-4" /> {$error}
    </p>
  {:else if $results.length}
    <p class="text-sm text-zinc-500">hasil untuk “{$lastQuery}”</p>
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
      {#each $results as a (a.id)}
        <AnimeCard anime={a} />
      {/each}
    </div>
  {:else if $homeLoading}
    <p class="flex items-center gap-2 text-sm text-zinc-500"><LoaderCircle class="size-4 animate-spin" /> memuat katalog…</p>
  {:else if $homeError}
    <p class="flex items-center gap-2 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
      <TriangleAlert class="size-4" /> {$homeError}
    </p>
  {:else if $sections.length}
    {#each $sections as section (section.key)}
      <HomeSection {section} />
    {/each}
  {:else}
    <p class="text-sm text-zinc-500">Ketik judul lalu cari.</p>
  {/if}
</div>

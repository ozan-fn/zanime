<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { ChevronRight } from '@lucide/svelte';
  import AnimeCard from '../../search/components/AnimeCard.svelte';
  import type { HomeSection } from '../types';

  let { section }: { section: HomeSection } = $props();
</script>

<section class="space-y-2">
  <div class="flex items-center justify-between gap-2">
    <h2 class="text-base font-semibold">{section.title}</h2>
    <a
      href="/browse/{section.key}"
      use:link
      class="flex shrink-0 items-center gap-0.5 text-xs text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
    >
      lihat semua <ChevronRight class="size-3.5" />
    </a>
  </div>
  <ul class="flex gap-3 overflow-x-auto pb-2">
    {#each section.items as a (a.id)}
      <li class="w-32 shrink-0 sm:w-36">
        <AnimeCard anime={a} />
      </li>
    {/each}
  </ul>
</section>

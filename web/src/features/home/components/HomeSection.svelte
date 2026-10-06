<script lang="ts">
  import AnimeCard from '../../search/components/AnimeCard.svelte';
  import type { HomeSection } from '../types';

  let { section }: { section: HomeSection } = $props();
</script>

<section class="space-y-2">
  <h2 class="text-base font-semibold">{section.title}</h2>
  <ul class="flex gap-3 overflow-x-auto pb-2">
    {#each section.items as a (a.id)}
      <li class="w-32 shrink-0 sm:w-36">
        <AnimeCard anime={a} />
      </li>
    {/each}
  </ul>
</section>

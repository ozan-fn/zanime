<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { abs } from '../../../lib/api';
  import type { Relation } from '../types';
  import { imgOf } from '../types';

  let { items }: { items: Relation[] } = $props();
  const img = (r: Relation) => abs(imgOf(r.coverImage) || imgOf(r.image));
</script>

{#if items.length}
  <section class="space-y-2">
    <h2 class="text-base font-semibold">Terkait</h2>
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
      {#each items as r (r.animeId)}
        <a
          href="/anime/{r.animeId}"
          use:link
          class="overflow-hidden rounded border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900"
        >
          {#if img(r)}
            <img src={img(r)} alt={r.title} loading="lazy" class="aspect-[3/4] w-full object-cover" />
          {/if}
          <div class="p-2">
            <p class="truncate text-sm font-medium">{r.title}</p>
            <p class="text-xs text-zinc-500">
              {#if r.type}{r.type} · {/if}{#if r.episodeCount}{r.episodeCount} eps{:else}–{/if}
            </p>
          </div>
        </a>
      {/each}
    </div>
  </section>
{/if}

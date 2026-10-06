<script lang="ts">
  import Router, { link } from 'svelte-spa-router';
  import { Clapperboard, Moon, Sun } from '@lucide/svelte';
  import { dark } from './lib/theme';
  import { routes } from './router';
</script>

<header class="border-b border-zinc-200 dark:border-zinc-800">
  <div class="mx-auto flex max-w-5xl items-center justify-between p-4">
    <a href="/" use:link class="flex items-center gap-2 font-semibold">
      <Clapperboard class="size-5" /> Nonton
    </a>
    <button
      onclick={() => dark.update((d) => !d)}
      aria-label="tema"
      class="rounded border border-zinc-300 p-2 dark:border-zinc-700"
    >
      {#if $dark}
        <Sun class="size-4" />
      {:else}
        <Moon class="size-4" />
      {/if}
    </button>
  </div>
</header>

<main>
  <Router {routes} />
</main>

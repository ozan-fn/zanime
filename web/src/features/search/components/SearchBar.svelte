<script lang="ts">
  import { Search } from '@lucide/svelte';

  let { onSearch }: { onSearch: (q: string) => void } = $props();
  let value = $state('');
</script>

<form
  class="flex gap-2"
  onsubmit={(e) => {
    e.preventDefault();
    onSearch(value);
  }}
>
  <div class="relative flex-1">
    <Search class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-zinc-500" />
    <input
      bind:value
      type="search"
      placeholder="Cari anime…"
      class="w-full rounded border border-zinc-300 bg-white py-2 pr-3 pl-9 text-sm outline-none placeholder:text-zinc-400 focus:border-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:focus:border-zinc-400"
    />
  </div>
  <button type="submit" class="rounded bg-zinc-900 px-4 py-2 text-sm font-medium text-white dark:bg-zinc-100 dark:text-zinc-900">
    Cari
  </button>
</form>

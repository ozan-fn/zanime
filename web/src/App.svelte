<script lang="ts">
  import Router, { link } from 'svelte-spa-router';
  import { Clapperboard, Eye, Moon, Sun } from '@lucide/svelte';
  import { get } from './lib/api';
  import { dark } from './lib/theme';
  import { routes } from './router';

  interface Stats {
    memMb: number;
    peakMemMb: number;
    cpu: number;
    peakCpu: number;
  }

  let stats = $state<Stats | null>(null);
  /** Penonton aktif (heartbeat dari halaman watch, TTL 45 dtk). */
  let watching = $state(0);

  $effect(() => {
    let alive = true;
    const ambil = () =>
      get<{ watching: number }>('/api/presence')
        .then((p) => {
          if (alive) watching = p.watching;
        })
        .catch(() => {});
    void ambil();
    const iv = setInterval(ambil, 15000);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  });

  /** Resource proses zanime; backend memperbarui angkanya tiap 2 dtk. */
  $effect(() => {
    let alive = true;
    const ambil = () =>
      get<Stats>('/api/stats')
        .then((s) => {
          if (alive) stats = s;
        })
        .catch(() => {});
    void ambil();
    const iv = setInterval(ambil, 3000);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  });
</script>

<header class="border-b border-zinc-200 dark:border-zinc-800">
  <div class="mx-auto flex max-w-5xl items-center justify-between p-4">
    <a href="/" use:link class="flex items-center gap-2 font-semibold">
      <Clapperboard class="size-5" /> Nonton
    </a>
    <div class="flex items-center gap-3">
      {#if watching > 0}
        <span
          class="flex items-center gap-1 rounded border border-zinc-300 px-2 py-1 text-xs text-zinc-600 dark:border-zinc-700 dark:text-zinc-400"
          title="Jumlah penonton aktif saat ini"
        >
          <Eye class="size-3.5" />
          {watching} nonton
        </span>
      {/if}
      {#if stats}
        <div
          class="hidden items-center gap-2 font-mono text-[11px] text-zinc-500 md:flex"
          title="Pemakaian resource proses zanime (memori & CPU, kini + puncak)"
        >
          <span>mem {stats.memMb} MB</span>
          <span>peak {stats.peakMemMb} MB</span>
          <span>cpu {stats.cpu.toFixed(1)}%</span>
          <span>peak {stats.peakCpu.toFixed(1)}%</span>
        </div>
      {/if}
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
  </div>
</header>

<main>
  <Router {routes} />
</main>

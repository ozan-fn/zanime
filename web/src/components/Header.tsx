import { route } from 'preact-router';
import { Icon } from './ui/Icon';

// Header: logo + form pencarian. Submit melakukan navigasi ke /s/{q} — URL
// adalah sumber kebenaran, jadi back/refresh tetap benar.
export function Header() {
  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    const form = e.currentTarget as HTMLFormElement;
    const q = (form.elements.namedItem('q') as HTMLInputElement).value.trim();
    route(q ? `/s/${encodeURIComponent(q)}` : '/');
  };

  return (
    <header class="sticky top-0 z-20 border-b border-white/5 bg-background/70 backdrop-blur-xl">
      <div class="mx-auto flex max-w-7xl items-center gap-4 px-4 py-3">
        <a
          href="/"
          aria-label="zanime, beranda"
          class="shrink-0 rounded-lg transition-transform hover:scale-105 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
        >
          <svg viewBox="0 0 32 32" class="size-9" aria-hidden="true">
            <rect width="32" height="32" rx="7" fill="#fff" />
            <path d="M12 9v14l11-7z" fill="#000" />
          </svg>
        </a>
        <form class="relative ml-auto w-full max-w-md" role="search" onSubmit={submit}>
          <label for="q" class="sr-only">Judul anime</label>
          <Icon
            name="search"
            class="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-subtle"
          />
          <input
            id="q"
            name="q"
            type="search"
            autocomplete="off"
            enterkeyhint="search"
            placeholder="Cari judul anime"
            class="h-11 w-full rounded-lg border border-border bg-card pl-10 pr-4 text-base text-foreground placeholder:text-subtle transition-colors hover:border-border-strong focus-visible:border-foreground focus-visible:outline-none sm:h-10 sm:text-sm"
          />
        </form>
      </div>
    </header>
  );
}

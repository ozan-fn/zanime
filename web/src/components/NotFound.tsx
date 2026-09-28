export function NotFound() {
  return (
    <div class="flex flex-col items-center gap-3 py-24 text-center">
      <p class="text-4xl font-semibold">404</p>
      <p class="text-sm text-subtle">Halaman tidak ditemukan.</p>
      <a
        href="/"
        class="inline-flex h-10 items-center rounded-lg border border-border bg-card px-3 text-sm hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
      >
        Kembali ke beranda
      </a>
    </div>
  );
}

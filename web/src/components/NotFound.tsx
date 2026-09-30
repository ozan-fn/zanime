import { Link } from 'react-router';

export function NotFound() {
  return (
    <div className="flex flex-col items-center gap-3 py-24 text-center">
      <p className="text-4xl font-semibold">404</p>
      <p className="text-sm text-subtle">Halaman tidak ditemukan.</p>
      <Link
        to="/"
        className="inline-flex h-10 items-center rounded-lg border border-border bg-card px-3 text-sm hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
      >
        Kembali ke beranda
      </Link>
    </div>
  );
}

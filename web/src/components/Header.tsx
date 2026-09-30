import { useEffect, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { Icon } from './ui/Icon';
import { safeDecode } from '../lib/urls';

// Header: logo + form pencarian. Submit navigasi ke /s/{q} lewat useNavigate —
// bukan submit form bawaan browser, yang dulu memuat ulang halaman di URL yang
// sama sehingga halaman tonton tetap terlihat. URL tetap sumber kebenaran, jadi
// back/refresh benar. Input ikut URL: tanpa ini, kembali ke hasil pencarian
// (atau membuka tautan /s/... langsung) menyisakan kolom kosong padahal
// halaman menampilkan hasilnya.
export function Header() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const fromUrl = pathname.startsWith('/s/') ? safeDecode(pathname.slice(3)) : '';
  const [value, setValue] = useState(fromUrl);

  useEffect(() => setValue(fromUrl), [fromUrl]);

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const q = value.trim();
    navigate(q ? `/s/${encodeURIComponent(q)}` : '/');
  };

  return (
    <header className="sticky top-0 z-20 border-b border-white/5 bg-background/70 backdrop-blur-xl">
      <div className="mx-auto flex max-w-7xl items-center gap-4 px-4 py-3">
        <Link
          to="/"
          aria-label="zanime, beranda"
          className="shrink-0 rounded-lg transition-transform hover:scale-105 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
        >
          <svg viewBox="0 0 32 32" className="size-9" aria-hidden="true">
            <rect width="32" height="32" rx="7" fill="#fff" />
            <path d="M12 9v14l11-7z" fill="#000" />
          </svg>
        </Link>
        <form className="relative ml-auto w-full max-w-md" role="search" onSubmit={submit}>
          <label htmlFor="q" className="sr-only">
            Judul anime
          </label>
          <Icon
            name="search"
            className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-subtle"
          />
          <input
            id="q"
            name="q"
            type="search"
            autoComplete="off"
            enterKeyHint="search"
            placeholder="Cari judul anime"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            className="h-11 w-full rounded-lg border border-border bg-card pl-10 pr-4 text-base text-foreground placeholder:text-subtle transition-colors hover:border-border-strong focus-visible:border-foreground focus-visible:outline-none sm:h-10 sm:text-sm"
          />
        </form>
      </div>
    </header>
  );
}

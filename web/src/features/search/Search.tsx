import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router';
import { searchAnime } from './api';
import { animeHref, safeDecode } from '../../lib/urls';
import { rememberTitles } from '../watch/catalog';
import type { Anime } from '../../lib/types';

// Pencarian: URL /s/{q} sumber kebenaran. Hasil mengisi katalog judul agar
// halaman tonton punya nama sebelum request episodenya pulang.
export function Search() {
  const { q } = useParams();
  const query = q ? safeDecode(q).trim() : '';
  const [list, setList] = useState<Anime[] | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    document.title = query ? `${query} · zanime` : 'zanime';
    setList(null);
    setError('');
    if (!query) return;

    let alive = true;
    searchAnime(query)
      .then((r) => {
        if (!alive) return;
        rememberTitles(r);
        setList(r);
      })
      .catch((e: Error) => alive && setError(e.message));
    return () => {
      alive = false;
    };
  }, [query]);

  if (!query) {
    return (
      <div className="flex flex-col items-center gap-4 py-24 text-center">
        <svg viewBox="0 0 32 32" className="size-14" aria-hidden="true">
          <rect width="32" height="32" rx="4.6" fill="#fff" />
          <path d="M12 9v14l11-7z" fill="#000" />
        </svg>
        <p className="text-sm text-subtle">Ketik judul anime di kolom pencarian, lalu pilih episodenya.</p>
      </div>
    );
  }

  if (error) return <p className="text-sm text-destructive">Pencarian gagal: {error}.</p>;

  if (!list) {
    return (
      <div className="mt-4 grid grid-cols-2 gap-x-3 gap-y-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {Array.from({ length: 12 }, (_, i) => (
          <div key={i} className="animate-pulse">
            <div className="aspect-[2/3] rounded-lg bg-card" />
            <div className="mt-2 h-3 w-3/4 rounded bg-card" />
          </div>
        ))}
      </div>
    );
  }

  if (!list.length)
    return (
      <p className="text-sm text-subtle">
        Tidak ada hasil untuk “{query}”. Coba kata kunci lain atau judul aslinya.
      </p>
    );

  return (
    <>
      <p className="text-sm text-subtle" aria-live="polite">
        {list.length} judul ditemukan.
      </p>
      <div className="mt-4 grid grid-cols-2 gap-x-3 gap-y-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {list.map((a) => (
          <Link
            key={a.id}
            to={animeHref(a.id)}
            title={a.synopsis}
            className="group block rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
          >
            <span className="relative block aspect-[2/3] overflow-hidden rounded-lg border border-border bg-card transition-colors group-hover:border-border-strong">
              {a.poster && (
                <img
                  src={a.poster}
                  alt={`Poster ${a.name}`}
                  loading="lazy"
                  className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                />
              )}
            </span>
            <span className="mt-2 line-clamp-2 block text-sm font-medium leading-snug">{a.name}</span>
          </Link>
        ))}
      </div>
    </>
  );
}

import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router';
import { animeDetails, animeEpisodes, type AnimeDetails } from './api';
import { knownTitle } from './catalog';
import { animeHref, slugTitle, watchHref } from '../../lib/urls';
import type { Episode } from '../../lib/types';
import { Icon } from '../../components/ui/Icon';

// Katalog: /a/{id}. Judul dari cache pencarian dipakai sebelum detail pulang,
// jadi heading tidak melompat.
export function Anime() {
  const { animeId = '' } = useParams();
  const [detail, setDetail] = useState<AnimeDetails | null>(null);
  const [episodes, setEpisodes] = useState<Episode[] | null>(null);
  const [error, setError] = useState('');
  const title = knownTitle(animeId) || slugTitle(animeId);

  useEffect(() => {
    if (!animeId) return;
    document.title = `${title} · zanime`;
    let alive = true;
    animeDetails(animeId)
      .then((d) => alive && setDetail(d))
      .catch((e: Error) => alive && setError(e.message));
    animeEpisodes(animeId)
      .then((e) => alive && setEpisodes(e))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [animeId, title]);

  if (!animeId || error) {
    return (
      <p className="text-sm text-destructive">
        {error ? `Katalog gagal dimuat: ${error}.` : 'Anime tidak ditemukan.'}
      </p>
    );
  }

  const meta = detail
    ? [detail.status, detail.duration, detail.aired, detail.score && `Skor ${detail.score}`, detail.studios]
        .filter(Boolean)
        .join(' · ')
    : '';

  return (
    <>
      <Link
        to="/"
        className="inline-flex h-10 w-fit items-center gap-2 rounded-lg border border-border bg-card px-3 text-sm hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
      >
        <Icon name="arrow-left" className="size-4" /> Cari judul lain
      </Link>
      <h1 className="mt-3 text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>

      <div className="mt-6 flex flex-col gap-5 sm:flex-row">
        {detail?.poster && (
          <img
            src={detail.poster}
            alt={`Poster ${title}`}
            loading="lazy"
            className="w-44 shrink-0 self-start rounded-lg border border-border object-cover"
          />
        )}
        <div className="min-w-0 flex-1">
          <h2 className="text-lg font-semibold tracking-tight">
            {detail ? (detail.japanese ? `${detail.name} (${detail.japanese})` : detail.name) : title}
          </h2>
          {meta && <p className="mt-1 text-sm text-subtle">{meta}</p>}
          {detail?.genres?.length ? (
            <div className="mt-3 flex flex-wrap gap-1.5">
              {detail.genres.map((g) => (
                <span key={g} className="rounded-lg border border-border bg-card px-2.5 py-0.5 text-xs text-subtle">
                  {g}
                </span>
              ))}
            </div>
          ) : null}
          {detail?.synopsis && <p className="mt-4 text-sm leading-relaxed text-subtle">{detail.synopsis}</p>}
        </div>
      </div>

      <section className="mt-8">
        <h2 className="text-sm font-medium text-subtle">Episode</h2>
        {!episodes && (
          <div className="mt-2 grid grid-cols-3 gap-2 sm:grid-cols-4 md:grid-cols-5">
            {Array.from({ length: 12 }, (_, i) => (
              <div key={i} className="h-9 animate-pulse rounded-lg bg-raised" />
            ))}
          </div>
        )}
        {episodes && !episodes.length && (
          <p className="mt-2 text-sm text-subtle">Judul ini belum punya episode yang bisa diputar.</p>
        )}
        {episodes?.length ? (
          <div className="mt-2 grid max-h-96 grid-cols-3 gap-2 overflow-y-auto rounded-lg border border-border bg-card p-2 sm:grid-cols-4 md:grid-cols-5">
            {episodes.map((e) => (
              <Link
                key={e.id}
                to={watchHref(animeId, e.id)}
                className="rounded-lg px-2 py-1.5 text-center text-sm text-subtle hover:bg-raised hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
              >
                {e.number}
              </Link>
            ))}
          </div>
        ) : null}
      </section>

      {detail?.related?.length ? (
        <section className="mt-8">
          <h3 className="text-sm font-medium text-subtle">Anime terkait</h3>
          <div className="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {detail.related.map((r) => (
              <Link
                key={r.id}
                to={animeHref(r.id)}
                className="rounded-lg border border-border bg-card p-3 text-xs transition-colors hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
              >
                {r.name}
              </Link>
            ))}
          </div>
        </section>
      ) : null}

      {detail?.recommended?.length ? (
        <section className="mt-8">
          <h3 className="text-sm font-medium text-subtle">Rekomendasi</h3>
          <div className="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {detail.recommended.map((r) => (
              <Link
                key={r.id}
                to={animeHref(r.id)}
                className="rounded-lg border border-border bg-card p-3 text-xs transition-colors hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
              >
                {r.name}
              </Link>
            ))}
          </div>
        </section>
      ) : null}
    </>
  );
}

import { useEffect, useState } from 'preact/hooks';
import { animeDetails, animeEpisodes, type AnimeDetails } from './api';
import { knownTitle } from './catalog';
import { slugTitle, watchHref } from '../../lib/urls';
import type { Episode } from '../../lib/types';
import { Icon } from '../../components/ui/Icon';

// Katalog: /a/{id}. Judul dari cache pencarian dipakai sebelum detail pulang,
// jadi heading tidak melompat.
export function Anime({ animeId }: { animeId?: string }) {
  const [detail, setDetail] = useState<AnimeDetails | null>(null);
  const [episodes, setEpisodes] = useState<Episode[] | null>(null);
  const [error, setError] = useState('');
  const title = (animeId && knownTitle(animeId)) || slugTitle(animeId || '');

  useEffect(() => {
    if (!animeId) return;
    document.title = `${title} · zanime`;
    let alive = true;
    animeDetails(animeId).then((d) => alive && setDetail(d)).catch((e) => alive && setError(e.message));
    animeEpisodes(animeId).then((e) => alive && setEpisodes(e)).catch(() => {});
    return () => {
      alive = false;
    };
  }, [animeId]);

  if (!animeId || error) {
    return (
      <p class="text-sm text-destructive">
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
      <a
        href="/"
        class="inline-flex h-10 w-fit items-center gap-2 rounded-lg border border-border bg-card px-3 text-sm hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
      >
        <Icon name="arrow-left" class="size-4" /> Cari judul lain
      </a>
      <h1 class="mt-3 text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>

      <div class="mt-6 flex flex-col gap-5 sm:flex-row">
        {detail?.poster && (
          <img
            src={detail.poster}
            alt={`Poster ${title}`}
            loading="lazy"
            class="w-44 shrink-0 self-start rounded-lg border border-border object-cover"
          />
        )}
        <div class="min-w-0 flex-1">
          <h2 class="text-lg font-semibold tracking-tight">
            {detail ? (detail.japanese ? `${detail.name} (${detail.japanese})` : detail.name) : title}
          </h2>
          {meta && <p class="mt-1 text-sm text-subtle">{meta}</p>}
          {detail?.genres?.length ? (
            <div class="mt-3 flex flex-wrap gap-1.5">
              {detail.genres.map((g) => (
                <span key={g} class="rounded-lg border border-border bg-card px-2.5 py-0.5 text-xs text-subtle">
                  {g}
                </span>
              ))}
            </div>
          ) : null}
          {detail?.synopsis && <p class="mt-4 text-sm leading-relaxed text-subtle">{detail.synopsis}</p>}
        </div>
      </div>

      <section class="mt-8">
        <h2 class="text-sm font-medium text-subtle">Episode</h2>
        {!episodes && (
          <div class="mt-2 grid grid-cols-3 gap-2 sm:grid-cols-4 md:grid-cols-5">
            {Array.from({ length: 12 }, (_, i) => (
              <div key={i} class="h-9 animate-pulse rounded-lg bg-raised" />
            ))}
          </div>
        )}
        {episodes && !episodes.length && (
          <p class="mt-2 text-sm text-subtle">Judul ini belum punya episode yang bisa diputar.</p>
        )}
        {episodes?.length ? (
          <div class="mt-2 grid max-h-96 grid-cols-3 gap-2 overflow-y-auto rounded-lg border border-border bg-card p-2 sm:grid-cols-4 md:grid-cols-5">
            {episodes.map((e) => (
              <a
                key={e.id}
                href={watchHref(animeId, e.id, 'sub')}
                class="rounded-lg px-2 py-1.5 text-center text-sm text-subtle hover:bg-raised hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
              >
                {e.number}
              </a>
            ))}
          </div>
        ) : null}
      </section>

      {detail?.related?.length ? (
        <section class="mt-8">
          <h3 class="text-sm font-medium text-subtle">Anime terkait</h3>
          <div class="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {detail.related.map((r) => (
              <a
                key={r.id}
                href={`/a/${encodeURIComponent(r.id)}`}
                class="rounded-lg border border-border bg-card p-3 text-xs transition-colors hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
              >
                {r.name}
              </a>
            ))}
          </div>
        </section>
      ) : null}
    </>
  );
}

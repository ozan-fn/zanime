import { useEffect, useState } from 'preact/hooks';
import { Player } from '../../components/Player';
import { NotFound } from '../../components/NotFound';
import { Icon } from '../../components/ui/Icon';
import { animeDetails, animeEpisodes, type AnimeDetails } from './api';
import { knownTitle } from './catalog';
import { masterUrl, subtitleFileUrl, subtitleStatus } from './player';
import { normMode, slugTitle, watchHref } from '../../lib/urls';
import type { Episode, SubtitleStatus } from '../../lib/types';

// Tonton: URL /w/{animeId}/{epId}?mode=dub adalah sumber kebenaran — back,
// refresh, dan tautan langsung benar tanpa state halaman. Status subtitle
// dipolling di sini; player hanya menerima statusnya sebagai prop, jadi
// player tidak tahu ada penerjemahan yang berjalan.
export function Watch({
  animeId,
  epId,
  mode: rawMode,
}: {
  animeId?: string;
  epId?: string;
  mode?: string;
}) {
  const mode = normMode(rawMode);
  const title = knownTitle(animeId || '') || slugTitle(animeId || '');
  const [episodes, setEpisodes] = useState<Episode[] | null>(null);
  const [detail, setDetail] = useState<AnimeDetails | null>(null);
  const [error, setError] = useState('');
  const [status, setStatus] = useState<SubtitleStatus | null>(null);

  useEffect(() => {
    document.title = epId ? `${title} · episode · zanime` : `${title} · zanime`;
  }, [title, epId]);

  useEffect(() => {
    if (!animeId) return;
    let alive = true;
    setError('');
    setEpisodes(null);
    animeEpisodes(animeId)
      .then((list) => alive && setEpisodes(list))
      .catch((e) => alive && setError(e.message));
    animeDetails(animeId).then((d) => alive && setDetail(d)).catch(() => {});
    return () => {
      alive = false;
    };
  }, [animeId]);

  // Polling sengaja lengket: satu jawaban gagal tidak mengakhiri polling
  // karena pekerjaan terjemahan tetap jalan di server.
  useEffect(() => {
    if (!epId) return;
    let alive = true;
    let timer: number | undefined;
    const poll = () => {
      subtitleStatus(epId, mode)
        .then((s) => {
          if (!alive) return;
          setStatus(s);
          if (s.state === 'converting') timer = window.setTimeout(poll, 2000);
        })
        .catch(() => {
          if (alive) timer = window.setTimeout(poll, 4000);
        });
    };
    poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [epId, mode]);

  if (!animeId) return <NotFound />;

  if (error) return <p class="text-sm text-destructive">Daftar episode gagal dimuat: {error}.</p>;

  const eps = episodes || [];
  const idx = eps.findIndex((e) => e.id === epId);
  const prev = idx > 0 ? eps[idx - 1] : null;
  const next = idx >= 0 && idx < eps.length - 1 ? eps[idx + 1] : null;
  const meta = detail
    ? [detail.status, detail.duration, detail.aired, detail.score && `Skor ${detail.score}`, detail.studios]
        .filter(Boolean)
        .join(' · ')
    : '';

  return (
    <>
      <a
        href={`/a/${encodeURIComponent(animeId)}`}
        class="inline-flex h-10 w-fit items-center gap-2 rounded-lg border border-border bg-card px-3 text-sm hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
      >
        <Icon name="arrow-left" class="size-4" /> Semua episode
      </a>
      <h1 class="mt-3 text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>

      {epId && eps.length && idx >= 0 ? (
        <section class="mt-4">
          <Player
            src={masterUrl(epId, mode)}
            subtitle={subtitleFileUrl(epId, mode)}
            status={status}
          />
          <div class="mt-3 flex items-center gap-2">
            {prev ? (
              <a
                href={watchHref(animeId, prev.id, mode)}
                class="rounded-lg border border-border bg-card px-3 py-2 text-sm hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
              >
                <Icon name="chevron-left" class="mr-1 inline size-3.5" /> Ep {prev.number}
              </a>
            ) : null}
            {next ? (
              <a
                href={watchHref(animeId, next.id, mode)}
                class="rounded-lg border border-border bg-card px-3 py-2 text-sm hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
              >
                Ep {next.number} <Icon name="chevron-right" class="ml-1 inline size-3.5" />
              </a>
            ) : null}
            {status?.state === 'converting' && (
              <p class="ml-auto text-sm text-subtle" aria-live="polite">
                Menerjemahkan subtitle… {status.done}/{status.total}
              </p>
            )}
          </div>
        </section>
      ) : epId ? (
        <p class="mt-4 text-sm text-destructive">Episode tidak ditemukan. Pilih dari daftar.</p>
      ) : null}

      {detail ? (
        <section class="mt-6 flex flex-col gap-4 sm:flex-row">
          {detail.poster && (
            <img
              src={detail.poster}
              alt={`Poster ${title}`}
              loading="lazy"
              class="w-32 shrink-0 self-start rounded-lg border border-border object-cover"
            />
          )}
          <div class="min-w-0 flex-1">
            {meta && <p class="text-sm text-subtle">{meta}</p>}
            {detail.genres?.length ? (
              <div class="mt-2 flex flex-wrap gap-1.5">
                {detail.genres.map((g) => (
                  <span key={g} class="rounded-lg border border-border bg-card px-2.5 py-0.5 text-xs text-subtle">
                    {g}
                  </span>
                ))}
              </div>
            ) : null}
            {detail.synopsis && (
              <p class="mt-3 text-sm leading-relaxed text-subtle">{detail.synopsis}</p>
            )}
          </div>
        </section>
      ) : null}

      <section class="mt-6">
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
        {eps.length ? (
          <div class="mt-2 grid max-h-96 grid-cols-3 gap-2 overflow-y-auto rounded-lg border border-border bg-card p-2 sm:grid-cols-4 md:grid-cols-5">
            {eps.map((e) => (
              <a
                key={e.id}
                href={watchHref(animeId, e.id, mode)}
                aria-current={e.id === epId ? 'true' : 'false'}
                class={`rounded-lg px-2 py-1.5 text-center text-sm ${
                  e.id === epId
                    ? 'bg-foreground font-semibold text-background'
                    : 'text-subtle hover:bg-raised hover:text-foreground'
                }`}
              >
                {e.number}
              </a>
            ))}
          </div>
        ) : null}
      </section>
    </>
  );
}

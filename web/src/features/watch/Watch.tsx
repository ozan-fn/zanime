import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { Player } from '../../components/Player';
import { NotFound } from '../../components/NotFound';
import { Icon } from '../../components/ui/Icon';
import { animeDetails, animeEpisodes, type AnimeDetails } from './api';
import { knownTitle } from './catalog';
import { masterUrl, subtitleFileUrl, subtitleStatus } from './player';
import { slugTitle, watchHref } from '../../lib/urls';
import type { Episode, SubtitleStatus } from '../../lib/types';

// Tonton: URL /w/{animeId}/{epId} adalah sumber kebenaran — back, refresh, dan
// tautan langsung benar tanpa state halaman. Status subtitle dipolling di sini;
// player hanya menerima statusnya sebagai prop, jadi player tidak tahu ada
// penerjemahan yang berjalan. Audio selalu sub, lihat progress.md.
export function Watch() {
  const { animeId = '', epId } = useParams();
  const title = knownTitle(animeId) || slugTitle(animeId);
  const [episodes, setEpisodes] = useState<Episode[] | null>(null);
  const [detail, setDetail] = useState<AnimeDetails | null>(null);
  const [error, setError] = useState('');
  const [status, setStatus] = useState<SubtitleStatus | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const num = episodes?.find((e) => e.id === epId)?.number;
    document.title = num ? `${title} · episode ${num} · zanime` : `${title} · zanime`;
  }, [title, epId, episodes]);

  useEffect(() => {
    if (!animeId) return;
    let alive = true;
    setError('');
    setEpisodes(null);
    animeEpisodes(animeId)
      .then((list) => alive && setEpisodes(list))
      .catch((e: Error) => alive && setError(e.message));
    animeDetails(animeId)
      .then((d) => alive && setDetail(d))
      .catch(() => {});
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
    // Status episode sebelumnya tidak boleh tertinggal di layar.
    setStatus(null);
    const poll = () => {
      subtitleStatus(epId)
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
  }, [epId]);

  // Seri panjang (One Piece, ratusan episode) membuka daftar di episode 1:
  // episode yang sedang diputar harus digulirkan ke dalam pandangan, kalau
  // tidak pengguna tidak tahu di mana posisinya. Digulir manual lewat
  // grid.scrollTop, bukan scrollIntoView: yang terakhir ikut menggulir jendela
  // ke bawah sampai daftarnya terlihat, dan player yang baru dibuka jadi
  // terlempar keluar layar.
  useEffect(() => {
    const grid = gridRef.current;
    if (!epId || !grid) return;
    const cell = grid.querySelector(`[data-ep="${CSS.escape(epId)}"]`);
    if (!cell) return;
    const g = grid.getBoundingClientRect();
    const c = cell.getBoundingClientRect();
    if (c.top < g.top || c.bottom > g.bottom) {
      grid.scrollTop += c.top - g.top - (grid.clientHeight - c.height) / 2;
    }
  }, [epId, episodes]);

  if (!animeId) return <NotFound />;

  if (error) return <p className="text-sm text-destructive">Daftar episode gagal dimuat: {error}.</p>;

  const eps = episodes || [];
  const loaded = episodes !== null;
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
      <Link
        to={`/a/${encodeURIComponent(animeId)}`}
        className="inline-flex h-10 w-fit items-center gap-2 rounded-lg border border-border bg-card px-3 text-sm hover:border-border-strong hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
      >
        <Icon name="arrow-left" className="size-4" /> Semua episode
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>
      </div>

      {epId ? (
        idx >= 0 ? (
          <section className="mt-4">
            <Player src={masterUrl(epId)} subtitle={subtitleFileUrl(epId)} status={status} />
            <div className="mt-3 flex flex-wrap items-center gap-2">
              {prev ? (
                <Link
                  to={watchHref(animeId, prev.id)}
                  className="rounded-lg border border-border bg-card px-3 py-2 text-sm hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
                >
                  <Icon name="chevron-left" className="mr-1 inline size-3.5" /> Ep {prev.number}
                </Link>
              ) : null}
              {next ? (
                <Link
                  to={watchHref(animeId, next.id)}
                  className="rounded-lg border border-border bg-card px-3 py-2 text-sm hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
                >
                  Ep {next.number} <Icon name="chevron-right" className="ml-1 inline size-3.5" />
                </Link>
              ) : null}
              {status?.state === 'converting' && (
                <p className="ml-auto text-sm text-subtle" aria-live="polite">
                  Menerjemahkan subtitle… {status.done}/{status.total}
                </p>
              )}
              {status?.state === 'error' && (
                <p className="ml-auto text-sm text-destructive" aria-live="polite">
                  Subtitle Indonesia gagal diterjemahkan.
                </p>
              )}
            </div>
          </section>
        ) : loaded ? (
          <p className="mt-4 text-sm text-destructive">Episode tidak ditemukan. Pilih dari daftar.</p>
        ) : (
          // Placeholder setinggi player: daftar episode masih dimuat, dan
          // sebelumnya kondisi ini sudah mencetak "Episode tidak ditemukan".
          <div className="mt-4 aspect-video w-full animate-pulse rounded-lg border border-border bg-card" />
        )
      ) : null}

      {detail ? (
        <section className="mt-6 flex flex-col gap-4 sm:flex-row">
          {detail.poster && (
            <img
              src={detail.poster}
              alt={`Poster ${title}`}
              loading="lazy"
              className="w-32 shrink-0 self-start rounded-lg border border-border object-cover"
            />
          )}
          <div className="min-w-0 flex-1">
            {meta && <p className="text-sm text-subtle">{meta}</p>}
            {detail.genres?.length ? (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {detail.genres.map((g) => (
                  <span key={g} className="rounded-lg border border-border bg-card px-2.5 py-0.5 text-xs text-subtle">
                    {g}
                  </span>
                ))}
              </div>
            ) : null}
            {detail.synopsis && <p className="mt-3 text-sm leading-relaxed text-subtle">{detail.synopsis}</p>}
          </div>
        </section>
      ) : null}

      <section className="mt-6">
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
        {eps.length ? (
          <div
            ref={gridRef}
            className="mt-2 grid max-h-96 grid-cols-3 gap-2 overflow-y-auto rounded-lg border border-border bg-card p-2 sm:grid-cols-4 md:grid-cols-5"
          >
            {eps.map((e) => (
              <Link
                key={e.id}
                to={watchHref(animeId, e.id)}
                data-ep={e.id}
                aria-current={e.id === epId ? 'true' : 'false'}
                className={`rounded-lg px-2 py-1.5 text-center text-sm ${
                  e.id === epId
                    ? 'bg-foreground font-semibold text-background'
                    : 'text-subtle hover:bg-raised hover:text-foreground'
                }`}
              >
                {e.number}
              </Link>
            ))}
          </div>
        ) : null}
      </section>
    </>
  );
}

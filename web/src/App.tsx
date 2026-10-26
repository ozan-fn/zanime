import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, Route, Routes, useNavigate, useParams, useSearchParams } from 'react-router';
import {
  ArrowLeft,
  Check,
  Copy,
  Film,
  Loader2,
  Play,
  Search,
  Star,
  Subtitles,
  Volume2,
} from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  Dialog,
  Input,
  Spinner,
  cn,
} from './ui';
import './App.css';

// ---------- tipe API ----------

type Meta = {
  id: string;
  type: 'series' | 'movie';
  name: string;
  releaseInfo?: string;
  imdbRating?: string;
  poster?: string;
  description?: string;
};

type Video = {
  id: string;
  season: number;
  episode: number;
  title?: string;
  released?: string;
  thumbnail?: string;
};

type MetaDetail = {
  meta: {
    id: string;
    type: string;
    name: string;
    poster?: string;
    background?: string;
    logo?: string;
    description?: string;
    releaseInfo?: string;
    imdbRating?: string;
    runtime?: string;
    genres?: string[];
    cast?: string[];
    videos?: Video[];
  };
};

type Pick = {
  name: string;
  title?: string;
  url?: string;
  infoHash?: string;
  fileIdx?: number | null;
  behaviorHints?: { filename?: string };
  magnet: string;
  trackers?: string[] | null;
};

type Track = { index: number; lang?: string; title?: string };

type Status = {
  sid: string;
  ready: boolean;
  err?: string;
  video: { idx: number; name: string; size: number; ext: string; container: string };
  subFiles?: { idx: number; name: string }[];
  probe: { started: boolean; done?: boolean; err?: string };
  audio: Track[];
  subsEmb: Track[];
  downloaded?: number;
  total?: number;
  seeders?: number;
  peers?: number;
  downBps?: number;
  upBps?: number;
  remux?: { running: boolean; err?: string };
  duration?: number;
  subsCached?: string[]; // ref ("t7"/"f3") yang sudah siap dipasang di player
  subsPartial?: Record<string, number>; // ref tertanam yang masih mengalir dari pass video (ref → byte)
  subsErr?: Record<string, string>; // ref → alasan gagal ekstrak
};

const pad2 = (n: number) => String(n).padStart(2, '0');

// deteksi label subtitle Indonesia (lang "ind"/"id" atau nama file "Indonesia")
const INDO_RE = /(^|[^a-z])(ind|idn|id|indonesia|indo|bahasa)([^a-z]|$)/i;
const isIndoLabel = (s: string) => INDO_RE.test(s) || /indonesia/i.test(s);

const toMbps = (bps?: number) => ((bps ?? 0) * 8) / 1e6;

async function api<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {
      /* bukan JSON — pakai status saja */
    }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

function fmtSize(bytes?: number): string {
  if (!bytes) return '';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

// durasi dari probe (detik) → m:ss / h:mm:ss
function fmtDuration(sec?: number): string {
  if (!sec || sec <= 0) return '';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.round(sec % 60);
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`;
}

// ---------- parsing info stream Torrentio ----------

type StreamInfo = {
  quality: string;
  filename: string;
  seeders: string;
  size: string;
  provider: string;
  labels: string;
};

const QUALITY_RE = /\b(4k|2160p|1080p|720p|480p|360p)\b/i;

function parseStream(p: Pick): StreamInfo {
  const nameLines = (p.name ?? '').split('\n').map((s) => s.trim()).filter(Boolean);
  const titleLines = (p.title ?? '').split('\n').map((s) => s.trim()).filter(Boolean);
  const filename = p.behaviorHints?.filename || titleLines[0] || '';
  const metaIdx = titleLines.findIndex((l) => l.includes('👤') || l.includes('💾'));
  const metaLine = metaIdx >= 0 ? titleLines[metaIdx] : '';
  const quality =
    nameLines.find((l) => QUALITY_RE.test(l)) ||
    QUALITY_RE.exec(filename)?.[0] ||
    nameLines[0] ||
    '';
  const labels = titleLines
    .slice(metaIdx >= 0 ? metaIdx + 1 : filename ? 1 : 0)
    .join(' · ');
  return {
    quality: quality.toUpperCase(),
    filename,
    seeders: metaLine.match(/👤\s*(\d+)/)?.[1] ?? '',
    size: metaLine.match(/💾\s*([\d.,]+\s*\w+)/)?.[1] ?? '',
    provider: metaLine.match(/⚙️\s*(\S+)/)?.[1] ?? '',
    labels,
  };
}

// ---------- halaman: cari ----------

function SearchPage() {
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [results, setResults] = useState<Meta[]>([]);
  const [searched, setSearched] = useState(false);

  async function search(e?: FormEvent) {
    e?.preventDefault();
    if (!q.trim() || busy) return;
    setBusy(true);
    setErr('');
    setResults([]);
    try {
      const metas = await api<Meta[]>(`/api/search?q=${encodeURIComponent(q.trim())}`);
      setResults(metas);
      setSearched(true);
      if (!metas.length) setErr('Tidak ada hasil.');
    } catch (e2) {
      setErr('Gagal cari: ' + (e2 as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto max-w-5xl px-4 pb-24">
      <header className="flex flex-col items-center gap-6 py-16 text-center">
        <div className="flex items-center gap-2 text-2xl font-bold tracking-tight">
          <Film className="size-7" /> zanime
        </div>
        <p className="max-w-md text-sm text-muted-foreground">
          Cari anime, pilih episode, langsung tonton — tanpa unduh, tanpa ribet.
        </p>
        <form onSubmit={search} className="flex w-full max-w-lg gap-2">
          <div className="relative flex-1">
            <Search className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Judul anime / film…"
              className="h-10 pl-9"
              autoFocus
            />
          </div>
          <Button type="submit" disabled={busy} className="h-10 px-5">
            {busy ? <Loader2 className="size-4 animate-spin" /> : 'Cari'}
          </Button>
        </form>
        {err && <p className="text-sm text-destructive">{err}</p>}
      </header>

      {searched && results.length > 0 && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-5">
          {results.map((m) => (
            <Link key={m.id + m.type} to={`/title/${m.type}/${m.id}`} className="group">
              <Card className="overflow-hidden transition-colors group-hover:border-ring">
                <div className="relative aspect-[2/3] bg-muted">
                  {m.poster ? (
                    <img
                      src={m.poster}
                      alt={m.name}
                      loading="lazy"
                      className="size-full object-cover transition-transform duration-200 group-hover:scale-[1.03]"
                    />
                  ) : (
                    <div className="flex size-full items-center justify-center text-muted-foreground">
                      <Film className="size-8" />
                    </div>
                  )}
                  <Badge
                    variant={m.type === 'series' ? 'secondary' : 'accent'}
                    className="absolute top-2 left-2"
                  >
                    {m.type === 'series' ? 'TV' : 'FILM'}
                  </Badge>
                  <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 to-transparent p-2 pt-6">
                    <p className="truncate text-xs font-semibold text-white">{m.name}</p>
                    <div className="mt-1 flex items-center gap-2 text-[11px] text-zinc-300">
                      <span>{m.releaseInfo ?? '—'}</span>
                      {m.imdbRating && (
                        <span className="flex items-center gap-0.5 text-amber-400">
                          <Star className="size-3 fill-amber-400" /> {m.imdbRating}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

// ---------- halaman: detail + episode + dialog stream ----------

function TitlePage() {
  const { type, id } = useParams<{ type: string; id: string }>();
  const [sp, setSp] = useSearchParams();
  const ep = sp.get('ep'); // "1:2" → dialog terbuka; null → tertutup
  const [detail, setDetail] = useState<MetaDetail | null>(null);
  const [err, setErr] = useState('');
  const [loading, setLoading] = useState(true);
  const [season, setSeason] = useState(1);

  useEffect(() => {
    if (!type || !id) return;
    setLoading(true);
    api<MetaDetail>(`/api/meta?type=${type}&id=${id}`)
      .then((d) => {
        setDetail(d);
        const vids = d.meta.videos ?? [];
        if (vids.length) {
          const firstSeries = vids.find((v) => v.season > 0) ?? vids[0];
          setSeason(firstSeries.season);
        }
      })
      .catch((e) => setErr('Gagal memuat detail: ' + e.message))
      .finally(() => setLoading(false));
  }, [type, id]);

  const meta = detail?.meta;
  const videos = useMemo(() => meta?.videos ?? [], [meta]);
  const seasons = useMemo(() => [...new Set(videos.map((v) => v.season))], [videos]);
  const episodes = useMemo(() => videos.filter((v) => v.season === season), [videos, season]);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner className="size-6" />
      </div>
    );
  }
  if (err || !meta) {
    return (
      <div className="mx-auto max-w-lg px-4 py-16 text-center">
        <p className="text-sm text-destructive">{err || 'Detail tidak ditemukan.'}</p>
        <Link to="/">
          <Button variant="outline" className="mt-4">
            <ArrowLeft className="size-4" /> Kembali cari
          </Button>
        </Link>
      </div>
    );
  }

  const isMovie = (type ?? 'series') === 'movie';
  const openEp = (v: Video) => {
    const next = new URLSearchParams(sp);
    next.set('ep', `${v.season}:${v.episode}`);
    setSp(next);
  };
  const closeEp = () => {
    const next = new URLSearchParams(sp);
    next.delete('ep');
    setSp(next, { replace: true });
  };
  const openMovie = () => {
    const next = new URLSearchParams(sp);
    next.set('ep', 'play');
    setSp(next);
  };

  return (
    <div className="pb-24">
      {/* hero */}
      <div className="relative border-b border-border">
        <div className="absolute inset-0 overflow-hidden">
          {meta.background && (
            <img src={meta.background} alt="" className="size-full object-cover opacity-30" />
          )}
          <div className="absolute inset-0 bg-gradient-to-t from-background via-background/70 to-background/30" />
        </div>
        <div className="relative mx-auto flex max-w-5xl gap-6 px-4 py-10">
          <Link to="/" className="absolute top-4 left-4 z-10" aria-label="Kembali">
            <Button variant="ghost" size="icon">
              <ArrowLeft className="size-4" />
            </Button>
          </Link>
          <Card className="hidden w-44 shrink-0 overflow-hidden sm:block">
            {meta.poster ? (
              <img src={meta.poster} alt={meta.name} className="aspect-[2/3] size-full object-cover" />
            ) : (
              <div className="flex aspect-[2/3] items-center justify-center bg-muted text-muted-foreground">
                <Film className="size-8" />
              </div>
            )}
          </Card>
          <div className="min-w-0 pt-8">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={isMovie ? 'accent' : 'secondary'}>{isMovie ? 'FILM' : 'SERIES'}</Badge>
              {meta.releaseInfo && <span className="text-sm text-muted-foreground">{meta.releaseInfo}</span>}
              {meta.imdbRating && (
                <span className="flex items-center gap-1 text-sm text-amber-400">
                  <Star className="size-3.5 fill-amber-400" /> {meta.imdbRating}
                </span>
              )}
              {meta.runtime && <span className="text-sm text-muted-foreground">{meta.runtime}</span>}
            </div>
            <h1 className="mt-2 text-3xl font-bold tracking-tight">{meta.name}</h1>
            {meta.genres && meta.genres.length > 0 && (
              <div className="mt-3 flex flex-wrap gap-1.5">
                {meta.genres.map((g) => (
                  <Badge key={g} variant="outline">{g}</Badge>
                ))}
              </div>
            )}
            {meta.description && (
              <p className="mt-4 max-w-2xl text-sm leading-relaxed text-muted-foreground">
                {meta.description}
              </p>
            )}
            {meta.cast && meta.cast.length > 0 && (
              <p className="mt-3 text-xs text-muted-foreground">
                <span className="font-medium text-foreground">Pemeran:</span> {meta.cast.slice(0, 6).join(', ')}
              </p>
            )}
            <div className="mt-5 flex gap-2">
              {isMovie ? (
                <Button onClick={openMovie}>
                  <Play className="size-4" /> Tonton
                </Button>
              ) : videos.length > 0 ? (
                <Button onClick={() => openEp(episodes[0] ?? videos[0])}>
                  <Play className="size-4" /> Tonton E{pad2((episodes[0] ?? videos[0]).episode)}
                </Button>
              ) : null}
            </div>
          </div>
        </div>
      </div>

      {/* episode */}
      {!isMovie && (
        <div className="mx-auto max-w-5xl px-4 py-8">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="text-lg font-semibold">Episode</h2>
            {seasons.length > 1 && (
              <div className="flex flex-wrap gap-1">
                {seasons.map((s) => (
                  <Button
                    key={s}
                    size="sm"
                    variant={s === season ? 'default' : 'outline'}
                    onClick={() => setSeason(s)}
                  >
                    {s === 0 ? 'Special' : `S${pad2(s)}`}
                  </Button>
                ))}
              </div>
            )}
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {episodes.map((v) => (
              <Card
                key={v.id}
                className="cursor-pointer transition-colors hover:border-ring"
                onClick={() => openEp(v)}
              >
                <div className="flex gap-3 p-3">
                  <div className="h-16 w-28 shrink-0 overflow-hidden rounded-[2px] bg-muted">
                    {v.thumbnail && (
                      <img src={v.thumbnail} alt="" loading="lazy" className="size-full object-cover" />
                    )}
                  </div>
                  <div className="min-w-0">
                    <p className="text-xs font-medium text-muted-foreground">
                      {v.season === 0 ? 'SP' : `E`}{pad2(v.episode)}
                      {v.released && <span className="ml-2 opacity-70">{v.released.slice(0, 10)}</span>}
                    </p>
                    <p className="truncate text-sm font-medium">{v.title || `Episode ${v.episode}`}</p>
                  </div>
                  <Play className="ml-auto size-4 shrink-0 self-center text-muted-foreground" />
                </div>
              </Card>
            ))}
            {!episodes.length && (
              <p className="text-sm text-muted-foreground">Season ini tidak punya episode.</p>
            )}
          </div>
        </div>
      )}

      {/* dialog stream — terbuka lewat ?ep= (URL), jadi tombol Back membukanya lagi */}
      <StreamDialog
        open={ep !== null}
        onClose={closeEp}
        type={(type ?? 'series') as Meta['type']}
        id={id ?? ''}
        name={meta.name}
        episodeKey={ep && ep !== 'play' ? ep : null}
      />
    </div>
  );
}

// ---------- dialog pilih torrent ----------

function StreamDialog({
  open,
  onClose,
  type,
  id,
  name,
  episodeKey,
}: {
  open: boolean;
  onClose: () => void;
  type: Meta['type'];
  id: string;
  name: string;
  episodeKey: string | null;
}) {
  const navigate = useNavigate();
  const [streams, setStreams] = useState<Pick[] | null>(null);
  const [note, setNote] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState('');
  const [allSeries, setAllSeries] = useState(false);

  const videoId = episodeKey ? `${id}:${episodeKey}` : id;

  const load = async (targetId: string, isFallback: boolean) => {
    setBusy(true);
    setErr('');
    setStreams(null);
    setNote(isFallback ? 'Semua torrent serial (episode spesifik tidak tersedia):' : '');
    try {
      const data = await api<Pick[]>(
        `/api/streams?type=${type}&id=${targetId}&name=${encodeURIComponent(name)}`,
      );
      setStreams(data);
      if (!data.length && !isFallback && targetId.includes(':')) {
        setAllSeries(true);
        await load(targetId.split(':')[0], true);
        return;
      }
      if (!data.length && isFallback) setNote('');
    } catch (e) {
      setErr('Gagal ambil stream: ' + (e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    if (!open || !id) return;
    setAllSeries(false);
    void load(videoId, false);
  }, [open, videoId, id]);

  const play = async (p: Pick) => {
    if (!p.infoHash) return;
    try {
      const q = new URLSearchParams();
      q.set('hash', p.infoHash);
      if (typeof p.fileIdx === 'number') q.set('fileIdx', String(p.fileIdx));
      for (const tr of p.trackers ?? []) q.append('tr', tr);
      const start = await api<{ sid: string }>(`/api/torrent/start?${q.toString()}`);
      const label = parseStream(p);
      navigate(
        `/watch/${start.sid}?name=${encodeURIComponent(label.filename || name)}`,
      );
    } catch (e) {
      setErr('Gagal mulai torrent: ' + (e as Error).message);
    }
  };

  const copyMagnet = async (p: Pick) => {
    try {
      await navigator.clipboard.writeText(p.magnet);
      setCopied(p.magnet);
      setTimeout(() => setCopied(''), 1500);
    } catch {
      /* clipboard ditolak */
    }
  };

  return (
    <Dialog open={open} onClose={onClose} title={
      <span className="flex items-center gap-2">
        <Play className="size-4" /> Pilih torrent
        <span className="font-normal text-muted-foreground">
          — {name}{episodeKey ? ` S${episodeKey.replace(':', 'E')}` : ''}
        </span>
      </span>
    }>
      {err && <p className="mb-3 text-sm text-destructive">{err}</p>}
      {note && <p className="mb-3 text-xs text-amber-400">{note}</p>}
      {busy && (
        <div className="flex items-center gap-2 py-8 text-sm text-muted-foreground">
          <Spinner /> Mengambil daftar torrent dari Torrentio…
        </div>
      )}
      {!busy && streams && streams.length === 0 && !allSeries && (
        <p className="py-8 text-center text-sm text-muted-foreground">Tidak ada stream tersedia.</p>
      )}
      {!busy && streams && (
        <ul className="flex flex-col gap-2">
          {streams.map((p, i) => {
            const info = parseStream(p);
            return (
              <li key={p.infoHash ? p.infoHash + i : i}>
                <Card className="transition-colors hover:border-ring">
                  <div className="flex items-start gap-3 p-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-1.5">
                        {info.quality && <Badge variant="default">{info.quality}</Badge>}
                        {info.size && <Badge variant="secondary">{info.size}</Badge>}
                        {info.seeders && (
                          <Badge variant="secondary">👤 {info.seeders}</Badge>
                        )}
                        {info.provider && <Badge variant="outline">{info.provider}</Badge>}
                      </div>
                      <p className="mt-1.5 truncate text-sm font-medium" title={info.filename}>
                        {info.filename || '(tanpa nama file)'}
                      </p>
                      {info.labels && (
                        <p className="mt-0.5 truncate text-xs text-muted-foreground">{info.labels}</p>
                      )}
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <Button
                        size="icon"
                        variant="ghost"
                        onClick={() => copyMagnet(p)}
                        aria-label="Salin magnet"
                      >
                        {copied === p.magnet ? (
                          <Check className="size-4 text-emerald-500" />
                        ) : (
                          <Copy className="size-4" />
                        )}
                      </Button>
                      <Button
                        size="icon"
                        disabled={!p.infoHash}
                        onClick={() => play(p)}
                        aria-label="Tonton"
                        title={p.infoHash ? 'Tonton' : 'Stream ini hanya punya URL, bukan torrent'}
                      >
                        <Play className="size-4" />
                      </Button>
                    </div>
                  </div>
                </Card>
              </li>
            );
          })}
        </ul>
      )}
    </Dialog>
  );
}

// ---------- halaman: nonton ----------

function WatchPage() {
  const { sid } = useParams<{ sid: string }>();
  const navigate = useNavigate();
  const [sp] = useSearchParams();
  const name = sp.get('name') || 'Video';
  const [status, setStatus] = useState<Status | null>(null);
  const [err, setErr] = useState('');
  const [audio, setAudio] = useState(0);
  const [subKey, setSubKey] = useState<string | null>(null); // null = belum disentuh user
  const [started, setStarted] = useState(false);
  const [forcePlay, setForcePlay] = useState(false); // probe kelamaan → putar duluan
  const [live, setLive] = useState(false); // status real-time lewat WebSocket
  const liveRef = useRef(false);
  const videoRef = useRef<HTMLVideoElement>(null);

  const ready = !!status?.ready && (status?.video.idx ?? -1) >= 0;
  const progress =
    status?.total && status.downloaded !== undefined
      ? Math.min(100, Math.round((status.downloaded / status.total) * 100))
      : 0;

  const subsCached = status?.subsCached ?? [];
  const subsPartial = status?.subsPartial ?? {};
  // versi URL subtitle: berubah saat cue baru masuk, sehingga player memuat ulang
  const subVersion = (ref: string) =>
    subsCached.includes(ref) ? `f${ref.length}` : subsPartial[ref] ? `p${subsPartial[ref]}` : '';
  const subReady = (ref: string) => subVersion(ref) !== '';
  const partialSig = Object.entries(subsPartial)
    .map(([k, v]) => `${k}:${v}`)
    .join(',');

  // daftar subtitle: Indonesia diprioritaskan di urutan atas
  const subOptions: { key: string; label: string; src: string; indo: boolean }[] = [];
  if (ready && sid) {
    for (const t of status.subsEmb ?? []) {
      const label = [t.lang, t.title].filter(Boolean).join(' · ') || `Subtitle ${t.index + 1}`;
      subOptions.push({
        key: `t-${t.index}`,
        label,
        src: `/subs/${sid}/${status.video.idx}/t${t.index}.vtt`,
        indo: isIndoLabel(label),
      });
    }
    for (const f of status.subFiles ?? []) {
      subOptions.push({
        key: `f-${f.idx}`,
        label: f.name,
        src: `/subs/${sid}/${status.video.idx}/f${f.idx}.vtt`,
        indo: isIndoLabel(f.name),
      });
    }
    subOptions.sort((a, b) => Number(b.indo) - Number(a.indo));
  }
  // default: subtitle Indonesia pertama (kalau ada), kecuali user sudah memilih
  const activeSub = subKey ?? subOptions.find((o) => o.indo)?.key ?? '';
  const activeOpt = subOptions.find((o) => o.key === activeSub);
  const activeRef = activeOpt?.key.replace('-', '') ?? '';
  const activeReady = !!activeOpt && subReady(activeRef);
  const activeErr = status?.subsErr?.[activeRef];
  // dipasang kalau sudah ada isinya — boleh sebagian (mengalir dari pass video)
  const readySubs = subOptions.filter((o) => subReady(o.key.replace('-', '')));

  // Mulai memutar setelah probe selesai (hanya header ~1 MB → durasi + daftar track).
  // Kalau peer lambat, jangan tahan terus: setelah 8 detik tetap putar, daftar track
  // menyusul dari status poll. Subtitle TIDAK pernah ditunggu.
  const probeDone = !!status?.probe?.done;
  const canPlay = ready && (probeDone || forcePlay);

  // batas tunggu probe: 8 dtk. Lewat itu video tetap diputar (durasi/track menyusul).
  useEffect(() => {
    if (!ready || probeDone || forcePlay) return;
    const t = setTimeout(() => setForcePlay(true), 8000);
    return () => clearTimeout(t);
  }, [ready, probeDone, forcePlay]);
  useEffect(() => setForcePlay(false), [sid]);

  // WebSocket: push status tiap 500 ms → indikator alur bergerak tanpa polling.
  // Kalau putus (proxy tanpa upgrade, jaringan), polling biasa tetap jalan.
  useEffect(() => {
    if (!sid) return;
    let closed = false;
    let ws: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const connect = () => {
      const proto = location.protocol === 'https:' ? 'wss' : 'ws';
      ws = new WebSocket(`${proto}://${location.host}/api/torrent/ws/${sid}`);
      ws.onopen = () => {
        liveRef.current = true;
        setLive(true);
      };
      ws.onmessage = (ev) => {
        try {
          const st = JSON.parse(ev.data) as Status;
          setStatus(st);
          setErr(st.err || '');
        } catch {
          /* payload rusak → biarkan polling yang menangani */
        }
      };
      ws.onclose = () => {
        liveRef.current = false;
        setLive(false);
        ws = null;
        if (!closed) retry = setTimeout(connect, 2500);
      };
      ws.onerror = () => ws?.close();
    };
    connect();
    return () => {
      closed = true;
      clearTimeout(retry);
      ws?.close();
      liveRef.current = false;
      setLive(false);
    };
  }, [sid]);

  // polling cadangan: cepat saat WS belum tersambung, pelan sebagai fallback
  useEffect(() => {
    if (!sid) return;
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      if (stop) return;
      let next = liveRef.current ? 4000 : 700; // WS aktif → polling jadi cadangan
      try {
        const st = await api<Status>(`/api/torrent/status/${sid}`);
        if (stop) return;
        setStatus(st);
        setErr(st.err || '');
        if (!st.ready && !st.err) next = 800; // fase awal: lebih responsif
      } catch (e) {
        if (!stop) {
          setErr((e as Error).message);
          next = 4000;
        }
      }
      if (!stop) timer = setTimeout(tick, next);
    };
    void tick();
    return () => {
      stop = true;
      clearTimeout(timer);
    };
  }, [sid]);

  // subtitle FILE terpisah belum siap → minta server mengekstrak (sekali per ref).
  // Yang tertanam tidak diminta selama lewat remux: ia ikut pass video (tanpa unduh ulang);
  // hanya container direct (tanpa pass ffmpeg) yang perlu diekstrak sendiri.
  const directContainer = status?.video?.container === 'direct';
  useEffect(() => {
    if (!sid || !ready || !activeRef || activeReady || activeErr) return;
    if (activeRef.startsWith('t') && !directContainer) return;
    void api<Status>(`/api/torrent/status/${sid}?warm=${activeRef}`).catch(() => {});
  }, [sid, ready, activeRef, activeReady, activeErr, directContainer]);

  // pilih track subtitle → mode showing (ulang saat daftar track bertambah)
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    const tracks = Array.from(v.textTracks);
    for (const t of tracks) t.mode = 'disabled';
    if (activeSub) {
      const t = tracks.find((x) => x.id === activeSub);
      if (t) t.mode = 'showing';
    }
  }, [activeSub, status?.ready, subsCached.join(','), partialSig, status?.subsEmb?.length, status?.subFiles?.length]);

  if (!sid) return null;

  return (
    <div className="mx-auto max-w-6xl px-4 py-4">
      <div className="mb-3 flex items-center gap-3">
        <Button variant="outline" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="size-4" /> Kembali
        </Button>
        <h1 className="min-w-0 flex-1 truncate text-sm font-semibold" title={name}>
          {name}
        </h1>
        {status && status.ready && (
          <div className="hidden items-center gap-3 text-xs text-muted-foreground sm:flex">
            {status.seeders !== undefined && <span>👤 {status.seeders}</span>}
            {status.peers !== undefined && <span>🔗 {status.peers}</span>}
            {!!status.duration && <span>⏱ {fmtDuration(status.duration)}</span>}
            <span>⬇ {fmtSize(status.downloaded)}</span>
          </div>
        )}
      </div>

      {err && (
        <Card className="mb-3 border-destructive/50 p-4 text-sm text-destructive">
          {err}
          <Button variant="outline" size="sm" className="ml-3" onClick={() => location.reload()}>
            Coba lagi
          </Button>
        </Card>
      )}

      {ready && !canPlay && (
        <Card className="flex flex-col items-center gap-3 p-10 text-muted-foreground">
          <Spinner className="size-6" />
          <p className="text-sm">Menyiapkan info video…</p>
          <p className="text-center text-xs">
            Hanya membaca beberapa MB untuk durasi &amp; daftar track; subtitle menyusul saat siap.
          </p>
          {progress > 0 && (
            <div className="h-1.5 w-64 overflow-hidden rounded-[2px] bg-muted">
              <div className="h-full bg-primary transition-all" style={{ width: `${progress}%` }} />
            </div>
          )}
        </Card>
      )}

      {!ready && !err && (
        <Card className="flex flex-col items-center gap-3 p-10 text-muted-foreground">
          <Spinner className="size-6" />
          <p className="text-sm">
            {status?.ready === false || !status
              ? 'Mengambil info torrent… (menunggu peer)'
              : 'Menyiapkan video…'}
          </p>
          {progress > 0 && (
            <div className="h-1.5 w-64 overflow-hidden rounded-[2px] bg-muted">
              <div className="h-full bg-primary transition-all" style={{ width: `${progress}%` }} />
            </div>
          )}
        </Card>
      )}

      {ready && canPlay && (
        <>          <Card className="mb-2 flex items-center gap-3 overflow-x-auto px-3 py-2">
            <span className="shrink-0 text-xs font-medium">Peer</span>
            <FlowSpeed bps={status.downBps} />
            <span className="shrink-0 text-xs font-medium">Server</span>
            <FlowSpeed bps={status.upBps} />
            <span className="shrink-0 text-xs font-medium">Kamu</span>
            <span className="ml-auto hidden shrink-0 text-[11px] text-muted-foreground md:block">
              {live ? 'realtime · WS' : 'tiap 700 ms'}
            </span>
          </Card>

          <Card className="overflow-hidden bg-black">
            <video
              key={`${sid}-${audio}`}
              ref={videoRef}
              src={`/stream/${sid}/${status.video.idx}?audio=${audio}`}
              controls
              autoPlay
              playsInline
              onPlaying={() => setStarted(true)}
              className="aspect-video size-full bg-black"
            >
              {readySubs.map((s) => {
                // hanya subtitle yang sedang dipilih yang dimuat ulang saat cue baru
                // masuk — track lain tidak di-fetch berulang (pindah track tetap instan).
                const ref = s.key.replace('-', '');
                const ver = ref === activeRef ? subVersion(ref) : 'idle';
                return (
                  <track
                    key={`${s.key}-${ver}`}
                    id={s.key}
                    kind="subtitles"
                    src={`${s.src}?v=${ver}`}
                    label={s.label}
                  />
                );
              })}
            </video>
          </Card>

          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <Card className="p-3">
              <label className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <Volume2 className="size-3.5" /> Audio
              </label>
              <select
                className="h-9 w-full rounded-[2px] border border-input bg-background px-2 text-sm text-foreground"
                value={audio}
                onChange={(e) => setAudio(Number(e.target.value))}
              >
                {(status.audio?.length ? status.audio : [{ index: 0 }]).map((t) => (
                  <option key={t.index} value={t.index}>
                    {[t.lang, t.title].filter(Boolean).join(' · ') || `Track ${t.index + 1}`}
                  </option>
                ))}
              </select>
              {status.probe?.err && (
                <p className="mt-1 text-[11px] text-amber-500">
                  Info track terbatas: {status.probe.err}
                </p>
              )}
            </Card>

            <Card className="p-3">
              <label className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <Subtitles className="size-3.5" /> Subtitle
              </label>
              <select
                className="h-9 w-full rounded-[2px] border border-input bg-background px-2 text-sm text-foreground"
                value={activeSub}
                onChange={(e) => setSubKey(e.target.value)}
              >
                <option value="">Matikan</option>
                {subOptions.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.label}
                  </option>
                ))}
              </select>
              {!subOptions.length && (
                <p className="mt-1 text-[11px] text-muted-foreground">
                  {status.probe?.started
                    ? status.probe.err
                      ? `Gagal memindai subtitle: ${status.probe.err}`
                      : 'Tidak ada subtitle di file ini.'
                    : 'Memindai subtitle…'}
                </p>
              )}
              {activeOpt && !activeReady && !activeErr && (
                <p className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                  <Spinner className="size-3" />
                  {activeRef.startsWith('t') && !directContainer
                    ? 'Subtitle mengalir bersama video (tanpa unduh ulang)…'
                    : 'Menyiapkan subtitle di latar belakang…'}
                </p>
              )}
              {activeErr && (
                <p className="mt-1 text-[11px] text-destructive">Gagal menyiapkan subtitle: {activeErr}</p>
              )}
            </Card>
          </div>

          {!probeDone && forcePlay && (
            <p className="mt-2 text-[11px] text-muted-foreground">
              Info track belum selesai — video diputar lebih dulu; daftar audio/subtitle menyusul.
            </p>
          )}
          {status.remux?.running && !started && (
            <p className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
              <Spinner className="size-3.5" /> Mengubah ke MP4 sambil streaming — video mulai otomatis…
            </p>
          )}
          {status.remux?.running && started && (
            <p className="mt-2 text-[11px] text-muted-foreground">
              Transmux MKV → MP4 sambil diputar (tanpa cache di disk; seek belum tersedia).
            </p>
          )}
          {status.remux?.err && !status.remux.running && (
            <p className="mt-2 text-xs text-destructive">Remux: {status.remux.err}</p>
          )}
        </>
      )}
    </div>
  );
}

// alur kecepatan: peer -- [mbps] -- server -- [mbps] -- kamu (dash berjalan hanya saat ada aliran)
function FlowSpeed({ bps }: { bps?: number }) {
  const mbps = toMbps(bps);
  const active = mbps > 0.05;
  return (
    <div className="flex min-w-[110px] flex-1 flex-col items-center gap-1">
      <span className="text-[11px] leading-none font-semibold tabular-nums">
        {mbps.toFixed(1)}{' '}
        <span className="font-normal text-muted-foreground">Mbps</span>
      </span>
      <span
        className={cn(
          'flow-dash w-full',
          active ? 'text-emerald-500' : 'flow-idle text-muted-foreground/30',
        )}
      />
    </div>
  );
}

// ---------- router ----------

function NotFound() {
  return (
    <div className="mx-auto max-w-lg px-4 py-24 text-center">
      <Card className="p-8">
        <p className="text-4xl font-bold">404</p>
        <p className="mt-2 text-sm text-muted-foreground">Halaman tidak ada.</p>
        <Link to="/">
          <Button className="mt-4">Kembali ke cari</Button>
        </Link>
      </Card>
    </div>
  );
}

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<SearchPage />} />
      <Route path="/title/:type/:id" element={<TitlePage />} />
      <Route path="/watch/:sid" element={<WatchPage />} />
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}

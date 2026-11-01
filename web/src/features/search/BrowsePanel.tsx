import { useRef, useState } from 'react'
import { Search } from 'lucide-react'
import { api } from '@/lib/api'
import { log } from '@/lib/logs'
import { Sq } from '@/features/text/Sq'
import type { Episode, Item, Meta, Server, Sub } from '@/lib/types'

/* Sidebar kiri: cari judul, hasil, dan episode TV. Stream (server/kualitas)
 * dikirim ke sidebar kanan lewat onStreams; panel ini tidak pernah ditutup. */

type Tab = 'movie' | 'tv'
type View = 'results' | 'episodes'

// Baris stream = satu pilihan yang bisa diklik di sidebar server.
export type StreamRow = {
  name: string
  type: string
  quality: string
  subs: Sub[]
  url: string
}

export type Streams = {
  title: string
  rows: StreamRow[]
  // Konteks judul/episode untuk kartu subtitle (di bawah player).
  media?: {
    tab: Tab
    id: string
    name: string
    year?: string
    season?: number
    episode?: number
  }
}

const TABS: Array<{ label: string; tab: Tab }> = [
  { label: 'Movie', tab: 'movie' },
  { label: 'TV', tab: 'tv' },
]

// Satu server → satu baris per kualitas; server gagal resolve dibuang dan
// hanya server premium yang ditampilkan.
const flt = (servers: Server[]): StreamRow[] =>
  servers.filter((s) => /premium/i.test(s.name)).flatMap((s) => {
    if (s.err || !s.url) return []
    const mk = (url: string, q: string): StreamRow => ({
      name: s.name,
      type: s.type,
      quality: q,
      subs: (s.subs || []).map((x) => ({ label: x.label, lang: x.lang, url: x.url })),
      url,
    })
    return s.qualities && s.qualities.length
      ? s.qualities.map((q) => mk(q.url, q.label))
      : [mk(s.url!, s.quality ? String(s.quality).toLowerCase() : '')]
  })

function Note({ children }: { children: React.ReactNode }) {
  return <p className="p-4 text-center text-xs text-zinc-500">{children}</p>
}

function Chip({ label, on, disabled, onClick }: { label: React.ReactNode; on?: boolean; disabled?: boolean; onClick: () => void }) {
  return (
    <button type="button" disabled={disabled} onClick={onClick} className={`chip${on ? ' on' : ''}`}>{label}</button>
  )
}

export function BrowsePanel({ onStreams }: { onStreams: (s: Streams) => void }) {
  const [tab, setTab] = useState<Tab>('movie')
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Item[]>([])
  const [meta, setMeta] = useState<Meta | null>(null)
  const [season, setSeason] = useState(1)
  const [view, setView] = useState<View>('results')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  /* Spam klik: setiap permintaan baru membatalkan yang masih jalan, dan respons
   * lama diabaikan. Backend ikut membatalkan scrape-nya saat koneksi ditutup. */
  const inflight = useRef<AbortController | null>(null)
  function start() {
    if (inflight.current) log.info('Permintaan sebelumnya dibatalkan', 'dlg:abort')
    inflight.current?.abort()
    const c = new AbortController()
    inflight.current = c
    return c.signal
  }
  const latest = (signal: AbortSignal) => !signal.aborted

  // Cari judul (backend memanggil source catalog dan men-cache ke ./tmp).
  async function doSearch(query: string, useTab: Tab = tab) {
    setError(''); setResults([]); setMeta(null); setView('results')
    if (!query.trim()) return
    const signal = start()
    setBusy('Mencari…')
    log.info(`Mencari judul "${query}" (${useTab === 'tv' ? 'TV' : 'Movie'})…`, 'dlg:search')
    try {
      const body = await api<{ results: Item[] }>(`/api/search?tab=${useTab}&q=${encodeURIComponent(query)}`, signal)
      if (!latest(signal)) return
      setResults(body.results || [])
      log.ok(`Pencarian selesai · ${(body.results || []).length} judul ditemukan`, 'dlg:search')
    } catch (err) {
      if (!latest(signal)) return
      log.err(`Pencarian gagal · ${(err as Error).message}`, 'dlg:search')
      setError((err as Error).message)
    } finally {
      if (latest(signal)) setBusy('')
    }
  }

  // Pilih judul: movie langsung ke streams; tv ke daftar episode.
  async function openTitle(item: Item) {
    const signal = start()
    setError(''); setBusy('Memuat detail…')
    log.info(`Membuka detail "${item.name}"…`, 'dlg:meta')
    try {
      const m = await api<Meta>(`/api/meta/${tab}/${item.id}`, signal)
      if (!latest(signal)) return
      if (!m.tmdb) throw new Error('Judul ini tidak punya id TMDB.')
      setMeta(m)
      log.ok(`Detail siap · ${(m.episodes || []).length} episode`, 'dlg:meta')
      if (tab === 'movie') await loadStreams(m, null)
      else {
        const seasons = [...new Set((m.episodes || []).map((e) => e.season))].sort((a, b) => a - b)
        setSeason(seasons.includes(1) ? 1 : seasons[0] || 1)
        setView('episodes')
      }
    } catch (err) {
      if (!latest(signal)) return
      log.err(`Detail gagal dimuat · ${(err as Error).message}`, 'dlg:meta')
      setError((err as Error).message)
    } finally {
      if (latest(signal)) setBusy('')
    }
  }

  // Scrapping embedmaster untuk movie atau satu episode tv.
  async function loadStreams(m: Meta, ep: Episode | null) {
    const signal = start()
    setBusy('Mencari stream…')
    const what = ep ? `${m.name} S${ep.season}E${ep.episode}` : m.name
    log.info(`Mencari server untuk "${what}"…`, 'dlg:streams')
    try {
      let url = `/api/streams?tab=${tab}&id=${encodeURIComponent(m.tmdb)}`
      if (ep) url += `&s=${ep.season}&e=${ep.episode}`
      const body = await api<{ servers: Server[] }>(url, signal)
      if (!latest(signal)) return
      const rows = flt(body.servers || [])
      onStreams({
        title: what,
        rows,
        media: {
          tab,
          id: m.id, // imdb tt… — /api/subtitles memakai meta, bukan id TMDB
          name: m.name,
          year: m.year,
          season: ep?.season,
          episode: ep?.episode,
        },
      })
      if (!rows.length) {
        log.err('Tidak ada server yang bisa diputar', 'dlg:streams')
        setError('Tidak ada server stream yang bisa diputar.')
      } else {
        log.ok(`${rows.length} server siap diputar`, 'dlg:streams')
      }
    } catch (err) {
      if (!latest(signal)) return
      log.err(`Server gagal dimuat · ${(err as Error).message}`, 'dlg:streams')
      setError((err as Error).message)
    } finally {
      if (latest(signal)) setBusy('')
    }
  }

  const seasons = [...new Set((meta?.episodes || []).map((e) => e.season))].sort((a, b) => a - b)
  const eps = (meta?.episodes || []).filter((e) => e.season === season)

  return (
    <aside className="flex min-h-0 flex-col gap-2 rounded-lg border border-border bg-card p-2.5 sm:gap-2.5 sm:rounded-xl sm:p-3 lg:h-full lg:p-4">
      <div className="flex gap-1.5">
        <input
          id="search-q"
          autoComplete="off"
          spellCheck={false}
          placeholder="Cari judul, mis. frieren"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void doSearch(q) }}
          className="h-9 min-w-0 flex-1 rounded-lg border border-border bg-zinc-900 px-3 py-2 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-zinc-600 sm:h-auto sm:px-2.5 sm:py-1.5 sm:text-xs"
        />
        <button type="button" aria-label="Cari" onClick={() => void doSearch(q)}
          className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary text-primary-contrast transition hover:bg-zinc-200 sm:h-auto sm:w-auto sm:px-2.5">
          <Search className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        {TABS.map((t) => (
          <Chip key={t.tab} label={t.label} on={tab === t.tab}
            onClick={() => { setTab(t.tab); setView('results'); setMeta(null); void doSearch(q, t.tab) }} />
        ))}
        <Chip label="Kembali" disabled={view === 'results'} onClick={() => setView('results')} />
      </div>

      {error && <p className="rounded border border-rose-900 bg-rose-950/40 px-2 py-1.5 text-[11px] text-rose-300">{error}</p>}

      <div className="min-h-0 flex-1 space-y-1.5 overflow-y-auto pr-0.5 max-h-[55vh] lg:max-h-none">
        {busy ? <Note>{busy}</Note>
          : view === 'results' ? (
            results.length
              ? results.map((m) => (
                <button key={m.id} type="button" onClick={() => void openTitle(m)}
                  className="flex w-full items-center gap-2 rounded-lg border border-border bg-zinc-900 p-1.5 text-left transition hover:border-zinc-600 hover:bg-zinc-800">
                  {m.poster && (
                    <img src={m.poster} alt="" loading="lazy"
                      className="h-16 w-11 shrink-0 rounded bg-zinc-800 object-cover sm:h-14 sm:w-10"
                      onError={(e) => (e.currentTarget.style.display = 'none')} />
                  )}
                  <span className="min-w-0">
                    <Sq text={m.name} className="block text-sm text-zinc-200 sm:text-xs" />
                    <span className="text-[10px] text-zinc-500">{[m.year, m.type === 'tv' ? 'TV' : 'Movie'].filter(Boolean).join(' · ')}</span>
                  </span>
                </button>
              ))
              : <Note>Masukkan query lalu tekan Cari.</Note>
          ) : (
            <div>
              <div className="flex flex-wrap gap-1.5">
                {seasons.map((sn) => (
                  <Chip key={sn} label={sn === 0 ? 'Specials' : `S${sn}`} on={sn === season}
                    onClick={() => setSeason(sn)} />
                ))}
              </div>
              <div className="mt-2 space-y-1.5">
                {eps.length
                  ? eps.map((v) => (
                    <button key={`${v.season}:${v.episode}`} type="button" onClick={() => void loadStreams(meta!, v)}
                      className="flex w-full items-center gap-2 rounded-lg border border-border bg-zinc-900 px-2 py-2 text-left transition hover:bg-zinc-800 sm:py-1.5">
                      <span className="grid h-6 w-7 shrink-0 place-items-center rounded bg-zinc-800 text-[10px] font-semibold text-zinc-300">
                        E{v.episode}
                      </span>
                      <Sq text={v.title || `Episode ${v.episode}`} className="min-w-0 flex-1 text-sm text-zinc-300 sm:text-xs" />
                    </button>
                  ))
                  : <Note>Season kosong.</Note>}
              </div>
            </div>
          )}
      </div>
    </aside>
  )
}

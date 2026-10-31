import { useState } from 'react'
import { Search, X } from 'lucide-react'
import { api } from '@/lib/api'
import { log } from '@/lib/logs'
import { Sq } from '@/features/text/Sq'
import { useApp } from '@/features/app/AppProvider'
import type { Episode, Item, Meta, Server, Sub } from '@/lib/types'

/* Dialog pencarian: semuanya lewat backend.
 *   - Tab Movie / TV menentukan tipe katalog.
 *   - Series: chip S0..S5 lalu daftar episode; movie langsung ke streams.
 *   - Streams: satu baris per (server, kualitas) hasil scraping embedmaster.
 *     Klik = putar lewat slug /s/<slug> milik backend (media + m3u8 + subtitle
 *     semua di-proxy server, host video butuh Referer mereka). */

type Tab = 'movie' | 'tv'
type View = 'results' | 'episodes' | 'streams'

// Baris stream = satu pilihan yang bisa diklik.
type StreamRow = {
  name: string
  type: string
  quality: string
  subs: Sub[]
  url: string
  title: string
}

const TABS: Array<{ label: string; tab: Tab }> = [
  { label: 'Movie', tab: 'movie' },
  { label: 'TV', tab: 'tv' },
]

const flt = (servers: Server[]): StreamRow[] =>
  servers.flatMap((s) => {
    if (s.err || !s.url) return []
  const mk = (url: string, q: string): StreamRow => ({
    name: s.name, type: s.type, quality: q,
    // URL subtitle sudah slug /s/… dari backend.
    subs: (s.subs || []).map((x) => ({ label: x.label, lang: x.lang, url: x.url })),
    url, title: s.name,
  })
  return s.qualities && s.qualities.length
    ? s.qualities.map((q) => mk(q.url, q.label))
    : [mk(s.url!, s.quality ? String(s.quality).toLowerCase() : '')]
})

function Empty({ children }: { children: React.ReactNode }) {
  return <p className="p-6 text-center text-sm text-zinc-500">{children}</p>
}

function Chip({ label, on, disabled, onClick }: { label: React.ReactNode; on?: boolean; disabled?: boolean; onClick: () => void }) {
  return (
    <button type="button" disabled={disabled} onClick={onClick} className={`chip${on ? ' on' : ''}`}>{label}</button>
  )
}

function ResultGrid({ results, onPick }: { results: Item[]; onPick: (m: Item) => void }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(104px,1fr))] gap-3">
      {results.map((m) => (
        <button key={m.id} type="button" onClick={() => onPick(m)} className="group text-left transition hover:-translate-y-0.5">
          <img src={m.poster} alt={m.name} loading="lazy"
            className="aspect-[2/3] w-full rounded-lg border border-border bg-zinc-900 object-cover" />
          <Sq text={m.name} className="mt-1 block text-[11px] leading-tight text-zinc-300 group-hover:text-white" />
          {m.year && <p className="text-[10px] text-zinc-500">{m.year}</p>}
        </button>
      ))}
    </div>
  )
}

function EpisodeRow({ v, onPick }: { v: Episode; onPick: () => void }) {
  const label = v.title || `Episode ${v.episode}`
  return (
    <button type="button" onClick={onPick}
      className="file-row group flex w-full items-center gap-3 rounded-lg border border-border bg-zinc-900 px-3 py-2.5 text-left transition hover:bg-zinc-800">
      {v.thumbnail && (
        <img src={v.thumbnail} alt="" loading="lazy"
          className="h-11 w-20 shrink-0 rounded-md border border-border bg-zinc-800 object-cover"
          onError={(e) => (e.currentTarget.style.display = 'none')} />
      )}
      <span className="grid h-8 w-9 shrink-0 place-items-center rounded-md bg-zinc-800 text-[11px] font-semibold text-zinc-300">
        E{v.episode}
      </span>
      <Sq text={label} className="min-w-0 flex-1 text-sm text-zinc-300 group-hover:text-white" />
    </button>
  )
}

function StreamRowView({ s, onPick }: { s: StreamRow; onPick: () => void }) {
  return (
    <button type="button" onClick={onPick}
      className="group flex w-full flex-col rounded-lg border border-border bg-zinc-900 px-3 py-2.5 text-left transition hover:border-zinc-600 hover:bg-zinc-800">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-[11px]">
        <span className="font-medium text-zinc-300">{s.name}</span>
        <span className="font-medium text-zinc-100">{s.quality || s.type.toUpperCase()}</span>
        {s.subs.length > 0 && <span className="text-zinc-500">{s.subs.length} subtitle</span>}
      </div>
      <p className="mt-1 text-[12px] tracking-wide text-zinc-500">{s.type === 'hls' ? 'HLS' : 'MP4'} · klik untuk putar</p>
    </button>
  )
}

export function SearchDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { play, setStatus } = useApp()
  const [tab, setTab] = useState<Tab>('movie')
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Item[]>([])
  const [meta, setMeta] = useState<Meta | null>(null)
  const [rows, setRows] = useState<StreamRow[]>([])
  const [season, setSeason] = useState(1)
  const [view, setView] = useState<View>('results')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  // Cari judul (backend memanggil source catalog dan men-cache ke ./tmp).
  async function doSearch(query: string, useTab: Tab = tab) {
    setError(''); setResults([]); setRows([]); setMeta(null); setView('results')
    if (!query.trim()) return
    setBusy('Mencari…')
    log.info(`cari "${query}" (${useTab})`, 'dlg:search')
    try {
      const body = await api<{ results: Item[] }>(`/api/search?tab=${useTab}&q=${encodeURIComponent(query)}`)
      setResults(body.results || [])
      log.ok(`hasil: ${(body.results || []).length} judul`, 'dlg:search')
    } catch (err) {
      log.err(`cari gagal · ${(err as Error).message}`, 'dlg:search')
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // Pilih judul: detail meta (untuk tv: daftar episode; movie: langsung streams).
  async function openTitle(item: Item) {
    setError(''); setBusy('Memuat detail…')
    log.info(`buka detail ${item.name}`, 'dlg:meta')
    try {
      const m = await api<Meta>(`/api/meta/${tab}/${item.id}`)
      if (!m.tmdb) throw new Error('Judul ini tidak punya id TMDB.')
      setMeta(m)
      log.ok(`meta ok · tmdb ${m.tmdb}`, 'dlg:meta')
      if (tab === 'movie') await loadStreams(m, null)
      else {
        const seasons = [...new Set((m.episodes || []).map((e) => e.season))].sort((a, b) => a - b)
        const next = seasons.includes(1) ? 1 : seasons[0] || 1
        setSeason(next)
        setView('episodes')
      }
    } catch (err) {
      log.err(`detail gagal · ${(err as Error).message}`, 'dlg:meta')
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // Scrapping embedmaster untuk movie atau satu episode tv.
  async function loadStreams(m: Meta, ep: Episode | null) {
    setBusy('Mencari stream…'); setRows([])
    const what = ep ? `${m.name} S${ep.season}E${ep.episode}` : m.name
    log.info(`mencari stream · ${what}`, 'dlg:streams')
    try {
      let url = `/api/streams?tab=${tab}&id=${encodeURIComponent(m.tmdb)}`
      if (ep) url += `&s=${ep.season}&e=${ep.episode}`
      const body = await api<{ servers: Server[] }>(url)
      const list = flt(body.servers || [])
      setRows(list)
      setView('streams')
      if (!list.length) {
        log.err('tidak ada server stream yang bisa diputar', 'dlg:streams')
        setError('Tidak ada server stream yang bisa diputar.')
      } else {
        log.ok(`${list.length} pilihan stream bisa diputar`, 'dlg:streams')
      }
    } catch (err) {
      log.err(`stream gagal · ${(err as Error).message}`, 'dlg:streams')
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  function pickStream(s: StreamRow) {
    play({
      // Backend sudah memberi slug pendek /s/<slug> (auto proxy + subtitle).
      src: s.url,
      title: meta?.name || s.name,
      sub: [s.quality || s.type, s.name].filter(Boolean).join(' · '),
      subs: s.subs,
    })
    onClose()
    setStatus(`Memutar ${s.quality || s.type} dari ${s.name}.`, 'ok')
  }

  function goBack() {
    if (view === 'streams') setView(tab === 'tv' && meta ? 'episodes' : 'results')
    else if (view === 'episodes') setView('results')
  }

  if (!open) return null

  const seasons = [...new Set((meta?.episodes || []).map((e) => e.season))].sort((a, b) => a - b)
  const eps = (meta?.episodes || []).filter((e) => e.season === season)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ background: 'rgba(9, 9, 11, .78)' }}
      onClick={onClose}>
      <div className="flex h-[min(720px,86vh)] w-[min(860px,94vw)] min-h-0 flex-col rounded-xl border border-border bg-zinc-950 p-0 text-zinc-200 shadow-xl shadow-black/50"
        onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <input
            id="search-q"
            autoComplete="off"
            spellCheck={false}
            placeholder="Cari judul, mis. frieren"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void doSearch(q) }}
            className="min-w-0 flex-1 rounded-lg border border-border bg-zinc-900 px-3 py-2 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-zinc-600"
          />
          <button type="button" onClick={() => void doSearch(q)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3.5 py-2 text-sm font-semibold text-primary-contrast transition hover:bg-zinc-200">
            <Search className="h-4 w-4 shrink-0" />Cari
          </button>
          <button type="button" onClick={onClose}
            className="rounded-lg border border-border p-2 text-zinc-400 transition hover:bg-zinc-800 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex flex-wrap items-center gap-1.5 border-b border-border px-4 py-2.5">
          {TABS.map((t) => (
            <Chip key={t.tab} label={t.label} on={tab === t.tab}
              onClick={() => { setTab(t.tab); setView('results'); setMeta(null); void doSearch(q, t.tab) }} />
          ))}
          <Chip label="Kembali" disabled={view === 'results'} onClick={goBack} />
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
          {busy ? <Empty>{busy}</Empty>
            : error && !rows.length ? <p className="p-6 text-center text-sm text-rose-300">{error}</p>
            : view === 'results' ? (
              results.length
                ? <ResultGrid results={results} onPick={(m) => void openTitle(m)} />
                : <Empty>Masukkan query lalu tekan Cari.</Empty>
            ) : view === 'episodes' ? (
              <div>
                <div className="flex flex-wrap gap-1.5">
                  {seasons.map((sn) => (
                    <Chip key={sn} label={sn === 0 ? 'Specials' : `S${sn}`} on={sn === season}
                      onClick={() => setSeason(sn)} />
                  ))}
                </div>
                <div className="mt-3 space-y-1.5">
                  {eps.length
                    ? eps.map((v) => (
                      <EpisodeRow key={`${v.season}:${v.episode}`} v={v}
                        onPick={() => void loadStreams(meta!, v)} />
                    ))
                    : <Empty>Season kosong.</Empty>}
                </div>
              </div>
            ) : rows.length ? (
              <div className="space-y-1.5">
                {rows.map((s) => (
                  <StreamRowView key={`${s.name}:${s.quality}:${s.url.slice(-20)}`} s={s} onPick={() => pickStream(s)} />
                ))}
              </div>
            ) : <Empty>Streams kosong.</Empty>}
        </div>
      </div>
    </div>
  )
}

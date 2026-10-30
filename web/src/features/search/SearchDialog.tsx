import { useState } from 'react'
import type { ReactNode } from 'react'
import { ArrowLeft, Search, X } from 'lucide-react'
import { jget } from '@/lib/api'
import { metaSeconds } from '@/lib/fmt'
import { Sq } from '@/features/text/Sq'
import { buildEpThumbs } from '@/features/files/FilesCard'
import { resetEpThumbs, setPickedThumb, setStoreThumbs, pickedThumbOf } from '@/features/files/thumbs'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { saveLast } from '@/features/magnet/last'
import type { StreamItem } from './addons'
import {
  AC, AC_FAMILIES, CINEMETA, FLAG, MUV, MUV_FAMILIES, TIO,
  parseStream, type StremioTab,
} from './addons'

/* Dialog pencarian: Cinemeta (judul/seri/episode) → streams Torrentio.
 *   - Tab All/Movie/Series memilih sumber Cinemeta.
 *   - Series: tab S1, S2… lalu daftar episode; movie langsung ke streams.
 *   - Streams: chips filter diturunkan dari teks stream-nya sendiri:
 *     provider (Torrentio/…), kualitas 480p…, bahasa dari emoji bendera,
 *     dan toggle HEVC. Klik stream = magnet infoHash-nya ke /api/magnet,
 *     dengan fileIndex bila Torrentio memberikannya (season pack).
 *   - Reset filter setiap ganti judul/episode. */
type Meta = Record<string, any>

const emptyView = (tab: StremioTab): View => ({
  tab, results: [], cur: null, videos: [], season: null,
  streams: [], qs: new Set(), ls: new Set(), provs: new Set(), hevc: false,
  playRuntime: 0,
})

type View = {
  tab: StremioTab
  results: Meta[]
  cur: Meta | null
  videos: Meta[]
  season: number | null
  streams: StreamItem[]
  qs: Set<string>
  ls: Set<string>
  provs: Set<string>
  hevc: boolean
  playRuntime: number
}

const TABS: Array<{ label: string; tab?: StremioTab; muv?: string; ac?: number }> = [
  { label: 'All', tab: 'all' },
  { label: 'Movie', tab: 'movie' },
  { label: 'Series', tab: 'series' },
  ...MUV_FAMILIES.map((f) => ({ label: f.label, muv: f.id })),
  ...AC_FAMILIES.map((f, i) => ({ label: f.label, ac: i })),
]

const cineSearch = (type: string, q: string) =>
  `${CINEMETA}/catalog/${type}/top/search=${encodeURIComponent(q)}.json`

const dedupe = (metas: Meta[]) => {
  const seen = new Set<string>()
  return metas.filter((m) => m.id && m.poster && !seen.has(m.id) && seen.add(m.id))
}

function Empty({ children }: { children: ReactNode }) {
  return <p className="p-6 text-center text-sm text-zinc-500">{children}</p>
}

function Chip({ label, on, disabled, onClick }: {
  label: ReactNode
  on?: boolean
  disabled?: boolean
  onClick: () => void
}) {
  return (
    <button type="button" disabled={disabled} onClick={onClick} className={`chip${on ? ' on' : ''}`}>
      {label}
    </button>
  )
}

function ResultGrid({ results, onPick }: { results: Meta[]; onPick: (m: Meta) => void }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(104px,1fr))] gap-3">
      {results.map((m) => (
        <button key={m.id} type="button" onClick={() => onPick(m)} className="group text-left transition hover:-translate-y-0.5">
          <span className="relative block">
            <img src={m.poster} alt={m.name || m.id} loading="lazy"
              className="aspect-[2/3] w-full rounded-lg border border-border bg-zinc-900 object-cover" />
            <span className="absolute left-1 top-1 z-10 rounded bg-black/80 px-1 text-[9px] font-medium text-zinc-300">
              {(m.type === 'anime' ? 'Anime' : m.type === 'series' ? 'Series' : 'Movie')
                + (m.releaseInfo ? ` · ${String(m.releaseInfo).split('–')[0]}` : '')}
            </span>
          </span>
          <Sq text={m.name || m.id} className="mt-1 block text-[11px] leading-tight text-zinc-300 group-hover:text-white" />
        </button>
      ))}
    </div>
  )
}

function EpisodeRow({ v, onPick }: { v: Meta; onPick: () => void }) {
  const label = v.name || v.title || ''
  return (
    <button type="button" onClick={onPick}
      className="file-row group flex w-full items-center gap-3 rounded-lg border border-border bg-zinc-900 px-3 py-2.5 text-left transition hover:bg-zinc-800">
      {v.thumbnail && (
        <img src={v.thumbnail} alt="" loading="lazy"
          className="h-11 w-20 shrink-0 rounded-md border border-border bg-zinc-800 object-cover"
          onError={(e) => (e.currentTarget.style.display = 'none')} />
      )}
      <span className="grid h-8 w-9 shrink-0 place-items-center rounded-md bg-zinc-800 text-[11px] font-semibold text-zinc-300">
        E{v.episode ?? '?'}
      </span>
      <Sq text={label} className="min-w-0 flex-1 text-sm text-zinc-300 group-hover:text-white" />
    </button>
  )
}

function StreamRow({ s, onPick }: { s: StreamItem; onPick: () => void }) {
  const rel = (s.raw.title || s.raw.name || '').split('\n')
    .filter((l) => l.trim() && !/👤|⚙️/.test(l))
    .map((l) => l.replace(FLAG, '').replace(/\/\s+(?=\/|$)/g, '').replace(/\s{2,}/g, ' ').trim())
    .filter(Boolean).join(' — ') || s.raw.name || ''
  return (
    <button type="button" onClick={onPick}
      className="group flex w-full flex-col rounded-lg border border-border bg-zinc-900 px-3 py-2.5 text-left transition hover:border-zinc-600 hover:bg-zinc-800">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 text-[11px]">
        <span className="font-medium text-zinc-300">{s.prov}</span>
        <span className="font-medium text-zinc-100">{s.q || 'lainnya'}</span>
        <span className="text-zinc-500">{[s.seed && '👤 ' + s.seed, s.size].filter(Boolean).join(' · ')}</span>
      </div>
      <p title={rel} className="mt-1 line-clamp-2 text-xs leading-snug text-zinc-300">{rel}</p>
      <p className="mt-1 text-[12px] tracking-wide">{s.flags.join(' · ')}</p>
    </button>
  )
}

const streamsFiltered = (view: View): StreamItem[] => view.streams.filter((s) =>
  (!view.provs.size || view.provs.has(s.prov))
  && (!view.qs.size || view.qs.has(s.q))
  && (!view.ls.size || s.flags.some((f) => view.ls.has(f)))
  && (!view.hevc || s.hevc))

export function SearchDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { loadMagnet, setStatus, selectFile, setDirect, unsetDirect } = useTorrent()
  const [tab, setTab] = useState<StremioTab>('all')
  const [q, setQ] = useState('')
  const [view, setView] = useState<View>(() => emptyView('all'))
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const patch = (p: Partial<View>) => setView((v) => ({ ...v, ...p }))

  const streaming = view.streams.length > 0
  const currentView: 'results' | 'episodes' | 'streams' = streaming ? 'streams' : view.cur ? 'episodes' : 'results'

  async function doSearch(query: string, useTab: StremioTab = tab) {
    setError('')
    setBusy('Mencari…')
    setView(emptyView(useTab))
    const types = useTab === 'all' ? ['series', 'movie'] : [useTab]
    try {
      const metas = (await Promise.allSettled(types.map((t) => jget(cineSearch(t, query)))))
        .flatMap((r) => (r.status === 'fulfilled' ? r.value.metas || [] : []))
      // Tab All bisa memuat satu judul dua kali (movie + series).
      patch({ tab: useTab, results: dedupe(metas) })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // Katalog Muvibox: gabungan katalog movie+series satu keluarga.
  // Trending meneruskan isi kotak pencarian; keluarga lain mengabaikannya.
  async function loadMuv(famId: string, query: string) {
    const fam = MUV_FAMILIES.find((f) => f.id === famId)
    if (!fam) return
    setError('')
    setBusy('Memuat katalog Muvibox…')
    try {
      const reqs = fam.types.map((t) => {
        const slug = fam.search && query ? `${fam.id}/search=${encodeURIComponent(query)}` : fam.id
        return jget(`${MUV}/catalog/${t}/mb_${t}s_${slug}.json`)
      })
      const metas = (await Promise.allSettled(reqs))
        .flatMap((r) => (r.status === 'fulfilled' ? r.value.metas || [] : []))
      setView({ ...emptyView(`mb:${famId}`), results: dedupe(metas) })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // Katalog Absolute Cinema: satu keluarga bisa berisi beberapa katalog.
  // Anime (Kitsu) tidak punya daftar episode → pilih rilis/batch saja.
  async function loadAc(famIdx: number) {
    const fam = AC_FAMILIES[famIdx]
    if (!fam) return
    setError('')
    setBusy('Memuat katalog Absolute Cinema…')
    try {
      const reqs = fam.cats.map((c) => jget(`${AC}/catalog/${c.t}/${c.c}.json`))
      const metas = (await Promise.allSettled(reqs))
        .flatMap((r) => (r.status === 'fulfilled' ? r.value.metas || [] : []))
      setView({ ...emptyView(`ac:${famIdx}`), results: dedupe(metas) })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  function pickTab(next: StremioTab) {
    setTab(next)
    if (next.startsWith('mb:')) { void loadMuv(next.slice(3), q.trim()); return }
    if (next.startsWith('ac:')) { void loadAc(Number(next.slice(3))); return }
    if (q.trim()) void doSearch(q.trim(), next)
  }

  function onSearch() {
    const query = q.trim()
    // Muvibox Trending mencari; keluarga lain + Absolute Cinema muat ulang saja.
    if (tab.startsWith('mb:')) { void loadMuv(tab.slice(3), query); return }
    if (tab.startsWith('ac:')) { void loadAc(Number(tab.slice(3))); return }
    if (query) void doSearch(query)
  }

  async function openTitle(m: Meta) {
    try {
      setError('')
      setBusy('Memuat detail…')
      resetEpThumbs() // judul baru: thumbnail episode lama tidak dipakai
      setPickedThumb('')
      if (m.type === 'movie' || m.type === 'anime') {
        // Anime Kitsu: metanya tanpa daftar episode → perlakukan seperti
        // movie: pilih rilis (biasanya batch/season), lalu episode dipilih
        // dari daftar file di sidebar setelah torrent termuat.
        if (m.type === 'anime') {
          setStatus('Kitsu tidak menyediakan daftar episode: pilih rilis batch/season, lalu pilih file episode dari sidebar.', 'ok')
        }
        patch({ cur: m, videos: [], streams: [], playRuntime: metaSeconds(m.runtime) })
        setBusy('')
        void loadStreams(m.id, m.type)
        return
      }
      // Meta `mb:` hanya ada di addon Muvibox, selebihnya Cinemeta.
      const metaUrl = m.id.startsWith('mb:')
        ? `${MUV}/meta/series/${encodeURIComponent(m.id)}.json`
        : `${CINEMETA}/meta/series/${m.id}.json`
      const meta = await jget(metaUrl)
      const videos = (meta.meta?.videos || []).filter((v: Meta) => v.season != null)
      patch({ cur: m, videos, season: videos[0]?.season ?? 1, streams: [], playRuntime: 0 })
      setBusy('')
    } catch (err) {
      setBusy('')
      setError((err as Error).message)
    }
  }

  async function loadStreams(id: string, type: string) {
    setError('')
    setBusy('Mengambil streams…')
    try {
      const enc = encodeURIComponent(id)
      // Stream hanya dari Torrentio.
      const res = await jget(`${TIO}/stream/${type}/${enc}.json`)
      const pools = [res.streams || []]
      // Torrentio dedupe infoHash, stream direct dedupe url.
      const seen = new Set<string>()
      const streams = pools.flat()
        .filter((raw: Meta) => {
          const key = raw.infoHash ? 'h:' + raw.infoHash : 'u:' + (raw.url || raw.externalUrl || '')
          if (!key || seen.has(key)) return false
          seen.add(key)
          return true
        })
        .map(parseStream)
        .sort((a, b) => b.seedNum - a.seedNum) // seed terbanyak di atas
      patch({ streams, qs: new Set(), ls: new Set(), provs: new Set(), hevc: false })
      setBusy('')
    } catch (err) {
      setBusy('')
      setError((err as Error).message)
    }
  }

  async function playStream(s: StreamItem) {
    const raw = s.raw
    if (!raw.infoHash) {
      if (raw.url) { playDirect(s); return }
      if (raw.externalUrl) { window.open(raw.externalUrl, '_blank', 'noopener'); return }
      setStatus('Stream ini bukan torrent murni (debrid), tidak bisa dimuat.', 'error')
      return
    }
    setBusy('Memuat torrent… (metadata bisa sampai 45 detik)')
    const trs = (raw.sources || []).filter((x) => x.startsWith('tracker:'))
      .map((x) => '&tr=' + encodeURIComponent(x.slice(8))).join('')
    const dn = raw.title?.split('\n')[0].slice(0, 180) || raw.name || ''
    const magnet = `magnet:?xt=urn:btih:${raw.infoHash}&dn=${encodeURIComponent(dn)}${trs}`
    const body: { magnet: string; fileIndex?: number; runtime?: number } = { magnet }
    if (Number.isInteger(raw.fileIdx)) body.fileIndex = raw.fileIdx
    if (view.playRuntime) body.runtime = view.playRuntime // durasi dari metadata API
    try {
      const state = await loadMagnet(body)
      onClose()
      // thumbnail episode didefinisikan dari metadata judul (Simkl episode).
      const thumbs = buildEpThumbs(view.videos) as Record<string, string>
      setStoreThumbs(thumbs)
      const files = state.files || []
      const pick = files[state.selected ?? 0] || files[0]
      saveLast({ magnet, index: pick ? pick.index : 0, thumb: pickedThumbOf(), thumbs })
      if (pick) void selectFile(pick.index)
      setStatus(`Torrent siap: ${state.name || 'tanpa nama'}.`, 'ok')
      setBusy('')
    } catch (err) {
      setBusy('')
      // Muvibox dan Torrentio: gagal memuat → jangan tutup dialog.
      setStatus((err as Error).message, 'error')
    }
  }

  // Putar stream direct MP4 via proxy RAM `/fetch` (bukan torrent).
  // URL bungkus Muvibox (`player.muvibox...?url=<inner mp4>`) dibuka dulu
  // untuk mengambil URL video aslinya; token di dalamnya terbatas waktu
  // (Time-limited), jadi setiap putar selalu ambil daftar stream baru.
  function playDirect(s: StreamItem) {
    const raw = s.raw
    let link: string
    try {
      link = raw.url!
      const u = new URL(link)
      // Halaman bungkus bukan file media: ambil isi parameter `url=`.
      if (!/\.(mp4|mkv|webm|m3u8|avi|mov)([?#]|$)/i.test(u.pathname)) {
        const inner = u.searchParams.get('url')
        if (!inner) {
          setStatus('URL bungkus tidak berisi video yang bisa dimainkan.', 'error')
          return
        }
        link = inner
      }
    } catch {
      setStatus('URL stream tidak valid.', 'error')
      return
    }
    unsetDirect()
    setDirect(`/fetch?url=${encodeURIComponent(link)}`, {
      title: raw.title?.split('\n')[0] || s.prov,
      sub: `${s.prov}${s.q ? ' · ' + s.q : ''} · direct (bukan torrent)`,
    })
    onClose()
    setStatus('Memutar langsung lewat proxy RAM /fetch — tidak diunduh ke disk.', 'ok')
  }

  // Kembali: streams → episode/movie, episode → daftar hasil.
  function goBack() {
    if (currentView === 'streams') {
      patch({ streams: [] })
      if (view.cur?.type !== 'series') patch({ cur: null, videos: [] })
    } else if (currentView === 'episodes') {
      patch({ cur: null, videos: [] })
    }
  }

  if (!open) return null

  const seasons = [...new Set(view.videos.map((v) => v.season))].sort((a, b) => +a - +b)
  const eps = view.videos.filter((v) => v.season === view.season)
    .sort((a, b) => (a.episode ?? 0) - (b.episode ?? 0))
  const filtered = streamsFiltered(view)

  const provChips = [...new Set(view.streams.map((s) => s.prov))]
  const qualChips = [...new Set(view.streams.map((s) => s.q).filter(Boolean))]
    .sort((a, b) => parseInt(b) - parseInt(a))
  const hevcCount = view.streams.filter((s) => s.hevc).length

  const toggleSet = (set: Set<string>, value: string) => {
    const next = new Set(set)
    if (next.has(value)) next.delete(value)
    else next.add(value)
    return next
  }
  // Bahasa: Indonesia dulu, lalu Inggris, lalu sisanya dari yang paling banyak.
  const langs: Record<string, number> = {}
  for (const s of view.streams) for (const f of s.flags) langs[f] = (langs[f] || 0) + 1
  const langChips = Object.entries(langs)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .sort((a, b) => (a[0] === 'Indonesia' ? -1 : b[0] === 'Indonesia' ? 1 : a[0] === 'Inggris' ? -1 : b[0] === 'Inggris' ? 1 : 0))

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ background: 'rgba(9, 9, 11, .78)' }}
      onClick={onClose}
    >
      <div
        className="flex h-[min(720px,86vh)] w-[min(860px,94vw)] min-h-0 flex-col rounded-xl border border-border bg-zinc-950 p-0 text-zinc-200 shadow-xl shadow-black/50"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <input
            id="search-q"
            autoComplete="off"
            spellCheck={false}
            placeholder="Cari judul, mis. frieren"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') onSearch() }}
            className="min-w-0 flex-1 rounded-lg border border-border bg-zinc-900 px-3 py-2 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-zinc-600"
          />
          <button type="button" onClick={onSearch}
            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3.5 py-2 text-sm font-semibold text-primary-contrast transition hover:bg-zinc-200">
            <Search className="h-4 w-4 shrink-0" />
            Cari
          </button>
          <button type="button" onClick={onClose}
            className="rounded-lg border border-border p-2 text-zinc-400 transition hover:bg-zinc-800 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex flex-wrap items-center gap-1.5 border-b border-border px-4 py-2.5">
          {TABS.map((t) => {
            const id = t.tab ?? (t.muv ? `mb:${t.muv}` : `ac:${t.ac}`)
            return (
              <Chip
                key={id}
                label={t.label}
                on={tab === id}
                onClick={() => id && pickTab(id as StremioTab)}
              />
            )
          })}
          <Chip
            label={<span className="inline-flex items-center gap-1"><ArrowLeft className="h-3.5 w-3.5" />Kembali</span>}
            disabled={currentView === 'results'}
            onClick={goBack}
          />
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
          {busy
            ? <Empty>{busy}</Empty>
            : error
              ? <p className="p-6 text-center text-sm text-rose-300">{error}</p>
              : currentView === 'results'
                ? view.results.length
                  ? <ResultGrid results={view.results} onPick={(m) => void openTitle(m)} />
                  : <Empty>Masukkan query lalu tekan Cari, atau pilih salah satu katalog.</Empty>
                : currentView === 'episodes'
                  ? (
                    <div>
                      <div className="flex flex-wrap gap-1.5">
                        {seasons.map((s) => (
                          <Chip key={String(s)} label={s === 0 ? 'Specials' : `S${s}`} on={s === view.season}
                            onClick={() => patch({ season: s })} />
                        ))}
                      </div>
                      <div className="mt-3 space-y-1.5">
                        {eps.length
                          ? eps.map((v) => (
                            <EpisodeRow
                              key={String(v.id || `${view.cur?.id}:${v.season}:${v.episode}`)}
                              v={v}
                              onPick={() => {
                                setPickedThumb(v.thumbnail || '')
                                patch({ playRuntime: metaSeconds(v.runtime) })
                                void loadStreams(v.id || `${view.cur?.id}:${v.season}:${v.episode}`, 'series')
                              }}
                            />
                          ))
                          : <Empty>Katalog kosong.</Empty>}
                      </div>
                    </div>
                  )
                  : (
                    <div>
                      <div className="space-y-2">
                        <div className="flex flex-wrap gap-1.5">
                          <Chip label={<span className="font-medium">Streams ({filtered.length}/{view.streams.length})</span>} on={false} onClick={() => {}} />
                          {provChips.map((p) => (
                            <Chip key={p} label={p} on={view.provs.has(p)} onClick={() => patch({ provs: toggleSet(view.provs, p) })} />
                          ))}
                        </div>
                        <div className="flex flex-wrap gap-1.5">
                          {qualChips.map((quality) => (
                            <Chip key={quality} label={`${quality}p`} on={view.qs.has(quality)} onClick={() => patch({ qs: toggleSet(view.qs, quality) })} />
                          ))}
                          {hevcCount > 0 && (
                            <Chip label={`HEVC (${hevcCount})`} on={view.hevc} onClick={() => patch({ hevc: !view.hevc })} />
                          )}
                        </div>
                        <div className="flex flex-wrap gap-1.5">
                          {langChips.map(([f, n]) => (
                            <Chip key={f} label={`${f} ${n}`} on={view.ls.has(f)} onClick={() => patch({ ls: toggleSet(view.ls, f) })} />
                          ))}
                        </div>
                      </div>
                      <div className="mt-3 space-y-1.5">
                        {view.streams.length && !filtered.length
                          ? <Empty>Tidak ada stream yang lolos filter.</Empty>
                          : filtered.map((s) => (
                            <StreamRow key={s.raw.infoHash || s.raw.url || s.prov} s={s} onPick={() => void playStream(s)} />
                          ))}
                      </div>
                    </div>
                  )}
        </div>
      </div>
    </div>
  )
}

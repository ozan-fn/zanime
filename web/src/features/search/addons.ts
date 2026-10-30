// Konfigurasi addon Stremio yang dipakai kotak pencarian. Ubah alamat addon /
// daftar katalog di sini, tanpa menyentuh komponennya.

export const AC = 'https://cinema.debrid.in'
// Addon Stremio Absolute Cinema: katalog anime (Kitsu/AniList, tanpa
// daftar episode — pilih rilis batch/season); stream-nya torrent (infoHash),
// jadi lariku dengan alur Torrentio. Netflix/Top katalog TMDB → id `tt`,
// meta lewat Cinemeta.
export const AC_FAMILIES = [
  { label: '🗼Anime Pop', cats: [{ t: 'anime', c: 'kitsu_popular' }] },
  { label: '🗼Anime Top', cats: [{ t: 'anime', c: 'kitsu_top_rated' }] },
  { label: '🗼Anime Airing', cats: [{ t: 'anime', c: 'kitsu_current' }] },
  { label: '🗼AniList', cats: [{ t: 'anime', c: 'anilist_trending' }] },
  { label: 'Netflix', cats: [{ t: 'movie', c: 'tmdb_netflix_movie_us' }, { t: 'series', c: 'tmdb_netflix_series_us' }] },
  { label: 'Top Rated', cats: [{ t: 'movie', c: 'tmdb_discover_top_movie' }, { t: 'series', c: 'tmdb_discover_top_series' }] },
]

export const TIO = 'https://torrentio.strem.fun/sort=seeders|language=indonesian'
export const CINEMETA = 'https://v3-cinemeta.strem.io'

// Addon Stremio Muvibox: katalog film/serial (Trending dukung `search=`,
// sisanya browse saja) + stream direct MP4 (bukan torrent).
export const MUV = 'https://muvibox.inulpc.web.id'
// Keluarga katalog: id memetakan ke `mb_{movie|series}s_{id}.json`.
export const MUV_FAMILIES = [
  { id: 'trending', label: '🔥Trending', types: ['movie', 'series'], search: true },
  { id: 'indo', label: '🇮🇩Indo', types: ['movie', 'series'] },
  { id: 'korea', label: 'Korea', types: ['movie', 'series'] },
  { id: 'china', label: 'China', types: ['movie', 'series'] },
  { id: 'india', label: 'India', types: ['movie', 'series'] },
  { id: 'horror', label: 'Horror', types: ['movie'] },
  { id: 'anime', label: 'Anime', types: ['movie', 'series'] },
  { id: 'comedy', label: 'Comedy', types: ['movie', 'series'] },
  { id: 'romance', label: 'Romance', types: ['movie', 'series'] },
  { id: 'action', label: 'Action', types: ['movie', 'series'] },
]

export type StremioTab = 'all' | 'movie' | 'series' | `mb:${string}` | `ac:${string}`

// Flag emoji → kode 2 huruf → nama bahasa. Windows tidak punya font bendera,
// jadi emoji flag tampil sebagai "ID"/"GB" — dipetakan biar jelas.
const LANG_NAMES: Record<string, string> = {
  id: 'Indonesia', en: 'Inggris', gb: 'Inggris', us: 'Inggris',
  ru: 'Rusia', it: 'Italia', pt: 'Portugis', br: 'Portugis', es: 'Spanyol', mx: 'Latino',
  cn: 'Mandarin', tw: 'Mandarin', hk: 'Kanton', fr: 'Prancis', de: 'Jerman', sa: 'Arab',
  my: 'Melayu', th: 'Thai', vn: 'Vietnam', nl: 'Belanda', jp: 'Jepang', kr: 'Korea',
  in: 'Hindi', pk: 'Urdu', tr: 'Turki', pl: 'Polandia', dk: 'Denmark', se: 'Swedia',
  no: 'Norwegia', fi: 'Finlandia', il: 'Ibrani', gr: 'Yunani', ro: 'Rumania',
  hu: 'Hongaria', cz: 'Ceko', ph: 'Filipina',
}

export const FLAG = /\p{Regional_Indicator}{2}/gu

export function flagName(f: string): string {
  const cp = [...f]
  const code = (String.fromCodePoint(cp[0].codePointAt(0)! - 127397)
    + String.fromCodePoint(cp[1].codePointAt(0)! - 127397)).toLowerCase()
  return LANG_NAMES[code] || code.toUpperCase()
}

// "1.4K" / "980" di sebelah 👤 diubah ke angka untuk pengurutan.
export function parseCount(s?: string): number {
  const m = (s || '').match(/[\d.,]+/)
  if (!m) return -1
  let n = parseFloat(m[0].replace(/,/g, ''))
  if (/K$/i.test(s!)) n *= 1e3
  else if (/M$/i.test(s!)) n *= 1e6
  return n
}

export type StreamItem = {
  raw: { infoHash?: string; url?: string; externalUrl?: string; name?: string; title?: string; fileIdx?: number; sources?: string[] }
  prov: string
  q: string
  seed?: string
  seedNum: number
  size?: string
  flags: string[]
  hevc: boolean
}

export function parseStream(s: Record<string, any>): StreamItem {
  const text = (s.name || '') + ' ' + (s.title || '')
  const q = (text.match(/\b(\d{3,4})p\b/i) || [])[1]?.toLowerCase() || ''
  const t: string = s.title || ''
  const prov = (t.match(/⚙️\s*([^\n]+)/)?.[1]?.trim())
    || (s.name?.split('\n')[0].trim() || '') || 'Torrentio'
  const seed = (t.match(/👤\s*([\d.,]+[KkMm]?)/) || [''])[1]
  const size = (t.match(/💾\s*([\d.,]+\s*[KMG]?B)/i) || [''])[1]
  const flags = [...new Set((t.match(FLAG) || []).map(flagName))]
  return {
    raw: s as StreamItem['raw'], prov, q,
    seed, seedNum: parseCount(seed), size, flags,
    hevc: /hevc|h\.?265|x265/i.test(text),
  }
}

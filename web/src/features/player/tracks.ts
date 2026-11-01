import type { Sub } from '@/lib/types'

export type SubTrack = {
  src: string
  kind: 'subtitles'
  label: string
  lang: string
  default: boolean
}

// URL subtitle yang aman dimuat player: path same-origin (`/s/…` hasil proxy)
// atau http(s) absolut. Skema lain (data:, javascript:, blob:) ditolak.
const safeURL = (url: string) => /^(\/|https?:\/\/)/i.test(url)

// Pilihan subtitle otomatis: Indonesia dulu, lalu Inggris.
const ID_RE = /\b(id|ind)\b|indonesia/i
const EN_RE = /\b(en|eng)\b|english/i

/* subTracks menyiapkan track subtitle untuk <Track>: URL wajib aman, lang
 * wajib ada (dipakai menu subtitle), duplikat lang dibuang, dan satu track
 * dipilih otomatis — Indonesia, fallback Inggris; tanpa keduanya tidak ada
 * yang aktif. */
export function subTracks(subs: Sub[] = []): SubTrack[] {
  const tracks: SubTrack[] = []
  const langs = new Set<string>()
  for (const s of subs) {
    if (!s?.url || !s.lang || !safeURL(s.url) || langs.has(s.lang)) continue
    langs.add(s.lang)
    tracks.push({
      src: s.url,
      kind: 'subtitles',
      label: s.label || s.lang,
      lang: s.lang,
      default: false,
    })
  }
  const pick = (re: RegExp) => tracks.find((t) => re.test(`${t.lang} ${t.label}`))
  const auto = pick(ID_RE) || pick(EN_RE)
  if (auto) auto.default = true
  return tracks
}

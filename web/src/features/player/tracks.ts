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

/* toVtt: <track> hanya menerima WebVTT. File .vtt dilewatkan apa adanya
 * (BOM dibuang, CRLF dinormalkan); .srt diberi header WEBVTT dan koma pada
 * baris waktu diganti titik. Baris teks cue tidak disentuh. */
export function toVtt(text: string): string {
  const body = text.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n')
  if (body.trimStart().startsWith('WEBVTT')) return body
  const cues = body
    .split('\n')
    .map((line) => (line.includes('-->') ? line.replace(/,/g, '.') : line))
    .join('\n')
    .trim()
  return `WEBVTT\n\n${cues}\n`
}

// cueCount menghitung baris waktu VTT (00:00:01.000 --> …), dipakai untuk
// memastikan file kiriman pengguna benar-benar berisi cue.
export function cueCount(vtt: string): number {
  return (vtt.match(/^\s*\d{2}:\d{2}:\d{2}[.,]\d{3}\s*-->/gm) || []).length
}

export type Cue = { startTime: number; endTime: number }

/* shiftCues menggeser waktu cue sebesar `delay` detik (positif = subtitle
 * tampil lebih lambat). Waktu asli tiap cue disimpan di `base`, jadi geseran
 * idempoten: nilai bisa dinaikkan-turunkan berulang tanpa menumpuk, dan cue
 * baru dari track yang baru dimuat ikut mengenal delay yang sedang aktif.
 * Waktu mulai tidak pernah negatif. */
export function shiftCues(cues: readonly Cue[], delay: number, base: WeakMap<object, Cue>): void {
  for (const cue of cues) {
    let orig = base.get(cue)
    if (!orig) {
      orig = { startTime: cue.startTime, endTime: cue.endTime }
      base.set(cue, orig)
    }
    const start = Math.max(0, orig.startTime + delay)
    cue.startTime = start
    cue.endTime = Math.max(start, orig.endTime + delay)
  }
}

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

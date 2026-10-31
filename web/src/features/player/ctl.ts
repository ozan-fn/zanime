/* Kontrol player di luar React: elemen movi-player diakses imperatif. */
import type { Sub } from '@/lib/types'
import { log } from '@/lib/logs'

export const ctl = {
  el: null as any,
  wanted: '', // src yang terakhir dipasang; dedupe antar mount
  resumeStartAt: 0, // posisi lanjutan (detik) untuk pemutaran ulang
  retries: 0,
  lastRetry: 0,
}

// Elemen kustom: hapus shim property `src` milik modul sebelum menetapkan,
// kalau tidak setter bawaan custom element menimpa URL.
export function setPlayerSrc(el: any, url: string) {
  if (Object.prototype.hasOwnProperty.call(el, 'src')) delete el.src
  el.src = url
}

// Pasang sumber + track subtitle sekaligus lewat API source(). Elemen
// menyimpan daftar subtitle SEBELUM `src` dipasang, jadi player yang baru
// dibuat langsung memuat daftar itu — tidak ada balapan dengan `el.player`
// yang baru ada setelah load. movi-player menggambar subtitle sendiri lewat
// canvas/WASM, jadi <track> anak tidak dipakai.
export function setPlayerSource(el: any, url: string, subs?: Sub[]) {
  if (Object.prototype.hasOwnProperty.call(el, 'src')) delete el.src
  // Elemen yang belum ter-upgrade belum punya source(); jatuh ke src biasa.
  if (typeof el.source !== 'function') {
    el.src = url
    return
  }
  el.source({
    video: { src: url },
    subtitles: (subs || [])
      .filter((s) => s.url)
      .map((s) => ({
        src: s.url,
        lang: s.lang,
        label: s.label || s.lang,
        format: 'vtt',
      })),
  })
}

// Buang playback sekarang; mount berikutnya memuat ulang src baru.
export function resetPlayback() {
  ctl.wanted = ''
  ctl.resumeStartAt = 0
  const el = ctl.el
  if (el) {
    el.removeAttribute('startat')
    el.removeAttribute('src')
  }
}

// Nyalakan track pertama begitu player hasil load baru siap, tapi hanya bila
// belum ada subtitle aktif (mis. pilihan tersimpan dari `persist` sudah
// dipegang player). `before` = instance player lama, supaya kita tidak
// menyeleksi di player yang sudah akan dibuang. Mengembalikan true bila sudah
// selesai (dipilih atau memang tidak perlu).
export function selectFirstSub(el: any, subs: Sub[] | undefined, before?: unknown): boolean {
  const player = el?.player
  if (!player || player === before || !subs?.length) return false
  const langs = player.getSubtitleLangs?.() || []
  if (!langs.length) return false
  if (!langs.some((t: { active?: boolean }) => t.active)) {
    void el.selectSubtitleLang?.(subs[0].lang)
    log.ok(`subtitle aktif · ${subs[0].label || subs[0].lang}`, 'player:sub')
  }
  return true
}

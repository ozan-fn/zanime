/* Kontrol player di luar React: elemen movi-player diakses imperatif. */
import type { Sub } from '@/lib/types'

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

// Subtitle embedmaster didaftarkan lewat API player: movi-player menggambar
// sendiri lewat canvas/WASM, jadi <track> anak elemen diabaikan begitu `src`
// terpasang dan tidak ada <video> untuk ditempeli. `el.player` baru ada
// setelah loadeddata — pemanggil menunggu sampai helper ini balas true.
export function attachSubs(el: any, subs?: Sub[]): boolean {
  const player = el?.player
  if (!player || !subs?.length) return false
  for (const s of subs) {
    player.addSubtitleTrack({ url: s.url, lang: s.lang, label: s.label || s.lang, format: 'vtt' })
  }
  el.updateSubtitleTrackMenu?.()
  el.refreshOpenContextMenu?.()
  // Track pertama langsung diaktifkan (baris stream sudah menulis ada N subtitle).
  void el.selectSubtitleLang?.(subs[0].lang)
  return true
}

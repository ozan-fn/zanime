/* Kontrol player di luar React: elemen movi-player diakses imperatif. */
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

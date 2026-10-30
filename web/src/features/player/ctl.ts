/* Kontrol player di luar React: elemen movi-player diakses imperatif
 * (src/startat/currentTime), dan header (audio-rate) perlu menyentuh
 * playlist + posisi putar tanpa prop drilling ke Player. */
export const ctl = {
  el: null as any,
  wanted: '', // src yang terakhir dipasang; dedupe antar push status
  subsSession: '',
  resumeStartAt: 0, // posisi lanjutan (detik) untuk pemutaran ulang
  retries: 0,
  lastRetry: 0,
  lastSelected: null as number | null, // file terpilih terakhir (ganti → reset)
}

// Elemen kustom: hapus shim property `src` milik modul sebelum menetapkan,
// kalau tidak setter bawaan custom element menimpa URL.
export function setPlayerSrc(el: any, url: string) {
  if (Object.prototype.hasOwnProperty.call(el, 'src')) delete el.src
  el.src = url
}

// Buang playback sekarang; siklus status berikutnya memuat ulang begitu
// sesi HLS baru siap (dipakai saat ganti file / mulai torrent baru).
export function resetPlayback() {
  ctl.wanted = ''
  ctl.subsSession = ''
  ctl.resumeStartAt = 0
  const el = ctl.el
  if (el) {
    el.removeAttribute('startat')
    el.removeAttribute('src')
  }
}

export function resumeSeconds(): number {
  return Math.max(0, Math.floor(ctl.el?.currentTime || 0))
}

// Paksa playlist yang sama dimuat ulang (password: audio-rate).
export function invalidatePlaylist() {
  ctl.wanted = ''
}

export function applyResume(at: number) {
  const from = at > 1
  ctl.resumeStartAt = from ? at : 0
  if (from) ctl.el?.setAttribute('startat', String(at))
}

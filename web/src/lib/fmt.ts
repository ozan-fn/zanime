export function fmt(n?: number | null): string {
  if (!n || n < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  const digits = i === 0 || v >= 10 ? 0 : 1
  return `${v.toFixed(digits)} ${units[i]}`
}

export function fmtDur(sec?: number | null): string {
  const s = Math.max(0, Math.round(sec || 0))
  if (s < 60) return `${s} dtk`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`
  return `${Math.floor(m / 60)}j ${String(m % 60).padStart(2, '0')}m`
}

// Durasi dari metadata addon (menit untuk movie, detik untuk episode).
// Durasi perkiraan; mkvgo tetap membaca durasi sebenarnya dari header file.
export function metaSeconds(raw?: string): number {
  const n = parseFloat(raw || '')
  if (!Number.isFinite(n) || n <= 0) return 0
  return Math.round(n > 300 ? n : n * 60)
}

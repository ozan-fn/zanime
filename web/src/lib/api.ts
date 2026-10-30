import type { TorrentState } from './types'

// api(): semua POST/poll JSON; error dibacakan dari body.error bila ada.
export async function api<T = TorrentState>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  const text = await res.text()
  let body = {}
  try { body = text ? JSON.parse(text) : {} } catch { /* non-JSON error */ }
  if (!res.ok) throw new Error((body as { error?: string }).error || `HTTP ${res.status}`)
  return body as T
}

/* Katalog Cinemeta, Absolute Cinema, Muvibox, dan streams Torrentio diambil
 * lewat /api/addon (server). Browser hanya bicara same-origin, jadi tidak ada
 * preflight CORS dan rate limit addon menempel ke IP server, bukan IP tiap
 * penonton. */
export async function jget<T = any>(url: string): Promise<T> {
  const res = await fetch(`/api/addon?url=${encodeURIComponent(url)}`)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`)
  return body
}

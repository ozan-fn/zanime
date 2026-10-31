/* Log realtime di luar React: baris datang dari server lewat websocket
 * /api/logs, ditambah baris lokal dari player/dialog. Store kecil + langganan
 * (useSyncExternalStore) supaya komponen apa pun bisa menampilkannya tanpa
 * context. Satu baris dengan `id` yang sama menggantikan baris sebelumnya,
 * jadi langkah yang sama bisa tampil "[sedang…]" lalu berubah "[OK]". */
import { useSyncExternalStore } from 'react'

export type Level = 'info' | 'ok' | 'err'
export type LogEntry = { id: string; at: string; level: Level; text: string }

const CAP = 400 // batas baris di memori (ring buffer)
let entries: LogEntry[] = []
let seq = 0
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

function clock(): string {
  const d = new Date()
  const p = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
}

// push menambah baris; `id` non-kosong menggantikan baris dengan id sama.
export function push(level: Level, text: string, id = '', at = clock()): void {
  const key = id || `#${++seq}`
  const i = entries.findIndex((e) => e.id === key)
  if (i >= 0) {
    const next = entries.slice()
    next[i] = { id: key, at, level, text }
    entries = next
  } else {
    entries = [...entries, { id: key, at, level, text }]
  }
  if (entries.length > CAP) entries = entries.slice(entries.length - CAP)
  emit()
}

// pushEvent menerima satu event JSON dari server dan menormalkannya.
export function pushEvent(ev: { id?: string; at?: string; level?: string; text?: string }): void {
  const level: Level = ev.level === 'ok' || ev.level === 'err' ? ev.level : 'info'
  push(level, ev.text || '', ev.id || '', ev.at || clock())
}

export const log = {
  info: (text: string, id = '') => push('info', text, id),
  ok: (text: string, id = '') => push('ok', text, id),
  err: (text: string, id = '') => push('err', text, id),
}

export function clearLogs(): void {
  entries = []
  emit()
}

export function getLogs(): LogEntry[] {
  return entries
}

export function subscribeLogs(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function useLogs(): LogEntry[] {
  return useSyncExternalStore(subscribeLogs, getLogs, getLogs)
}

// ---- koneksi websocket ----

let socket: WebSocket | null = null
let retry: number | undefined

// connectLogs membuka /api/logs dan menyiarkan baris server ke store. Aman
// dipanggil berkali-kali; putus → coba lagi. Diabaikan bila WebSocket tak ada
// (mis. test tanpa jsdom WebSocket).
export function connectLogs(): void {
  if (typeof WebSocket === 'undefined' || typeof location === 'undefined' || socket) return
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const ws = new WebSocket(`${proto}://${location.host}/api/logs`)
  socket = ws
  ws.onmessage = (e) => {
    try {
      pushEvent(JSON.parse(String(e.data)))
    } catch {
      /* baris rusak: abaikan */
    }
  }
  ws.onclose = () => {
    socket = null
    retry = window.setTimeout(connectLogs, 1500)
  }
  ws.onerror = () => ws.close()
}

// disconnectLogs menutup koneksi (dipakai test / unmount).
export function disconnectLogs(): void {
  if (retry !== undefined) window.clearTimeout(retry)
  retry = undefined
  const ws = socket
  socket = null
  if (ws) {
    ws.onclose = null
    ws.close()
  }
}

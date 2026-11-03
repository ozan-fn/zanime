import { useCallback, useEffect, useRef, useState } from 'react'
import { Captions, ChevronDown, ChevronUp, Languages, RefreshCw } from 'lucide-react'
import { api } from '@/lib/api'
import { log } from '@/lib/logs'
import type { Sub, Subtitle } from '@/lib/types'

/* Kartu pencarian subtitle di bawah panel server. Sumbernya backend
 * `/api/subtitles` yang mencari ulang ke subdl tiap kali dipanggil — jadi
 * tautan unduh yang dipakai adalah hasil resolve halaman terbaru, bukan cache
 * lama. Subtitle subdl hanya dipasang saat diklik ("pakai").
 *
 * Subtitle bawaan semua server stream langsung tampil (bertanda nama server),
 * tanpa perlu memutar server-nya lebih dulu. Subtitle Inggris punya tombol
 * terjemahkan: permintaan dikirim ke server
 * (`/api/translate`), hasilnya ditandai "hasil translate" dan di-cache di
 * server sehingga kembali seketika saat diminta lagi. */

export type SubtitleMedia = {
  tab: 'movie' | 'tv'
  id: string
  name: string
  year?: string
  season?: number
  episode?: number
}

// Subtitle bawaan satu server stream (dari embedmaster/miruro).
export type ServerSubs = { name: string; subs: Sub[] }

type TranslateState = {
  busy: boolean
  error?: string
  url?: string
  cues?: number
  cached?: boolean
}

// Sama dengan regex player: subtitle Inggris dikenali dari lang/label.
const EN_RE = /\b(en|eng)\b|english/i
const ID_RE = /\b(id|ind)\b|indonesia/i

// Urutan tampil: Indonesia dulu, Inggris sesudahnya; bahasa lain dibuang.
const rank = (s: string) => (ID_RE.test(s) ? 0 : EN_RE.test(s) ? 1 : 2)

export function SubtitleCard({
  media,
  servers = [],
  onUse,
  onOpenChange,
}: {
  media?: SubtitleMedia
  servers?: ServerSubs[]
  onUse: (src: string, label: string) => void
  /** Diberitahu setiap kali kartu dibuka/ditutup (dipakai layout desktop). */
  onOpenChange?: (open: boolean) => void
}) {
  const [items, setItems] = useState<Subtitle[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(true)
  const [tr, setTr] = useState<Record<string, TranslateState>>({})
  // Permintaan terjemahan yang sedang jalan, per URL subtitle — dipakai tombol
  // batal dan pembersihan saat ganti server.
  const inflight = useRef<Record<string, AbortController>>({})

  // Ganti judul/episode = subtitle server dan hasil terjemahannya tidak relevan
  // lagi; terjemahan yang masih jalan ikut dibatalkan. Hasil yang pernah dibuat
  // tetap ada di cache server dan kembali seketika kalau diminta.
  const content = media ? `${media.tab}:${media.id}:${media.season ?? ''}:${media.episode ?? ''}` : ''
  useEffect(() => {
    for (const c of Object.values(inflight.current)) c.abort()
    inflight.current = {}
    setTr({})
  }, [content])

  // Subtitle semua server, digabung dan dedupe per URL (server berbeda sering
  // membawa track yang sama), lalu disaring ke Indonesia/Inggris dan diurutkan.
  const streamSubs: { server: string; sub: Sub }[] = []
  const seen = new Set<string>()
  for (const srv of servers) {
    for (const sub of srv.subs || []) {
      if (!sub?.url || seen.has(sub.url)) continue
      seen.add(sub.url)
      if (rank(`${sub.lang} ${sub.label}`) < 2) streamSubs.push({ server: srv.name, sub })
    }
  }
  streamSubs.sort((a, b) => rank(`${a.sub.lang} ${a.sub.label}`) - rank(`${b.sub.lang} ${b.sub.label}`))

  // Hasil subdl juga disaring dan diurutkan dengan aturan yang sama.
  const searchSubs = items
    .filter((s) => rank(`${s.lang} ${s.language} ${s.name}`) < 2)
    .sort((a, b) => rank(`${a.lang} ${a.language} ${a.name}`) - rank(`${b.lang} ${b.language} ${b.name}`))

  const load = useCallback(async (signal?: AbortSignal, refresh = false) => {
    if (!media) return
    setBusy(true)
    setError('')
    const what = media.tab === 'tv' ? `${media.name} S${media.season}E${media.episode}` : media.name
    log.info(`Mencari subtitle "${what}"…`, 'sub:search')
    try {
      let url = `/api/subtitles?tab=${media.tab}&id=${encodeURIComponent(media.id)}`
      if (media.tab === 'tv') url += `&s=${media.season}&e=${media.episode}`
      if (refresh) url += '&refresh=1'
      const body = await api<{ subtitles: Subtitle[] }>(url, signal)
      if (signal?.aborted) return
      const subs = body.subtitles || []
      setItems(subs)
      log.ok(`Subtitle siap · ${subs.length} pilihan`, 'sub:search')
    } catch (err) {
      if (signal?.aborted) return
      setItems([])
      setError((err as Error).message)
      log.err(`Subtitle gagal dimuat · ${(err as Error).message}`, 'sub:search')
    } finally {
      if (!signal?.aborted) setBusy(false)
    }
  }, [media])

  useEffect(() => {
    const c = new AbortController()
    void load(c.signal)
    return () => c.abort()
  }, [load])

  async function translate(sub: { url: string; label?: string; lang?: string }) {
    const key = sub.url
    inflight.current[key]?.abort()
    const c = new AbortController()
    inflight.current[key] = c
    setTr((v) => ({ ...v, [key]: { busy: true } }))
    log.info(`Menerjemahkan subtitle ${sub.label || sub.lang} ke Indonesia…`, 'sub:translate')
    try {
      const body = await api<{ url: string; cues: number; cached: boolean }>(
        `/api/translate?u=${encodeURIComponent(sub.url)}&to=id`,
        c.signal,
      )
      if (c.signal.aborted) return
      setTr((v) => ({ ...v, [key]: { busy: false, url: body.url, cues: body.cues, cached: body.cached } }))
      log.ok(
        `Terjemahan Indonesia siap · ${body.cues} cue${body.cached ? ' · dari cache server' : ''}`,
        'sub:translate',
      )
    } catch (err) {
      if (c.signal.aborted) {
        setTr((v) => {
          const next = { ...v }
          delete next[key]
          return next
        })
        log.info('Terjemahan dibatalkan', 'sub:translate')
        return
      }
      const msg = (err as Error).message
      setTr((v) => ({ ...v, [key]: { busy: false, error: msg } }))
      log.err(`Terjemahan gagal · ${msg}`, 'sub:translate')
    } finally {
      if (inflight.current[key] === c) delete inflight.current[key]
    }
  }

  // Batal: permintaan ke server diputus, jadi proses terjemahan di server ikut
  // berhenti (konteks request dibatalkan).
  function cancel(sub: { url: string }) {
    inflight.current[sub.url]?.abort()
  }

  const total = searchSubs.length + streamSubs.length

  return (
    <div className={`rounded-lg border border-border bg-card p-2.5 sm:rounded-xl sm:p-3 ${open ? 'lg:flex lg:h-full lg:min-h-0 lg:flex-col' : ''}`}>
      <div className="flex items-center gap-2 text-[10px] text-zinc-400">
        <Captions className="h-3.5 w-3.5" />
        <span className="font-semibold uppercase tracking-wide text-zinc-300">Subtitle</span>
        <span className="text-zinc-600">{total}</span>
        <button
          type="button"
          aria-label="Cari subtitle ulang"
          title="Ambil ulang daftar subtitle (fresh)"
          disabled={!media || busy}
          onClick={() => void load(undefined, true)}
          className="ml-auto grid h-7 w-7 place-items-center rounded text-zinc-400 transition hover:bg-zinc-800 hover:text-white disabled:opacity-40 sm:h-6 sm:w-6"
        >
          <RefreshCw className="h-3 w-3" />
        </button>
        <button
          type="button"
          aria-label={open ? 'Tutup pilihan subtitle' : 'Buka pilihan subtitle'}
          onClick={() =>
            setOpen((v) => {
              onOpenChange?.(!v)
              return !v
            })
          }
          className="grid h-7 w-7 place-items-center rounded text-zinc-400 transition hover:bg-zinc-800 hover:text-white sm:h-6 sm:w-6"
        >
          {open ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
        </button>
      </div>

      {open && error && <p className="mt-1 rounded border border-rose-900 bg-rose-950/40 px-2 py-1 text-[10px] text-rose-300">{error}</p>}

      {open && (
      <div className="mt-1 max-h-56 overflow-y-auto font-mono text-[10px] sm:max-h-44 lg:max-h-none lg:min-h-0 lg:flex-1">
        {/* Subtitle bawaan tiap server, langsung tampil tanpa diputar dulu. */}
        {streamSubs.map(({ server, sub: s }, i) => {
          const st = tr[s.url]
          const english = EN_RE.test(`${s.lang} ${s.label}`)
          return (
            <div key={`stream:${i}:${s.url}`} className="border-b border-border/60 py-2 last:border-0 sm:py-1.5">
              {/* Baris atas: bahasa, nama subtitle; tombol di baris bawah. */}
              <div className="flex items-start gap-1.5">
                <span className="w-14 shrink-0 pt-0.5 text-[9px] uppercase text-zinc-500" title={s.label}>
                  {s.lang || s.label}
                </span>
                <span className="min-w-0 flex-1 break-words text-[11px] leading-snug text-zinc-300" title={s.label}>
                  {s.label || s.lang}
                </span>
                <span className="max-w-16 shrink-0 truncate rounded border border-border px-1 text-[9px] text-zinc-500" title={server}>
                  {server || 'player'}
                </span>
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-1.5">
                {english && !st?.url && (
                  <button
                    type="button"
                    aria-label={`Terjemahkan subtitle ${s.label} ke Indonesia`}
                    disabled={st?.busy}
                    onClick={() => void translate(s)}
                    className="inline-flex shrink-0 items-center gap-1 rounded border border-border px-2 py-1 text-[10px] text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:opacity-40"
                  >
                    <Languages className="h-2.5 w-2.5" />
                    {st?.busy ? 'menerjemahkan…' : 'terjemahkan'}
                  </button>
                )}
                <button
                  type="button"
                  aria-label={`Pakai subtitle ${s.label}`}
                  onClick={() => {
                    onUse(s.url, s.label || s.lang)
                    log.ok(`Subtitle dipakai · ${s.label}`, 'sub:pick')
                  }}
                  className="shrink-0 rounded bg-zinc-200 px-2.5 py-1 text-[10px] font-medium text-zinc-900 transition hover:bg-white"
                >
                  pakai
                </button>
              </div>

              {/* Indikator proses translate di bawah tombol. */}
              {st?.busy && (
                <div className="mt-1 flex items-center gap-2 text-[9px] text-sky-400" role="status">
                  <span className="animate-pulse">menerjemahkan ke Indonesia…</span>
                  <button
                    type="button"
                    onClick={() => cancel(s)}
                    className="shrink-0 rounded border border-border px-1.5 py-0.5 text-[9px] text-zinc-300 transition hover:border-rose-700 hover:bg-rose-950/40 hover:text-rose-200"
                  >
                    batal
                  </button>
                </div>
              )}
              {st?.error && <p className="mt-1 text-[9px] text-rose-400">{st.error}</p>}
              {st?.url && (
                <div className="mt-1 flex flex-wrap items-center gap-1.5">
                  <span className="shrink-0 rounded border border-emerald-800 bg-emerald-950/40 px-1 text-[9px] uppercase text-emerald-300">
                    hasil translate
                  </span>
                  <span className="min-w-0 flex-1 truncate text-[10px] text-zinc-400">
                    Indonesia · {st.cues} cue{st.cached ? ' · cache server' : ''}
                  </span>
                  <button
                    type="button"
                    aria-label="Pakai hasil terjemahan Indonesia"
                    onClick={() => {
                      onUse(st.url!, 'Indonesia (terjemahan)')
                      log.ok('Subtitle terjemahan dipakai', 'sub:pick')
                    }}
                    className="shrink-0 rounded bg-zinc-200 px-2.5 py-1 text-[10px] font-medium text-zinc-900 transition hover:bg-white"
                  >
                    pakai
                  </button>
                </div>
              )}
            </div>
          )
        })}

        {busy ? (
          <p className="py-1.5 text-zinc-600">mencari subtitle…</p>
        ) : !media ? (
          <p className="py-1.5 text-zinc-600">Pilih judul dulu untuk mencari subtitle.</p>
        ) : !error && searchSubs.length === 0 && streamSubs.length === 0 ? (
          <p className="py-1.5 text-zinc-600">Tidak ada subtitle untuk judul/episode ini.</p>
        ) : (
          searchSubs.map((s) => {
            const st = tr[s.url]
            const english = EN_RE.test(`${s.lang} ${s.language} ${s.name}`)
            return (
              <div key={s.id} className="border-b border-border/60 py-2 last:border-0 sm:py-1.5">
                {/* Baris atas: bahasa, nama subtitle, unduhan; tombol di baris bawah. */}
                <div className="flex items-start gap-1.5">
                  <span className="w-14 shrink-0 pt-0.5 text-[9px] uppercase text-zinc-500" title={s.language}>
                    {s.language || s.lang}
                  </span>
                  <span className="min-w-0 flex-1 break-words text-[11px] leading-snug text-zinc-300" title={s.name}>
                    {s.name}
                  </span>
                  <span className="shrink-0 text-[10px] text-zinc-600 max-[360px]:hidden">{s.downloads}</span>
                </div>
                <div className="mt-1 flex flex-wrap items-center gap-1.5">
                  {english && !st?.url && (
                    <button
                      type="button"
                      aria-label={`Terjemahkan subtitle ${s.name} ke Indonesia`}
                      disabled={st?.busy}
                      onClick={() => void translate(s)}
                      className="inline-flex shrink-0 items-center gap-1 rounded border border-border px-2 py-1 text-[10px] text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:opacity-40"
                    >
                      <Languages className="h-2.5 w-2.5" />
                      {st?.busy ? 'menerjemahkan…' : 'terjemahkan'}
                    </button>
                  )}
                  <button
                    type="button"
                    aria-label={`Pakai subtitle ${s.name}`}
                    onClick={() => {
                      onUse(s.url, s.language || s.lang)
                      log.ok(`Subtitle dipakai · ${s.name}`, 'sub:pick')
                    }}
                    className="shrink-0 rounded bg-zinc-200 px-2.5 py-1 text-[10px] font-medium text-zinc-900 transition hover:bg-white"
                  >
                    pakai
                  </button>
                </div>

                {st?.busy && (
                  <div className="mt-1 flex items-center gap-2 text-[9px] text-sky-400" role="status">
                    <span className="animate-pulse">menerjemahkan ke Indonesia…</span>
                    <button
                      type="button"
                      onClick={() => cancel(s)}
                      className="shrink-0 rounded border border-border px-1.5 py-0.5 text-[9px] text-zinc-300 transition hover:border-rose-700 hover:bg-rose-950/40 hover:text-rose-200"
                    >
                      batal
                    </button>
                  </div>
                )}
                {st?.error && <p className="mt-1 text-[9px] text-rose-400">{st.error}</p>}
                {st?.url && (
                  <div className="mt-1 flex flex-wrap items-center gap-1.5">
                    <span className="shrink-0 rounded border border-emerald-800 bg-emerald-950/40 px-1 text-[9px] uppercase text-emerald-300">
                      hasil translate
                    </span>
                    <span className="min-w-0 flex-1 truncate text-[10px] text-zinc-400">
                      Indonesia · {st.cues} cue{st.cached ? ' · cache server' : ''}
                    </span>
                    <button
                      type="button"
                      aria-label="Pakai hasil terjemahan Indonesia"
                      onClick={() => {
                        onUse(st.url!, 'Indonesia (terjemahan)')
                        log.ok('Subtitle terjemahan dipakai', 'sub:pick')
                      }}
                      className="shrink-0 rounded bg-zinc-200 px-2.5 py-1 text-[10px] font-medium text-zinc-900 transition hover:bg-white"
                    >
                      pakai
                    </button>
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
      )}
    </div>
  )
}

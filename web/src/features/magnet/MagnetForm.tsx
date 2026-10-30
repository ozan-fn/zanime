import { useEffect, useRef, useState } from 'react'
import { Download, LoaderCircle, X } from 'lucide-react'
import { lastSaved, saveLast } from './last'
import { resetEpThumbs } from '@/features/files/thumbs'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'

const DEFAULT_MAGNET = 'magnet:?xt=urn:btih:3c5356a2834c80305e18479ad39d16a4f8b8682b&dn=%5BErai-raws%5D%20Gabriel%20DropOut%20-%2001%20~%2012%20%5B1080p%5D%5BMultiple%20Subtitle%5D&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Fopen.stealth.si%3A80%2Fannounce&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Fexodus.desync.com%3A6969%2Fannounce&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce'

const STATUS_STYLES: Record<string, string> = {
  info: 'border-border bg-card text-zinc-300',
  ok: 'border-zinc-700 bg-zinc-900 text-zinc-100',
  error: 'border-rose-400/20 bg-rose-400/[0.07] text-rose-200',
}

export function MagnetForm() {
  const { status, statusSource, loadBusy, setStatus, loadMagnet, cancelLoad, selectFile } = useTorrent()
  const [loading, setLoading] = useState(false)
  const [magnet, setMagnet] = useState(() => lastSaved().magnet || DEFAULT_MAGNET)
  // Auto-resume: magnet tersimpan di localStorage disubmit ulang sekali,
  // seperti requestSubmit() amanat versi HTML.
  const autoResumed = useRef(false)
  useEffect(() => {
    if (!lastSaved().magnet) return
    void customElements.whenDefined('movi-player').then(() => {
      if (autoResumed.current) return
      autoResumed.current = true
      void onSubmit({ preventDefault() {} } as unknown as React.FormEvent)
    })
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const onSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    const value = magnet.trim() || DEFAULT_MAGNET
    if (!magnet.trim()) setMagnet(DEFAULT_MAGNET)
    setLoading(true)
    setStatus('Menghubungi tracker dan menunggu metadata torrent… klik Batalkan untuk membatalkan.', 'info')
    try {
      const next = await loadMagnet(
        { magnet: value },
        (progress) => {
          const files = progress.files || []
          const saved = lastSaved()
          /* Magnet baru dari kolom tempel: buang thumbnail judul sebelumnya.
           * Magnet sama (auto-resume reload) memakai thumbnail yang tersimpan. */
          if (saved.magnet !== value) resetEpThumbs()
          const pick = (saved.magnet === value ? files[saved.index] : null)
            || files[progress.selected ?? 0] || files[0]
          saveLast({
            magnet: value,
            index: pick ? pick.index : 0,
          })
          setStatus(`Torrent siap: ${progress.name || 'tanpa nama'}. Pilih video untuk diputar.`, 'ok')
          if (pick) void selectFile(pick.index)
        },
      )
      saveLast({ magnet: value })
      return next
    } catch (err) {
      // AbortError = kita sendiri yang membatalkan; pesannya sudah diatur oleh
      // handler tombol Batalkan.
      if ((err as Error).name !== 'AbortError') setStatus((err as Error).message, 'error')
    } finally {
      setLoading(false)
    }
  }

  const onCancel = async () => {
    setStatus('Membatalkan pemuatan magnet…', 'info')
    try {
      await cancelLoad()
      setStatus('Pemuatan magnet dibatalkan. Torrent yang sedang aktif tidak diubah.', 'ok')
    } catch (err) {
      setStatus(`Batalkan gagal: ${(err as Error).message}`, 'error')
    }
  }

  const busy = loading || loadBusy

  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <form className="space-y-3" onSubmit={onSubmit}>
        <div className="flex items-center justify-between">
          <label htmlFor="magnet" className="text-[11px] font-semibold uppercase tracking-[0.14em] text-zinc-500">Magnet link</label>
          <span className="text-[11px] text-zinc-600">BitTorrent</span>
        </div>
        <textarea
          id="magnet"
          rows={3}
          spellCheck={false}
          autoComplete="off"
          value={magnet}
          onChange={(e) => setMagnet(e.target.value)}
          className="w-full resize-none rounded-lg border border-border bg-zinc-900 px-3 py-2.5 font-mono text-xs leading-relaxed text-zinc-200 outline-none transition placeholder:text-zinc-600 focus:border-zinc-600"
          placeholder="magnet:?xt=urn:btih:..."
        />
        <div className="flex items-stretch gap-2">
          <button
            id="load-btn"
            type="submit"
            disabled={busy}
            className="inline-flex flex-1 items-center justify-center gap-2 rounded-lg bg-primary px-4 py-2.5 text-sm font-semibold text-primary-contrast transition hover:bg-zinc-200 focus:outline-none focus:ring-2 focus:ring-zinc-500/50 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <LoaderCircle className={busy ? 'h-4 w-4 animate-spin' : 'hidden h-4 w-4'} />
            {!busy && <Download className="h-4 w-4" />}
            <span>Muat torrent</span>
          </button>
          <button
            type="button"
            onClick={onCancel}
            className={[
              busy ? 'inline-flex' : 'hidden',
              'shrink-0 items-center justify-center gap-1.5 rounded-lg border border-rose-400/30 bg-rose-500/10 px-4 py-2.5 text-sm font-semibold text-rose-200 transition hover:bg-rose-500/20 focus:outline-none focus:ring-2 focus:ring-rose-400/40 disabled:cursor-not-allowed disabled:opacity-60',
            ].join(' ')}
          >
            <X className="h-4 w-4 shrink-0" />
            Batalkan
          </button>
        </div>
      </form>

      {status ? (
        // break-all: nama file panjang (satu token tanpa spasi) dibungkus per
        // karakter, jadi tidak mendorong kartu lain melebar / tidak sejajar.
        <div
          role="status"
          className={`mt-3 min-w-0 break-all rounded-xl border px-3 py-2 text-xs leading-relaxed ${STATUS_STYLES[status.kind] || STATUS_STYLES.info}`}
        >
          {status.text}
        </div>
      ) : (
        <div className="mt-3 text-xs text-zinc-500">
          {statusSource === 'ws' ? 'live' : statusSource === 'poll' ? 'polling 2 dtk' : 'menyambung…'}
        </div>
      )}
    </div>
  )
}

// Saat tidak ada pesan eksplisit, tampilkan sumber status (WS/polling).

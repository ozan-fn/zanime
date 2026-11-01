import { useEffect, useRef, useState } from 'react'
import { clearLogs, useLogs, type Level } from '@/lib/logs'

/* Panel log realtime dari satu store (lib/logs), dipasang di bawah player:
 * baris scrape (daftar stream diuji) dan pemuatan video tampil di sini.
 * Satu langkah tampil "[sedang…]" lalu berubah "[OK]"/"[GAGAL]" (baris ber-id
 * sama digantikan di tempat, bukan menumpuk). */

const BADGE: Record<Level, { tag: string; cls: string }> = {
  info: { tag: 'sedang', cls: 'text-amber-300' },
  ok: { tag: 'OK', cls: 'text-emerald-400' },
  err: { tag: 'GAGAL', cls: 'text-rose-400' },
}

export function LogPanel({
  className = '',
  panelClassName = 'rounded border border-border',
  maxHeight = 'max-h-44',
}: {
  /** Kelas pembungkus (menentukan posisi: overlay di player / strip di dialog). */
  className?: string
  /** Kelas panelnya (lebar, sudut, border). */
  panelClassName?: string
  /** Tinggi maksimum daftar baris. */
  maxHeight?: string
}) {
  const entries = useLogs()
  const [open, setOpen] = useState(true)
  const boxRef = useRef<HTMLDivElement | null>(null)

  // Ikuti baris terbaru.
  useEffect(() => {
    const box = boxRef.current
    if (box) box.scrollTop = box.scrollHeight
  }, [entries])

  return (
    <div className={className}>
      <div className={`overflow-hidden bg-black/85 backdrop-blur ${panelClassName}`}>
        <div className="flex items-center gap-2 border-b border-border px-2 py-1 text-[10px] text-zinc-400">
          <span className="font-semibold uppercase tracking-wide text-zinc-300">Log</span>
          <span className="text-zinc-600">{entries.length}</span>
          <button type="button" onClick={clearLogs}
            className="ml-auto rounded px-1.5 py-0.5 text-zinc-400 transition hover:bg-zinc-800 hover:text-white">
            bersihkan
          </button>
          <button type="button" onClick={() => setOpen((v) => !v)}
            className="rounded px-1.5 py-0.5 text-zinc-400 transition hover:bg-zinc-800 hover:text-white">
            {open ? 'tutup' : 'buka'}
          </button>
        </div>
        {open && (
          <div ref={boxRef} className={`${maxHeight} overflow-y-auto px-2 py-1 font-mono text-[10px] leading-[1.5]`}>
            {entries.length === 0 ? (
              <div className="py-2 text-zinc-600">belum ada aktivitas…</div>
            ) : (
              entries.map((e) => {
                const b = BADGE[e.level]
                return (
                  <div key={e.id} className="flex gap-1.5">
                    <span className="shrink-0 text-zinc-600">{e.at}</span>
                    <span className={`w-[46px] shrink-0 ${b.cls}`}>[{b.tag}]</span>
                    <span className="min-w-0 break-words text-zinc-300">{e.text}</span>
                  </div>
                )
              })
            )}
          </div>
        )}
      </div>
    </div>
  )
}

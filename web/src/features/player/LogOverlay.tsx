import { useEffect, useRef, useState } from 'react'
import { clearLogs, useLogs, type Level } from '@/lib/logs'

/* Panel log realtime di depan player (absolute, z-30): lihat tiap tahap
 * scrape, uji stream 128 KiB, dan event pemuatan video. Satu langkah tampil
 * "[sedang…]" lalu berubah "[OK]"/"[GAGAL]" (baris ber-id sama). */

const BADGE: Record<Level, { tag: string; cls: string }> = {
  info: { tag: 'sedang', cls: 'text-amber-300' },
  ok: { tag: 'OK', cls: 'text-emerald-400' },
  err: { tag: 'GAGAL', cls: 'text-rose-400' },
}

export function LogOverlay() {
  const entries = useLogs()
  const [open, setOpen] = useState(true)
  const boxRef = useRef<HTMLDivElement | null>(null)

  // Ikuti baris terbaru.
  useEffect(() => {
    const box = boxRef.current
    if (box) box.scrollTop = box.scrollHeight
  }, [entries])

  return (
    <div className="pointer-events-none absolute inset-x-0 top-0 z-30 flex justify-end p-2">
      <div className="pointer-events-auto w-[min(460px,88%)] overflow-hidden rounded border border-border bg-black/85 backdrop-blur">
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
          <div ref={boxRef} className="max-h-44 overflow-y-auto px-2 py-1 font-mono text-[10px] leading-[1.5]">
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

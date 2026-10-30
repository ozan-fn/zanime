import { useEffect, useRef, useState } from 'react'
import { fmt, fmtDur } from '@/lib/fmt'
import { useTorrent } from '@/features/torrent-state/TorrentStateProvider'
import { torrentCount } from './count'

/* Kartu Status: detail torrent, cache disk, unduhan + kecepatan dua arah,
 * dan status proses server. Kecepatan/eviction rate dihitung dari selisih
 * antar frame status (0,7 dtk via WS), bukan disimpan server. */
export function StatsCard() {
  const { state, statusSource } = useTorrent()
  const tor = state?.torrent

  // Laju eviction: selisih evictions antar frame; window >= 1 dtk supaya
  // angkanya tidak melonjak saat WS dan fetch datang berdekatan.
  const [evictRate, setEvictRate] = useState('—')
  const lastEvict = useRef<{ n: number; t: number } | null>(null)
  const cache = state?.cache

  /* Kecepatan dari selisih byte kumulatif antara dua push status (0,7 dtk).
   * Dua arah: peer → server (unduhan dari swarm) dan server → client
   * (byte yang keluar ke player). Panah menyala saat ada aliran. */
  const [rates, setRates] = useState({ down: '0.00', up: '0.00', downOn: false, upOn: false })
  const lastNet = useRef<{ t: number; from: number; to: number } | null>(null)

  useEffect(() => {
    // Eviction rate.
    if (cache) {
      const now = performance.now()
      const evictions = cache.evictions || 0
      if (lastEvict.current && now - lastEvict.current.t >= 1000) {
        const perMin = Math.max(0, evictions - lastEvict.current.n) / ((now - lastEvict.current.t) / 1000) * 60
        setEvictRate(perMin >= 1 ? perMin.toFixed(0) : perMin > 0 ? '<1' : '0')
        lastEvict.current = { n: evictions, t: now }
      } else if (!lastEvict.current) {
        lastEvict.current = { n: evictions, t: now }
      }
    }
    // Mbps dua arah.
    const net = state?.net
    if (!net && !lastNet.current) return
    const now = performance.now()
    const from = net?.from_peers || 0
    const to = net?.to_client || 0
    if (lastNet.current) {
      const dt = (now - lastNet.current.t) / 1000
      if (dt >= 0.2) {
        const down = Math.max(0, from - lastNet.current.from) / dt * 8 / 1e6
        const up = Math.max(0, to - lastNet.current.to) / dt * 8 / 1e6
        setRates({
          down: down >= 10 ? down.toFixed(1) : down.toFixed(2),
          up: up >= 10 ? up.toFixed(1) : up.toFixed(2),
          downOn: down >= 0.05,
          upOn: up >= 0.05,
        })
      }
    }
    lastNet.current = { t: now, from, to }
    // Akalnya dari state yang sama; `cache` hanyalah state?.cache.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state])

  if (!state?.name) return null

  const cacheUsed = cache?.used || 0
  const cacheLimit = cache?.limit || 1
  const total = state.length || 0
  // Unduhan kumulatif bisa melebihi ukuran torrent saat piece diambil ulang.
  const dl = total ? Math.min(state.downloaded || 0, total) : (state.downloaded || 0)
  const dlPct = total ? `${Math.min(100, (dl / total) * 100)}%` : '0%'
  const wasted = tor?.wasted_bytes || 0
  const wastedShare = tor?.data_in ? (wasted / tor.data_in) * 100 : 0

  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <div className="flex items-center justify-between">
        <h2 className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.14em] text-zinc-500">
          Status
          <span className={['font-normal normal-case tracking-normal', statusSource === 'ws' ? 'text-zinc-400' : 'text-amber-400'].join(' ')}>
            {statusSource === 'ws' ? 'live' : statusSource === 'poll' ? 'polling 2 dtk' : 'menyambung…'}
          </span>
        </h2>
        <span className="text-[11px] text-zinc-500">
          <span className="font-semibold text-zinc-300">{state.peers || 0}</span> peer
        </span>
      </div>

      <div className="mt-3 space-y-3.5 text-[11px]">
        {tor && (
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-600">Torrent</span>
              <span title={state.info_hash || ''} className="max-w-[8rem] truncate font-mono text-[10px] text-zinc-500">
                {state.info_hash ? `${state.info_hash.slice(0, 10)}…` : '—'}
              </span>
            </div>
            <Row label="Piece" value={`${tor.complete_pieces || 0}/${tor.pieces || 0} · ${fmt(tor.piece_length || 0)}/piece`} />
            <Row label="Byte lengkap" value={fmt(tor.ram_bytes || 0)} />
            <Row label="Parsial" value={`${tor.partial_pieces || 0} piece · ${tor.busy_pieces || 0} hash`} />
            <Row label="Peer" value={`${state.peers || 0} aktif · ${tor.total_peers || 0} total`} />
            <Row label="Seeder" value={String(tor.seeders || 0)} />
            <Row label="Negosiasi" value={`${tor.half_open || 0} half-open · ${tor.pending_peers || 0} pending`} />
            <Row label="Chunk" value={`${tor.chunks_useful || 0} useful · ${tor.chunks_wasted || 0} waste`} />
            <Row label="Wire / hash" value={`${fmt(tor.wire_in || 0)} · ${fmt(tor.hashed || 0)}`} />
          </div>
        )}

        {cache && (
          <div className="border-t border-border/60 pt-3">
            <div className="flex items-center justify-between text-zinc-500">
              <span>Cache disk</span>
              <span><span className="text-zinc-300">{fmt(cacheUsed)}</span> · <span className="text-zinc-300">{cache.evictions || 0}</span> evict</span>
            </div>
            <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-white/[0.06]">
              <div className="h-full rounded-full bg-zinc-100 transition-[width] duration-500" style={{ width: `${Math.min(100, (cacheUsed / cacheLimit) * 100)}%` }} />
            </div>
            <Row label="Torrent di disk" value={`${fmt(cacheUsed)} (${torrentCount(tor)})`} />
            <Row label="Eviction / menit" value={evictRate} />
          </div>
        )}

        <div className="border-t border-border/60 pt-3">
          <div className="flex items-center justify-between text-zinc-500">
            <span>Terunduh (semua file)</span>
            <span className="text-zinc-300">{total ? `${fmt(dl)} / ${fmt(total)}` : fmt(dl)}</span>
          </div>
          <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-white/[0.06]">
            <div className="h-full rounded-full bg-zinc-100 transition-[width] duration-500" style={{ width: dlPct }} />
          </div>
          <div className="mt-1 space-y-1">
            <div className="flex items-baseline justify-between gap-3 text-zinc-600">
              <span className="flex items-center gap-1.5">
                <span className={`net-ic down${rates.downOn ? ' on' : ''}`} aria-hidden="true">↓</span>
                Peer → server
              </span>
              <span className="tabular-nums text-zinc-300">{rates.down} Mbps</span>
            </div>
            <div className="flex items-baseline justify-between gap-3 text-zinc-600">
              <span className="flex items-center gap-1.5">
                <span className={`net-ic up${rates.upOn ? ' on' : ''}`} aria-hidden="true">↑</span>
                server → client
              </span>
              <span className="tabular-nums text-zinc-300">{rates.up} Mbps</span>
            </div>
            <Row label="Byte terbuang" value={`${fmt(wasted)} · ${wastedShare.toFixed(1)}%`} />
          </div>
        </div>

        <div className="border-t border-border/60 pt-3">
          <div className="flex items-center justify-between">
            <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-600">Server</span>
            <span className="text-[10px] text-zinc-500">
              {state.hls
                ? `hls #${state.hls.index} ${state.hls.ready ? 'siap' : 'menunggu'}`
                : 'hls mati'}
            </span>
          </div>
          <Row label="User / tab" value={state.users ? `${state.users.active || 0} user · ${state.users.tabs || 0} tab` : '—'} />
          <Row label="Uptime" value={state.server ? fmtDur(state.server.uptime || 0) : '—'} />
          <Row label="Heap / goroutine" value={state.server ? `${fmt(state.server.heap || 0)} · ${state.server.goroutines || 0}` : '—'} />
        </div>
      </div>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="mt-1 flex items-baseline justify-between gap-3 text-zinc-600">
      <span className="shrink-0">{label}</span>
      <span className="min-w-0 truncate text-right tabular-nums text-zinc-400">{value}</span>
    </div>
  )
}
